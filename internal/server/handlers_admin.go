package server

import (
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	adminFlashKey   = "admin_flash"
	adminSuccessKey = "admin_success"
)

// handleAdminIndex is the admin landing — currently just the user roster
// with role-change controls. Future admin features (mod queue, site
// settings) will live alongside it.
func (s *Server) handleAdminIndex(w http.ResponseWriter, r *http.Request) {
	viewer := currentUser(r)
	users, err := s.queries.ListUsersForAdmin(r.Context())
	if err != nil {
		s.logger.Error("admin: list users", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]views.AdminUserRow, 0, len(users))
	for _, u := range users {
		rows = append(rows, views.AdminUserRow{
			ID:       u.ID,
			Username: u.Username,
			Email:    u.Email,
			Role:     u.Role,
			IsSelf:   u.ID == viewer.ID,
		})
	}
	flash := s.sessions.PopString(r.Context(), adminFlashKey)
	success := s.sessions.PopString(r.Context(), adminSuccessKey)
	render(w, r, views.AdminUsers(viewerFor(viewer), rows, flash, success))
}

// handleAdminSetRole updates one user's role. Admins can't demote
// themselves — that's a quick way to lock the instance out of all admin
// access, and the env-var bootstrap is the only fallback. Promotion from
// the UI is otherwise unconstrained.
func (s *Server) handleAdminSetRole(w http.ResponseWriter, r *http.Request) {
	viewer := currentUser(r)
	idStr := chiURLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	role := db.UserRole(r.PostFormValue("role"))
	if !role.Valid() {
		s.flashAdmin(r, "Invalid role.")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	target, err := s.queries.GetUser(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("admin set role: get user", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if target.ID == viewer.ID && role != db.UserRoleAdmin {
		s.flashAdmin(r, "You can't demote yourself. Have another admin do it.")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	if err := s.queries.UpdateUserRole(r.Context(), db.UpdateUserRoleParams{
		ID:   target.ID,
		Role: role,
	}); err != nil {
		s.logger.Error("admin set role", "err", err)
		s.flashAdmin(r, "Could not update role. Please try again.")
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.successAdmin(r, "Updated "+target.Username+"'s role to "+string(role)+".")
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

func (s *Server) flashAdmin(r *http.Request, msg string) {
	s.sessions.Put(r.Context(), adminFlashKey, msg)
}

func (s *Server) successAdmin(r *http.Request, msg string) {
	s.sessions.Put(r.Context(), adminSuccessKey, msg)
}
