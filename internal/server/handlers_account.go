package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/auth"
	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	accountFlashKey   = "account_flash"
	accountSuccessKey = "account_success"
)

// handleAccount renders /account: preferences, read-only details, password
// change form, and the list of sign-in methods with disconnect controls.
func (s *Server) handleAccount(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)

	identities, err := s.queries.ListIdentitiesForUser(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("account: list identities", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
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

	lists, err := s.queries.ListAudienceLists(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("account: list audience lists", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	prefVisibility := formatVisibility(user.DefaultNodeVisibility, user.DefaultAudienceListID)

	flash := s.sessions.PopString(r.Context(), accountFlashKey)
	success := s.sessions.PopString(r.Context(), accountSuccessKey)
	render(w, r, views.Account(viewerFor(user), *user, rows, hasPassword, lists, prefVisibility, user.Timezone, flash, success))
}

// handleAccountPreferences persists the user's default node visibility,
// optional audience list, and timezone. Visibility uses the same encoding
// as the composer ("public|connections|list:<uuid>|private"). Timezone is
// validated against the IANA database; the empty string is allowed and
// means "fall back to UTC".
func (s *Server) handleAccountPreferences(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	lists, err := s.queries.ListAudienceLists(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("account prefs: list audience lists", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	visKind, visListID, visErr := parseVisibility(r.PostFormValue("visibility"), user.ID, lists)
	if visErr != "" {
		s.flashAccount(r, visErr)
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
		ID:                     user.ID,
		DefaultNodeVisibility:  visKind,
		DefaultAudienceListID:  visListID,
		Timezone:               tz,
	}); err != nil {
		s.logger.Error("account prefs: update", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	s.successAccount(r, "Preferences saved.")
	http.Redirect(w, r, "/account", http.StatusSeeOther)
}

// handleAccountPasswordSet handles both setting an initial password
// (OAuth-only users) and changing an existing one. The presence of a
// current_password field in the form distinguishes the two; we also
// double-check against the DB so a missing field can't bypass the check.
func (s *Server) handleAccountPasswordSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
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
		http.Error(w, "internal error", http.StatusInternalServerError)
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
		http.NotFound(w, r)
		return
	}
	switch err := s.authSvc.DisconnectIdentity(r.Context(), user.ID, identityID); {
	case err == nil:
		s.successAccount(r, "Sign-in method disconnected.")
	case errors.Is(err, auth.ErrIdentityNotFound):
		http.NotFound(w, r)
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
