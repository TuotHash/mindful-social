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

// handleAdminUserEdit renders the per-user edit page. We look up the target
// user (404 on a bad/missing id) and pass through any flash/success messages
// from a previous POST.
func (s *Server) handleAdminUserEdit(w http.ResponseWriter, r *http.Request) {
	viewer := currentUser(r)
	target, ok := s.lookupAdminTarget(w, r)
	if !ok {
		return
	}
	flash := s.sessions.PopString(r.Context(), adminFlashKey)
	success := s.sessions.PopString(r.Context(), adminSuccessKey)
	render(w, r, views.AdminUserEdit(viewerFor(viewer), target, flash, success))
}

func (s *Server) handleAdminUpdateUsername(w http.ResponseWriter, r *http.Request) {
	target, ok := s.lookupAdminTarget(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := r.PostFormValue("username")
	if err := s.authSvc.AdminUpdateUsername(r.Context(), target.ID, username); err != nil {
		s.logger.Warn("admin update username", "target", target.ID, "err", err)
		s.flashAdmin(r, humanizeAuthErr(err))
		s.redirectAdminEdit(w, r, target.ID)
		return
	}
	s.successAdmin(r, "Username updated.")
	s.redirectAdminEdit(w, r, target.ID)
}

func (s *Server) handleAdminUpdateEmail(w http.ResponseWriter, r *http.Request) {
	target, ok := s.lookupAdminTarget(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	email := r.PostFormValue("email")
	if err := s.authSvc.AdminUpdateEmail(r.Context(), target.ID, email); err != nil {
		s.logger.Warn("admin update email", "target", target.ID, "err", err)
		s.flashAdmin(r, humanizeAuthErr(err))
		s.redirectAdminEdit(w, r, target.ID)
		return
	}
	s.successAdmin(r, "Email updated.")
	s.redirectAdminEdit(w, r, target.ID)
}

// handleAdminResetPassword sets a new password without verifying a previous
// one. Works for OAuth-only users too: in that case it creates a password
// identity instead of updating one.
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	target, ok := s.lookupAdminTarget(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	newPw := r.PostFormValue("new_password")
	confirm := r.PostFormValue("confirm_password")
	if newPw != confirm {
		s.flashAdmin(r, "The new password and confirmation don't match.")
		s.redirectAdminEdit(w, r, target.ID)
		return
	}
	if err := s.authSvc.AdminResetPassword(r.Context(), target.ID, newPw); err != nil {
		s.logger.Warn("admin reset password", "target", target.ID, "err", err)
		s.flashAdmin(r, humanizeAuthErr(err))
		s.redirectAdminEdit(w, r, target.ID)
		return
	}
	s.successAdmin(r, "Password reset. The user can now sign in with their email and the new password.")
	s.redirectAdminEdit(w, r, target.ID)
}

// lookupAdminTarget resolves the {id} URL param to a user and writes a 404 if
// it can't. Returns ok=false on any error so the handler returns early.
func (s *Server) lookupAdminTarget(w http.ResponseWriter, r *http.Request) (db.User, bool) {
	idStr := chiURLParam(r, "id")
	targetID, err := uuid.Parse(idStr)
	if err != nil {
		http.NotFound(w, r)
		return db.User{}, false
	}
	target, err := s.queries.GetUser(r.Context(), targetID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return db.User{}, false
		}
		s.logger.Error("admin: get user", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return db.User{}, false
	}
	return target, true
}

func (s *Server) redirectAdminEdit(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	http.Redirect(w, r, "/admin/users/"+id.String()+"/edit", http.StatusSeeOther)
}

func (s *Server) flashAdmin(r *http.Request, msg string) {
	s.sessions.Put(r.Context(), adminFlashKey, msg)
}

func (s *Server) successAdmin(r *http.Request, msg string) {
	s.sessions.Put(r.Context(), adminSuccessKey, msg)
}
