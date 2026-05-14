package server

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

const (
	maxNodeImageBytes   = 8 << 20
	nodeImageFormField  = "image"
	nodeImageUploadPerm = 0o644
	nodeImageDirPerm    = 0o755
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

	r.Body = http.MaxBytesReader(w, r.Body, maxNodeImageBytes+1024)
	file, _, err := r.FormFile(nodeImageFormField)
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

	_, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		writeNodeImageError(w, http.StatusBadRequest, "typeNotAllowed")
		return
	}
	ext, contentType, supported := nodeImageFormatMeta(format)
	if !supported {
		writeNodeImageError(w, http.StatusBadRequest, "typeNotAllowed")
		return
	}

	dir := filepath.Join(s.cfg.UploadDir, "topics", rootID.String())
	if err := os.MkdirAll(dir, nodeImageDirPerm); err != nil {
		s.logger.Error("node image upload: mkdir", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	name, err := randomImageName(ext)
	if err != nil {
		s.logger.Error("node image upload: name", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}
	storedPath := filepath.Join(dir, name)
	if err := os.WriteFile(storedPath, data, nodeImageUploadPerm); err != nil {
		s.logger.Error("node image upload: write", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	publicPath := "/uploads/topics/" + rootID.String() + "/" + name
	if _, err := s.queries.CreateNodeImage(r.Context(), db.CreateNodeImageParams{
		RootTopicID: rootID,
		UploadedBy:  user.ID,
		StoredPath:  publicPath,
		ContentType: contentType,
		ByteSize:    int64(len(data)),
	}); err != nil {
		// Best-effort filesystem cleanup so the DB and disk don't disagree.
		_ = os.Remove(storedPath)
		s.logger.Error("node image upload: insert", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	writeNodeImageSuccess(w, publicPath)
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
