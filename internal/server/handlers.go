package server

import (
	"net/http"
	"time"

	"github.com/a-h/templ"
	"github.com/go-chi/chi/v5"
)

const oauthExchangeTimeout = 10 * time.Second

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.db.Ping(r.Context()); err != nil {
		s.logger.Error("healthz: db ping failed", "err", err)
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok\n"))
}

// render is a stateless helper for handlers that need to write a templ
// component as the response. Keeping it here means feature-specific files
// don't have to import templ directly.
func render(w http.ResponseWriter, r *http.Request, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Render writes directly into w. If it fails mid-stream the response
	// will already be partially flushed, so there's nothing useful we can
	// recover; the request log will record the request id.
	_ = c.Render(r.Context(), w)
}

// chiURLParam wraps chi.URLParam so feature-specific files don't have to
// import chi just to read a path variable.
func chiURLParam(r *http.Request, key string) string {
	return chi.URLParam(r, key)
}
