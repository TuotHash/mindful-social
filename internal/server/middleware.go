package server

import (
	"context"
	"errors"
	"net/http"

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
			http.Error(w, "internal error", http.StatusInternalServerError)
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

func currentUser(r *http.Request) *db.User {
	u, _ := r.Context().Value(ctxUserKey).(*db.User)
	return u
}

// viewerFor turns a *db.User into the slim Viewer struct templates render.
// Returning nil for nil keeps call sites tidy.
func viewerFor(u *db.User) *views.Viewer {
	if u == nil {
		return nil
	}
	return &views.Viewer{ID: u.ID.String(), Username: u.Username}
}

