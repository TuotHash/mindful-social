package server

import (
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

// handleFollow creates a follow edge from the current user to the user
// identified by the {username} URL parameter. Idempotent at the DB layer
// (CreateFollow does ON CONFLICT DO NOTHING) so a double-submit is harmless.
func (s *Server) handleFollow(w http.ResponseWriter, r *http.Request) {
	viewer := currentUser(r)
	target, ok := s.resolveProfileTarget(w, r, viewer)
	if !ok {
		return
	}
	if err := s.queries.CreateFollow(r.Context(), db.CreateFollowParams{
		FollowerID: viewer.ID,
		FollowedID: target.ID,
	}); err != nil {
		s.logger.Error("follow", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/users/"+target.Username, http.StatusSeeOther)
}

// handleUnfollow removes a follow edge from viewer to target.
func (s *Server) handleUnfollow(w http.ResponseWriter, r *http.Request) {
	viewer := currentUser(r)
	target, ok := s.resolveProfileTarget(w, r, viewer)
	if !ok {
		return
	}
	if err := s.queries.DeleteFollow(r.Context(), db.DeleteFollowParams{
		FollowerID: viewer.ID,
		FollowedID: target.ID,
	}); err != nil {
		s.logger.Error("unfollow", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/users/"+target.Username, http.StatusSeeOther)
}

// resolveProfileTarget loads the {username} from the URL and rejects
// self-follow attempts. Writes the response and returns ok=false on error.
func (s *Server) resolveProfileTarget(w http.ResponseWriter, r *http.Request, viewer *db.User) (db.User, bool) {
	username := chiURLParam(r, "username")
	if username == "" {
		http.NotFound(w, r)
		return db.User{}, false
	}
	target, err := s.queries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return db.User{}, false
		}
		s.logger.Error("resolve profile target", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return db.User{}, false
	}
	if target.ID == viewer.ID {
		http.Error(w, "cannot follow yourself", http.StatusBadRequest)
		return db.User{}, false
	}
	return target, true
}
