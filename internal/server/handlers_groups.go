package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	maxGroupNameLen        = 80
	maxGroupDescriptionLen = 600
)

func (s *Server) handleGroupsIndex(w http.ResponseWriter, r *http.Request) {
	rows, err := s.queries.ListVisibleGroups(r.Context(), viewerID(r))
	if err != nil {
		s.logger.Error("groups index", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.GroupsIndex(viewerFor(currentUser(r)), "", rows))
}

func (s *Server) handleGroupCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	description := strings.TrimSpace(r.PostFormValue("description"))
	visibility := parseGroupVisibility(r.PostFormValue("visibility"))

	rerender := func(flash string) {
		rows, err := s.queries.ListVisibleGroups(r.Context(), viewerID(r))
		if err != nil {
			s.logger.Error("groups create rerender", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		render(w, r, views.GroupsIndex(viewerFor(user), flash, rows))
	}

	switch {
	case name == "":
		rerender("Pick a name for the group.")
		return
	case len(name) > maxGroupNameLen:
		rerender("Group name is too long.")
		return
	case len(description) > maxGroupDescriptionLen:
		rerender("Group description is too long.")
		return
	}

	slug, err := s.uniqueGroupSlug(r.Context(), slugify(name))
	if err != nil {
		s.logger.Error("group create: unique slug", "err", err)
		rerender("Could not create group. Please try again.")
		return
	}

	tx, err := s.db.Begin(r.Context())
	if err != nil {
		s.logger.Error("group create: begin tx", "err", err)
		rerender("Could not create group. Please try again.")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := s.queries.WithTx(tx)

	group, err := qtx.CreateGroup(r.Context(), db.CreateGroupParams{
		Slug:        slug,
		Name:        name,
		Description: description,
		OwnerID:     user.ID,
		Visibility:  visibility,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			rerender("A group with that name already exists.")
			return
		}
		s.logger.Error("group create", "err", err)
		rerender("Could not create group. Please try again.")
		return
	}
	if err := qtx.AddGroupMember(r.Context(), db.AddGroupMemberParams{
		GroupID: group.ID,
		UserID:  user.ID,
		Role:    db.GroupMemberRoleOwner,
	}); err != nil {
		s.logger.Error("group create: add owner", "err", err)
		rerender("Could not create group. Please try again.")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		s.logger.Error("group create: commit", "err", err)
		rerender("Could not create group. Please try again.")
		return
	}
	http.Redirect(w, r, "/groups/"+group.Slug, http.StatusSeeOther)
}

func (s *Server) handleGroupDetail(w http.ResponseWriter, r *http.Request) {
	group, membership, ok := s.resolveGroup(w, r)
	if !ok {
		return
	}
	s.renderGroupDetail(w, r, group, membership, "")
}

func (s *Server) handleGroupJoin(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	group, membership, ok := s.resolveGroup(w, r)
	if !ok {
		return
	}
	if membership != nil {
		http.Redirect(w, r, "/groups/"+group.Slug, http.StatusSeeOther)
		return
	}
	if group.Visibility != db.GroupVisibilityKindPublic {
		s.renderGroupDetail(w, r, group, membership, "This group is invite-only.")
		return
	}
	if err := s.queries.AddGroupMember(r.Context(), db.AddGroupMemberParams{
		GroupID: group.ID,
		UserID:  user.ID,
		Role:    db.GroupMemberRoleMember,
	}); err != nil {
		s.logger.Error("group join", "err", err)
		s.renderGroupDetail(w, r, group, membership, "Could not join group. Please try again.")
		return
	}
	http.Redirect(w, r, "/groups/"+group.Slug, http.StatusSeeOther)
}

func (s *Server) handleGroupLeave(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	group, membership, ok := s.resolveGroup(w, r)
	if !ok {
		return
	}
	if membership == nil {
		http.Redirect(w, r, "/groups/"+group.Slug, http.StatusSeeOther)
		return
	}
	if membership.Role == db.GroupMemberRoleOwner {
		s.renderGroupDetail(w, r, group, membership, "Owners can't leave their own group.")
		return
	}
	if err := s.queries.RemoveGroupMember(r.Context(), db.RemoveGroupMemberParams{
		GroupID: group.ID,
		UserID:  user.ID,
	}); err != nil {
		s.logger.Error("group leave", "err", err)
		s.renderGroupDetail(w, r, group, membership, "Could not leave group. Please try again.")
		return
	}
	http.Redirect(w, r, "/groups/"+group.Slug, http.StatusSeeOther)
}

func (s *Server) handleGroupAddMember(w http.ResponseWriter, r *http.Request) {
	group, membership, ok := s.resolveGroup(w, r)
	if !ok {
		return
	}
	if !canManageGroup(membership) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	if username == "" {
		s.renderGroupDetail(w, r, group, membership, "Enter a username.")
		return
	}
	target, err := s.queries.GetUserByUsername(r.Context(), username)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.renderGroupDetail(w, r, group, membership, "No user with that username.")
			return
		}
		s.logger.Error("group add member: lookup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := s.queries.AddGroupMember(r.Context(), db.AddGroupMemberParams{
		GroupID: group.ID,
		UserID:  target.ID,
		Role:    db.GroupMemberRoleMember,
	}); err != nil {
		s.logger.Error("group add member", "err", err)
		s.renderGroupDetail(w, r, group, membership, "Could not add member. Please try again.")
		return
	}
	http.Redirect(w, r, "/groups/"+group.Slug, http.StatusSeeOther)
}

func (s *Server) handleGroupRemoveMember(w http.ResponseWriter, r *http.Request) {
	group, membership, ok := s.resolveGroup(w, r)
	if !ok {
		return
	}
	if !canManageGroup(membership) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	memberID, err := uuid.Parse(chiURLParam(r, "userID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.RemoveGroupMember(r.Context(), db.RemoveGroupMemberParams{
		GroupID: group.ID,
		UserID:  memberID,
	}); err != nil {
		s.logger.Error("group remove member", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/groups/"+group.Slug, http.StatusSeeOther)
}

func parseGroupVisibility(raw string) db.GroupVisibilityKind {
	switch strings.TrimSpace(raw) {
	case "public":
		return db.GroupVisibilityKindPublic
	case "closed":
		return db.GroupVisibilityKindClosed
	default:
		return db.GroupVisibilityKindInvite
	}
}

func canManageGroup(membership *db.GroupMembership) bool {
	return membership != nil && (membership.Role == db.GroupMemberRoleOwner || membership.Role == db.GroupMemberRoleAdmin)
}

func (s *Server) resolveGroup(w http.ResponseWriter, r *http.Request) (db.Group, *db.GroupMembership, bool) {
	slug := strings.TrimSpace(chiURLParam(r, "slug"))
	if slug == "" {
		http.NotFound(w, r)
		return db.Group{}, nil, false
	}
	group, err := s.queries.GetGroupBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return db.Group{}, nil, false
		}
		s.logger.Error("resolve group", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return db.Group{}, nil, false
	}
	var membership *db.GroupMembership
	if user := currentUser(r); user != nil {
		row, err := s.queries.GetGroupMembership(r.Context(), db.GetGroupMembershipParams{
			GroupID: group.ID,
			UserID:  user.ID,
		})
		if err == nil {
			membership = &row
		} else if !errors.Is(err, pgx.ErrNoRows) {
			s.logger.Error("resolve group: membership", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return db.Group{}, nil, false
		}
	}
	if group.Visibility == db.GroupVisibilityKindClosed && membership == nil {
		http.NotFound(w, r)
		return db.Group{}, nil, false
	}
	return group, membership, true
}

func (s *Server) renderGroupDetail(w http.ResponseWriter, r *http.Request, group db.Group, membership *db.GroupMembership, flash string) {
	members, err := s.queries.ListGroupMembers(r.Context(), group.ID)
	if err != nil {
		s.logger.Error("group detail: members", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nodes, err := s.queries.ListNodesForGroupForViewer(r.Context(), db.ListNodesForGroupForViewerParams{
		GroupID:     group.ID,
		ViewerID:    viewerID(r),
		ResultLimit: 50,
	})
	if err != nil {
		s.logger.Error("group detail: nodes", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.GroupDetail(viewerFor(currentUser(r)), flash, group, membership, members, nodes, canManageGroup(membership)))
}
