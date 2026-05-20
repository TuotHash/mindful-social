package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/auth"
	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	accountFlashKey          = "account_flash"
	accountSuccessKey        = "account_success"
	maxProfileImageBytes     = 12 << 20
	maxCompressedImageBytes  = 2 << 20
	maxProfileImageDimension = 2048
	profileImageFormField    = "profile_image"
	profileImageUploadPerm   = 0o644
)

// handleAccount renders /account: preferences, read-only details, password
// change form, and the list of sign-in methods with disconnect controls.
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)

	identities, err := s.queries.ListIdentitiesForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("account: list identities", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	hasPassword := false
	rows := make([]views.AccountIdentity, 0, len(identities))
	for _, ident := range identities {
		if ident.Provider == "password" {
			hasPassword = true
		}
		rows = append(rows, views.AccountIdentity{
			ID:            ident.ID,
			Provider:      ident.Provider,
			Label:         identityLabel(ident.Provider),
			Subject:       ident.Subject,
			CanDisconnect: len(identities) > 1,
		})
	}

	prefVisibility := string(user.DefaultNodeVisibility)
	if prefVisibility == "" {
		prefVisibility = string(db.VisibilityKindPublic)
	}

	media, err := s.accountMedia(r, user.ID)
	if err != nil {
		s.logger.Error("account: list media", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	flash := s.sessions.PopString(r.Context(), accountFlashKey)
	success := s.sessions.PopString(r.Context(), accountSuccessKey)
	render(w, r, views.Account(viewerFor(user), *user, rows, media, hasPassword, prefVisibility, user.Timezone, flash, success))
}

func (s *Server) accountMedia(r *http.Request, userID uuid.UUID) ([]views.AccountMediaItem, error) {
	images, err := s.queries.ListNodeImagesByUploader(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	videos, err := s.queries.ListNodeVideosByUploader(r.Context(), userID)
	if err != nil {
		return nil, err
	}
	items := make([]views.AccountMediaItem, 0, len(images)+len(videos))
	for _, img := range images {
		items = append(items, views.AccountMediaItem{
			Kind:           "image",
			Path:           img.StoredPath,
			ContentType:    img.ContentType,
			ByteSize:       img.ByteSize,
			CreatedAt:      img.CreatedAt.Time,
			RootTopicSlug:  img.RootTopicSlug,
			RootTopicTitle: img.RootTopicTitle,
		})
	}
	for _, vid := range videos {
		items = append(items, views.AccountMediaItem{
			Kind:           "video",
			Path:           vid.StoredPath,
			ContentType:    vid.ContentType,
			ByteSize:       vid.ByteSize,
			Width:          vid.Width,
			Height:         vid.Height,
			DurationMs:     vid.DurationMs,
			CreatedAt:      vid.CreatedAt.Time,
			RootTopicSlug:  vid.RootTopicSlug,
			RootTopicTitle: vid.RootTopicTitle,
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
	return items, nil
}

// handleAccountPreferences persists the user's default node visibility
// and timezone. Visibility is a plain enum value — only the audience-
// independent kinds (public, connections, private) are valid defaults
// since group selection is per-node. Timezone is validated against the
// IANA database; the empty string is allowed and means "fall back to
// UTC".
func (s *Server) handleAccountPreferences(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	var visKind db.VisibilityKind
	switch strings.TrimSpace(r.PostFormValue("visibility")) {
	case "", "public":
		visKind = db.VisibilityKindPublic
	case "connections":
		visKind = db.VisibilityKindConnections
	case "private":
		visKind = db.VisibilityKindPrivate
	default:
		s.flashAccount(r, "Invalid visibility option.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	tz := strings.TrimSpace(r.PostFormValue("timezone"))
	if tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			s.flashAccount(r, "That timezone isn't recognised. Pick one from the list.")
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
	}

	if err := s.queries.UpdateUserPreferences(r.Context(), db.UpdateUserPreferencesParams{
		ID:                    user.ID,
		DefaultNodeVisibility: visKind,
		Timezone:              tz,
	}); err != nil {
		s.logger.Error("account prefs: update", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	s.successAccount(r, "Preferences saved.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// handleAccountProfileImage accepts a PNG/JPEG/GIF upload, decodes it,
// downscales and re-encodes it as a JPEG under maxCompressedImageBytes,
// and stores it at /uploads/profiles/<uid>-<hash>.jpg. The hash in the
// filename busts the 24-hour Cache-Control set by cacheStatic so a
// re-upload shows immediately. The previous file is removed on re-upload
// so the upload dir doesn't accrue orphaned blobs.
func (s *Server) handleAccountProfileImage(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	r.Body = http.MaxBytesReader(w, r.Body, maxProfileImageBytes+1024)
	file, _, err := r.FormFile(profileImageFormField)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.flashAccount(r, "Profile pictures must be 12 MB or smaller before processing.")
		} else {
			s.flashAccount(r, "Choose a PNG, JPEG, or GIF image to upload.")
		}
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			s.flashAccount(r, "Profile pictures must be 12 MB or smaller before processing.")
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		s.logger.Error("account image: read", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if len(data) == 0 {
		s.flashAccount(r, "Choose a PNG, JPEG, or GIF image to upload.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	// Decompression-bomb gate: parse the header only and bail out before
	// image.Decode allocates the full pixel buffer.
	if cfg, _, derr := image.DecodeConfig(bytes.NewReader(data)); derr != nil {
		s.flashAccount(r, "Profile pictures must be PNG, JPEG, or GIF images.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	} else if int64(cfg.Width)*int64(cfg.Height) > maxDecodedPixels {
		s.flashAccount(r, "Profile pictures are limited to 50 megapixels before decoding.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		s.flashAccount(r, "Profile pictures must be PNG, JPEG, or GIF images.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	if !isSupportedProfileImageFormat(format) {
		s.flashAccount(r, "Profile pictures must be PNG, JPEG, or GIF images.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}
	processed, err := compressProfileImage(img, maxProfileImageDimension, maxCompressedImageBytes)
	if err != nil {
		s.flashAccount(r, "Could not compress image under 2 MB. Try a smaller image.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	dir := filepath.Join(s.cfg.UploadDir, "profiles")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		s.logger.Error("account image: mkdir", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(processed)
	name := user.ID.String() + "-" + hex.EncodeToString(sum[:4]) + ".jpg"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, processed, profileImageUploadPerm); err != nil {
		s.logger.Error("account image: write", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	publicPath := "/uploads/profiles/" + name
	previousPath := user.ProfileImagePath
	if err := s.queries.UpdateUserProfileImage(r.Context(), db.UpdateUserProfileImageParams{
		ID:               user.ID,
		ProfileImagePath: publicPath,
	}); err != nil {
		s.logger.Error("account image: update user", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	removePreviousProfileImage(s.cfg.UploadDir, previousPath, publicPath)

	s.successAccount(r, "Profile picture updated.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// removePreviousProfileImage deletes the file an earlier upload left
// behind once the database points at a new one. Failures are silent —
// an orphaned blob is harmless, and we don't want the success flash to
// flip to an error on what is effectively a cleanup pass.
func removePreviousProfileImage(uploadDir, previousPath, currentPath string) {
	const prefix = "/uploads/profiles/"
	if previousPath == "" || previousPath == currentPath {
		return
	}
	if !strings.HasPrefix(previousPath, prefix) {
		return
	}
	name := strings.TrimPrefix(previousPath, prefix)
	// Defensive: reject anything that would escape the profiles dir.
	if name == "" || strings.ContainsRune(name, '/') || strings.Contains(name, "..") {
		return
	}
	_ = os.Remove(filepath.Join(uploadDir, "profiles", name))
}

func isSupportedProfileImageFormat(format string) bool {
	switch format {
	case "jpeg", "png", "gif":
		return true
	default:
		return false
	}
}

func compressProfileImage(src image.Image, maxDimension int, maxBytes int) ([]byte, error) {
	b := src.Bounds()
	w := b.Dx()
	h := b.Dy()
	if w <= 0 || h <= 0 {
		return nil, errors.New("invalid image dimensions")
	}
	scale := 1.0
	largest := w
	if h > largest {
		largest = h
	}
	if largest > maxDimension {
		scale = float64(maxDimension) / float64(largest)
	}

	for attempt := 0; attempt < 8; attempt++ {
		tw := int(math.Round(float64(w) * scale))
		th := int(math.Round(float64(h) * scale))
		if tw < 1 {
			tw = 1
		}
		if th < 1 {
			th = 1
		}
		resized := resizeImageNearest(src, tw, th)
		rgba := flattenToOpaqueRGBA(resized)
		for _, quality := range []int{86, 80, 74, 68, 62, 56, 50} {
			var out bytes.Buffer
			if err := jpeg.Encode(&out, rgba, &jpeg.Options{Quality: quality}); err != nil {
				return nil, err
			}
			if out.Len() <= maxBytes {
				return out.Bytes(), nil
			}
		}
		scale *= 0.85
	}
	return nil, errors.New("unable to compress within byte limit")
}

func flattenToOpaqueRGBA(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

func resizeImageNearest(src image.Image, width, height int) *image.RGBA {
	sb := src.Bounds()
	sw := sb.Dx()
	sh := sb.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	if sw == 0 || sh == 0 {
		return dst
	}
	for y := 0; y < height; y++ {
		sy := sb.Min.Y + (y * sh / height)
		for x := 0; x < width; x++ {
			sx := sb.Min.X + (x * sw / width)
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

// handleAccountPasswordSet handles both setting an initial password
// (OAuth-only users) and changing an existing one. The presence of a
// current_password field in the form distinguishes the two; we also
// double-check against the DB so a missing field can't bypass the check.
func (s *Server) handleAccountPasswordSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}
	current := r.PostFormValue("current_password")
	newPw := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")

	if newPw != confirm {
		s.flashAccount(r, "The new password and confirmation don't match.")
		http.Redirect(w, r, "/account", http.StatusSeeOther)
		return
	}

	_, err := s.queries.GetPasswordIdentityForUser(r.Context(), user.ID)
	hasPassword := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.logger.Error("account: lookup password identity", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	if hasPassword {
		if strings.TrimSpace(current) == "" {
			s.flashAccount(r, "Enter your current password to change it.")
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		if err := s.authSvc.ChangePassword(r.Context(), user.ID, current, newPw); err != nil {
			s.flashAccount(r, humanizeAccountAuthErr(err))
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		// Sign every other device out so a stolen-cookie attacker loses
		// access immediately. The current request's session is preserved
		// so the user isn't bounced back to /login mid-flow.
		if err := auth.RevokeUserSessions(r.Context(), s.sessions, user.ID, true); err != nil {
			s.logger.Error("account: revoke sessions after password change", "err", err, "user_id", user.ID)
		}
		s.successAccount(r, "Password updated.")
	} else {
		if err := s.authSvc.SetInitialPassword(r.Context(), user.ID, newPw); err != nil {
			s.flashAccount(r, humanizeAccountAuthErr(err))
			http.Redirect(w, r, "/account", http.StatusSeeOther)
			return
		}
		s.successAccount(r, "Password set. You can now sign in with your email and password.")
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// handleAccountIdentityDisconnect removes one OAuth/password identity from
// the user's account. Refuses to remove the last remaining method so
// nobody can accidentally lock themselves out.
func (s *Server) handleAccountIdentityDisconnect(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	idStr := chiURLParam(r, "id")
	identityID, err := uuid.Parse(idStr)
	if err != nil {
		s.notFound(w, r)
		return
	}
	switch err := s.authSvc.DisconnectIdentity(r.Context(), user.ID, identityID); {
	case err == nil:
		s.successAccount(r, "Sign-in method disconnected.")
	case errors.Is(err, auth.ErrIdentityNotFound):
		s.notFound(w, r)
		return
	case errors.Is(err, auth.ErrLastIdentity):
		s.flashAccount(r, "You can't disconnect your last sign-in method.")
	default:
		s.logger.Error("account: disconnect identity", "err", err)
		s.flashAccount(r, "Couldn't disconnect that sign-in method. Please try again.")
	}
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// humanizeAccountAuthErr re-uses humanizeAuthErr but rewrites a couple of
// messages that read wrong in the account-page context (e.g. an
// "Invalid email or password" error during a password change really
// means the current password was wrong).
func humanizeAccountAuthErr(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidLogin):
		return "Your current password is incorrect."
	case errors.Is(err, auth.ErrNoPassword):
		return "No password is set for this account yet."
	case errors.Is(err, auth.ErrPasswordExists):
		return "A password is already set. Use the change form above."
	}
	return humanizeAuthErr(err)
}

func (s *Server) flashAccount(r *http.Request, msg string) {
	s.sessions.Put(r.Context(), accountFlashKey, msg)
}

func (s *Server) successAccount(r *http.Request, msg string) {
	s.sessions.Put(r.Context(), accountSuccessKey, msg)
}
