package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/auth"
	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

type ctxKey int

const (
	ctxUserKey ctxKey = iota
)

// loadUser pulls the current user from the session (if any) and stashes a
// *db.User in the request context. Routes that don't need auth still see
// nil cleanly via currentUser().
func (s *Server) loadUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := auth.CurrentUserID(r.Context(), s.sessions)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.queries.GetUser(r.Context(), id)
		if err != nil {
			// Session points at a deleted user — destroy the session so
			// the loop self-heals on the next request.
			if errors.Is(err, pgx.ErrNoRows) {
				_ = auth.LogoutUser(r.Context(), s.sessions)
				next.ServeHTTP(w, r)
				return
			}
			s.logger.Error("loadUser: db", "err", err)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
		ctx := context.WithValue(r.Context(), ctxUserKey, &u)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireUser bounces unauthenticated requests to /login.
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if currentUser(r) == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireAdmin gates admin-only routes. Returns 404 (not 403) so the route
// is invisible to non-admins — they can't tell whether the path exists.
func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := currentUser(r)
		if u == nil || u.Role != db.UserRoleAdmin {
			s.notFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func currentUser(r *http.Request) *db.User {
	u, _ := r.Context().Value(ctxUserKey).(*db.User)
	return u
}

// viewerID returns the current user's id as a pointer suitable for the
// nullable viewer_id parameter on visibility-aware queries. nil when the
// request is anonymous.
func viewerID(r *http.Request) *uuid.UUID {
	if u := currentUser(r); u != nil {
		id := u.ID
		return &id
	}
	return nil
}

// recoverer catches panics from downstream handlers, logs them through
// s.logger with the request id and stack, and writes a 500. Replaces
// chi's middleware.Recoverer, which dumps a colored stack to stderr and
// bypasses the structured log stream entirely. http.ErrAbortHandler is
// re-panicked so the server still aborts the connection as intended.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			rvr := recover()
			if rvr == nil {
				return
			}
			if rvr == http.ErrAbortHandler {
				panic(rvr)
			}
			s.logger.Error("panic recovered",
				"panic", rvr,
				"method", r.Method,
				"path", r.URL.Path,
				"request_id", chimw.GetReqID(r.Context()),
				"stack", string(debug.Stack()),
			)
			if r.Header.Get("Connection") != "Upgrade" {
				s.renderError(w, r, http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLogger emits one structured log line per request. It runs after
// loadUser so user_id is available in context. 4xx responses log at Warn,
// 5xx at Error, everything else at Info.
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		attrs := []any{
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", chimw.GetReqID(r.Context()),
		}
		if u := currentUser(r); u != nil {
			attrs = append(attrs, "user_id", u.ID)
		}

		level := slog.LevelInfo
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		s.logger.Log(r.Context(), level, "request", attrs...)
	})
}

// viewerFor turns a *db.User into the slim Viewer struct templates render.
// Returning nil for nil keeps call sites tidy.
func viewerFor(u *db.User) *views.Viewer {
	if u == nil {
		return nil
	}
	return &views.Viewer{
		ID:               u.ID.String(),
		Username:         u.Username,
		ProfileImagePath: u.ProfileImagePath,
		IsAdmin:          u.Role == db.UserRoleAdmin,
		IsStaff:          u.Role == db.UserRoleAdmin || u.Role == db.UserRoleModerator,
		Timezone:         u.Timezone,
	}
}
