package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

const (
	nodeImageFormField  = "image"
	nodeImageUploadPerm = 0o644
	nodeImageDirPerm    = 0o755

	// JPEG quality ladder the recompressor walks when the budget is not
	// met at the previous step. 86 is the high-end default; below ~50 the
	// extra savings come with very visible blocking.
	nodeImageJPEGQualityFloor = 50
	nodeImageJPEGQualityStart = 86
	nodeImageJPEGQualityStep  = 6

	// Minimum byte target so the per-megapixel budget can never demand
	// fewer bytes than a valid JPEG header. 10 KiB comfortably covers a
	// quality-50 thumbnail.
	nodeImageMinTargetBytes = 10 * 1024
)

// nodeImageUploadResponse mirrors the shape EasyMDE's imageUploadEndpoint
// expects. On success it sets {data:{filePath:...}}; on error it sets
// {error:"<code>"} (code is keyed against EasyMDE's errorMessages so a
// well-known code renders the localized message).
type nodeImageUploadResponse struct {
	Data  *nodeImageUploadData `json:"data,omitempty"`
	Error string               `json:"error,omitempty"`
}

type nodeImageUploadData struct {
	FilePath string `json:"filePath"`
}

// handleNodeImageUpload accepts a multipart image upload from the markdown
// editor and stores it against the root topic of the subtree the form is
// editing. The same picture then renders inline for every descendant of
// that root topic without a per-node copy.
//
// Pre-storage the picture is downscaled to fit cfg.NodeImageMaxDimension
// on its longest side and (PNG/JPEG) recompressed to JPEG against a
// per-megapixel byte budget — both knobs configurable. Each stage emits a
// debug log line so operators can diagnose "why are my uploads still so
// big" without instrumenting the pipeline.
//
// Authorization: the viewer must have link permission on the given node
// (i.e. they are able to add content under it). That keeps the upload
// surface aligned with "who is allowed to author here at all".
func (s *Server) handleNodeImageUpload(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}

	allowed, err := s.canLinkToNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node image upload: link policy", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}
	if !allowed {
		writeNodeImageError(w, http.StatusForbidden, "noPermission")
		return
	}

	rootID, err := s.queries.FindRootTopicForNode(r.Context(), node.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeNodeImageError(w, http.StatusBadRequest, "noPermission")
			return
		}
		s.logger.Error("node image upload: find root topic", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	maxUpload := s.cfg.NodeImageMaxUploadBytes
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+1024)
	file, header, err := r.FormFile(nodeImageFormField)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeNodeImageError(w, http.StatusRequestEntityTooLarge, "fileTooLarge")
			return
		}
		writeNodeImageError(w, http.StatusBadRequest, "noFileGiven")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeNodeImageError(w, http.StatusRequestEntityTooLarge, "fileTooLarge")
			return
		}
		s.logger.Error("node image upload: read", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}
	if len(data) == 0 {
		writeNodeImageError(w, http.StatusBadRequest, "noFileGiven")
		return
	}

	uploadedName := ""
	if header != nil {
		uploadedName = header.Filename
	}
	s.logger.Debug("node image upload: received",
		"node", node.ID,
		"root", rootID,
		"filename", uploadedName,
		"bytes", len(data),
		"max_upload_bytes", maxUpload,
	)

	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		s.logger.Debug("node image upload: decode failed", "err", err)
		writeNodeImageError(w, http.StatusBadRequest, "typeNotAllowed")
		return
	}
	ext, contentType, supported := nodeImageFormatMeta(format)
	if !supported {
		s.logger.Debug("node image upload: format not supported", "format", format)
		writeNodeImageError(w, http.StatusBadRequest, "typeNotAllowed")
		return
	}
	srcBounds := img.Bounds()
	srcW, srcH := srcBounds.Dx(), srcBounds.Dy()
	s.logger.Debug("node image upload: decoded",
		"format", format,
		"width", srcW,
		"height", srcH,
		"input_bytes", len(data),
	)

	storedData, storedExt, storedContentType := data, ext, contentType
	storedW, storedH := srcW, srcH
	if format == "gif" {
		// Animated GIFs can't survive a still-frame re-encode. Skip
		// compression and the resize — operators rely on the upload
		// ceiling to keep these in check.
		s.logger.Debug("node image upload: gif passthrough",
			"width", srcW,
			"height", srcH,
			"bytes", len(data),
		)
	} else {
		processed, pw, ph, err := s.compressNodeImage(img, srcW, srcH, len(data))
		if err != nil {
			s.logger.Error("node image upload: compress", "err", err)
			writeNodeImageError(w, http.StatusInternalServerError, "importError")
			return
		}
		if processed != nil {
			storedData = processed
			storedExt = ".jpg"
			storedContentType = "image/jpeg"
			storedW, storedH = pw, ph
		}
	}

	s.logger.Debug("node image upload: prepared for storage",
		"format_in", format,
		"format_out", storedContentType,
		"width_in", srcW,
		"height_in", srcH,
		"width_out", storedW,
		"height_out", storedH,
		"bytes_in", len(data),
		"bytes_out", len(storedData),
	)

	dir := filepath.Join(s.cfg.UploadDir, "topics", rootID.String())
	if err := os.MkdirAll(dir, nodeImageDirPerm); err != nil {
		s.logger.Error("node image upload: mkdir", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	name, err := randomImageName(storedExt)
	if err != nil {
		s.logger.Error("node image upload: name", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}
	storedPath := filepath.Join(dir, name)
	if err := os.WriteFile(storedPath, storedData, nodeImageUploadPerm); err != nil {
		s.logger.Error("node image upload: write", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	publicPath := "/uploads/topics/" + rootID.String() + "/" + name
	if _, err := s.queries.CreateNodeImage(r.Context(), db.CreateNodeImageParams{
		RootTopicID: rootID,
		UploadedBy:  user.ID,
		StoredPath:  publicPath,
		ContentType: storedContentType,
		ByteSize:    int64(len(storedData)),
	}); err != nil {
		// Best-effort filesystem cleanup so the DB and disk don't disagree.
		_ = os.Remove(storedPath)
		s.logger.Error("node image upload: insert", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	writeNodeImageSuccess(w, publicPath)
}

// compressNodeImage downsizes src to fit the configured max dimension and,
// if the resulting JPEG would still exceed the per-megapixel byte budget,
// steps the JPEG quality down until it does. Returns nil bytes when no
// re-encode is needed (input fits the dimension cap AND already fits the
// byte budget) — the caller then stores the original blob untouched.
func (s *Server) compressNodeImage(src image.Image, srcW, srcH, srcBytes int) ([]byte, int, int, error) {
	maxDim := s.cfg.NodeImageMaxDimension
	largest := srcW
	if srcH > largest {
		largest = srcH
	}

	targetBytes := byteBudgetForPixels(srcW*srcH, s.cfg.NodeImageBytesPerMegapixel)
	s.logger.Debug("node image upload: compression plan",
		"src_width", srcW,
		"src_height", srcH,
		"src_bytes", srcBytes,
		"max_dimension", maxDim,
		"bytes_per_megapixel", s.cfg.NodeImageBytesPerMegapixel,
		"target_bytes", targetBytes,
	)

	if largest <= maxDim && srcBytes <= targetBytes {
		s.logger.Debug("node image upload: skip recompress",
			"reason", "input within dimension and byte budget",
		)
		return nil, srcW, srcH, nil
	}

	dstW, dstH := srcW, srcH
	working := src
	if largest > maxDim {
		dstW, dstH = scaleToFit(srcW, srcH, maxDim)
		working = resizeImageNearest(src, dstW, dstH)
		s.logger.Debug("node image upload: resized",
			"from_width", srcW,
			"from_height", srcH,
			"to_width", dstW,
			"to_height", dstH,
		)
		targetBytes = byteBudgetForPixels(dstW*dstH, s.cfg.NodeImageBytesPerMegapixel)
	}

	rgba := flattenToOpaqueRGBA(working)
	for q := nodeImageJPEGQualityStart; q >= nodeImageJPEGQualityFloor; q -= nodeImageJPEGQualityStep {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, rgba, &jpeg.Options{Quality: q}); err != nil {
			return nil, 0, 0, err
		}
		s.logger.Debug("node image upload: jpeg encode step",
			"quality", q,
			"bytes", buf.Len(),
			"target_bytes", targetBytes,
			"width", dstW,
			"height", dstH,
		)
		if buf.Len() <= targetBytes || q == nodeImageJPEGQualityFloor {
			// Either the budget is met or we've hit the quality floor —
			// the floor path keeps the smallest version we produced.
			return buf.Bytes(), dstW, dstH, nil
		}
	}
	return nil, 0, 0, errors.New("jpeg encode produced no candidate")
}

// byteBudgetForPixels turns a pixel count and per-megapixel budget into a
// hard byte target, clamped to nodeImageMinTargetBytes so the floor never
// drops below "valid JPEG with a header".
func byteBudgetForPixels(pixels int, bytesPerMegapixel int64) int {
	target := int(int64(pixels) * bytesPerMegapixel / 1_000_000)
	if target < nodeImageMinTargetBytes {
		target = nodeImageMinTargetBytes
	}
	return target
}

// scaleToFit returns the dimensions that fit within the cap on the
// longest side while preserving aspect ratio. The result is at least 1x1.
func scaleToFit(w, h, cap int) (int, int) {
	if w <= 0 || h <= 0 {
		return w, h
	}
	largest := w
	if h > largest {
		largest = h
	}
	if largest <= cap {
		return w, h
	}
	factor := float64(cap) / float64(largest)
	dw := int(float64(w)*factor + 0.5)
	dh := int(float64(h)*factor + 0.5)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	return dw, dh
}

// nodeImageFormatMeta turns the goldmark/image-decoded format string into
// the file extension and Content-Type the rest of the pipeline expects.
// Any format not on this list is rejected so the upload directory only
// ever holds known image bytes.
func nodeImageFormatMeta(format string) (ext, contentType string, ok bool) {
	switch format {
	case "jpeg":
		return ".jpg", "image/jpeg", true
	case "png":
		return ".png", "image/png", true
	case "gif":
		return ".gif", "image/gif", true
	}
	return "", "", false
}

// randomImageName generates a 16-byte hex filename. Collisions are
// astronomically unlikely; the stored_path UNIQUE constraint catches them
// if they ever happen.
func randomImageName(ext string) (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]) + ext, nil
}

func writeNodeImageSuccess(w http.ResponseWriter, publicPath string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(nodeImageUploadResponse{
		Data: &nodeImageUploadData{FilePath: publicPath},
	})
}

func writeNodeImageError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(nodeImageUploadResponse{Error: code})
}
