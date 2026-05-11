package server

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
)

// canEditNode mirrors the SQL node_action_allowed(edit_policy, ...) check
// for an already-loaded node. Anonymous viewers fail immediately; the
// author and staff (moderator+admin) always pass; otherwise we resolve
// the policy. Kept in Go (vs. always calling the SQL helper) so handlers
// that already have the node loaded don't pay for an extra round-trip.
func (s *Server) canEditNode(ctx context.Context, node db.Node, viewer *db.User) (bool, error) {
	return s.evaluatePolicy(ctx, node.EditPolicy, node.CreatedBy, viewer)
}

// canLinkToNode is the link_policy equivalent of canEditNode.
func (s *Server) canLinkToNode(ctx context.Context, node db.Node, viewer *db.User) (bool, error) {
	return s.evaluatePolicy(ctx, node.LinkPolicy, node.CreatedBy, viewer)
}

// canDeleteNode is author-only by default but staff (moderators+admins)
// can also delete any node — that's the point of moderation.
func canDeleteNode(node db.Node, viewer *db.User) bool {
	if viewer == nil {
		return false
	}
	return viewer.ID == node.CreatedBy || isStaff(viewer)
}

// isStaff reports whether the viewer carries an admin-assigned curation
// role. Used both for permission bypass and for showing/hiding staff UI.
func isStaff(u *db.User) bool {
	if u == nil {
		return false
	}
	return u.Role == db.UserRoleModerator || u.Role == db.UserRoleAdmin
}

func (s *Server) evaluatePolicy(ctx context.Context, policy db.NodeActionPolicy, author uuid.UUID, viewer *db.User) (bool, error) {
	if viewer == nil {
		return false, nil
	}
	if viewer.ID == author {
		return true, nil
	}
	if isStaff(viewer) {
		return true, nil
	}
	switch policy {
	case db.NodeActionPolicyPublic:
		return true, nil
	case db.NodeActionPolicyConnections:
		state, err := s.queries.GetFollowState(ctx, db.GetFollowStateParams{
			ViewerID:  viewer.ID,
			ProfileID: author,
		})
		if err != nil {
			return false, err
		}
		return state.ViewerFollows && state.FollowsViewer, nil
	}
	return false, nil
}

// parseActionPolicy decodes the form value coming from the policy toggle.
// Empty / unrecognised values resolve to fallback so handlers can keep the
// existing policy unchanged when a non-author submits a form that doesn't
// include the field.
func parseActionPolicy(raw string, fallback db.NodeActionPolicy) (db.NodeActionPolicy, bool) {
	switch strings.TrimSpace(raw) {
	case "author":
		return db.NodeActionPolicyAuthor, true
	case "connections":
		return db.NodeActionPolicyConnections, true
	case "public":
		return db.NodeActionPolicyPublic, true
	}
	return fallback, false
}
