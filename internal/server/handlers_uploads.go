package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// handleUpload serves bytes from cfg.UploadDir with a visibility check
// keyed on the URL space. Bytes live under three scopes:
//
//   - profiles/<file>           — public; intentionally cacheable
//   - topics/<root>/<file>      — gated by canViewNode on <root>
//   - drafts/<file>             — gated by "viewer is logged in"
//
// Anything else 404s. We never let chi's static FileServer touch uploads
// because that handler had no idea about node visibility and stamped a
// public 1-day cache on every response, leaking private-node assets to
// anyone who learned the URL.
//
// Every response also carries a strict sandbox CSP so even a polyglot
// file served as image/gif can't execute script if a browser ignores
// nosniff (older clients, embedded contexts).
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/uploads/")
	if rel == "" || strings.Contains(rel, "..") {
		s.notFound(w, r)
		return
	}
	parts := strings.SplitN(rel, "/", 2)
	scope := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = parts[1]
	}

	switch scope {
	case "profiles":
		// Profile images are intentionally public — they render in author
		// chips and avatars on every page including logged-out views.
		s.serveUpload(w, r, rel, uploadCachePublic)

	case "topics":
		if rest == "" {
			s.notFound(w, r)
			return
		}
		subParts := strings.SplitN(rest, "/", 2)
		rootID, err := uuid.Parse(subParts[0])
		if err != nil {
			s.notFound(w, r)
			return
		}
		node, err := s.queries.GetNode(r.Context(), rootID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				s.notFound(w, r)
				return
			}
			s.logger.Error("upload: load root node", "err", err, "root_id", rootID)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
		visible, err := s.canViewNode(r.Context(), node, viewerID(r))
		if err != nil {
			s.logger.Error("upload: visibility", "err", err, "root_id", rootID)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
		if !visible {
			s.notFound(w, r)
			return
		}
		s.serveUpload(w, r, rel, uploadCachePrivate)

	case "drafts":
		// Drafts have no node association at upload time, so a per-node
		// visibility check isn't possible. Gate on "logged in" as a soft
		// floor and avoid the public CDN cache. Long term we should bind
		// drafts to their uploader so only that user (and downstream
		// viewers of the published node) can fetch them.
		if currentUser(r) == nil {
			s.notFound(w, r)
			return
		}
		s.serveUpload(w, r, rel, uploadCachePrivate)

	default:
		s.notFound(w, r)
	}
}

type uploadCache int

const (
	uploadCachePublic uploadCache = iota
	uploadCachePrivate
)

func (s *Server) serveUpload(w http.ResponseWriter, r *http.Request, rel string, cache uploadCache) {
	// filepath.Clean rejects path traversal; we already guarded against ".."
	// in the caller, but defence in depth is cheap. The trailing slash
	// stripping prevents serving a directory listing if the cleaned name
	// happens to resolve to one.
	clean := filepath.Clean(rel)
	if clean != rel || strings.HasPrefix(clean, "/") || strings.HasPrefix(clean, "..") {
		s.notFound(w, r)
		return
	}
	full := filepath.Join(s.cfg.UploadDir, filepath.FromSlash(clean))
	info, err := os.Stat(full)
	if err != nil || info.IsDir() {
		s.notFound(w, r)
		return
	}

	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	// Sandbox CSP prevents an attacker-controlled byte stream served from
	// our origin from executing script, even if a browser sniffs the
	// Content-Type. default-src 'none' shuts off subresource loading too.
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	switch cache {
	case uploadCachePublic:
		h.Set("Cache-Control", "public, max-age=86400")
	case uploadCachePrivate:
		h.Set("Cache-Control", "private, no-store")
	}

	http.ServeFile(w, r, full)
}
