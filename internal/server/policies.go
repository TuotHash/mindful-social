package server

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

// canEditNode mirrors the SQL node_action_allowed(edit_policy, ...) check
// for an already-loaded node. Anonymous viewers fail immediately; the
// author and staff (moderator+admin) always pass; group editors / admins /
// owners can edit any node hosted in their group; otherwise we resolve
// the per-node policy.
func (s *Server) canEditNode(ctx context.Context, node db.Node, viewer *db.User) (bool, error) {
	if ok, err := s.canModerateNodeAsGroupStaff(ctx, node, viewer); ok || err != nil {
		return ok, err
	}
	return s.evaluatePolicy(ctx, node.EditPolicy, node.CreatedBy, viewer)
}

// canLinkToNode is the link_policy equivalent of canEditNode.
func (s *Server) canLinkToNode(ctx context.Context, node db.Node, viewer *db.User) (bool, error) {
	return s.evaluatePolicy(ctx, node.LinkPolicy, node.CreatedBy, viewer)
}

// canDeleteNode is author-only by default; site staff and the node's
// hosting-group editors / admins / owners can also delete it.
func (s *Server) canDeleteNode(ctx context.Context, node db.Node, viewer *db.User) (bool, error) {
	if viewer == nil {
		return false, nil
	}
	if viewer.ID == node.CreatedBy || isStaff(viewer) {
		return true, nil
	}
	return s.canModerateNodeAsGroupStaff(ctx, node, viewer)
}

// canModerateNodeAsGroupStaff returns true when the viewer is editor /
// admin / owner of the node's hosting group. Nodes without a group_id
// always fail. Used by canEditNode and canDeleteNode so group staff can
// curate the content posted inside their group.
func (s *Server) canModerateNodeAsGroupStaff(ctx context.Context, node db.Node, viewer *db.User) (bool, error) {
	if viewer == nil || node.GroupID == nil {
		return false, nil
	}
	role, err := s.viewerGroupRole(ctx, *node.GroupID, viewer.ID)
	if err != nil {
		return false, err
	}
	return groupRoleAtLeast(role, db.GroupMemberRoleEditor), nil
}

// viewerGroupRole returns the viewer's role in the given group, or the
// empty string when they aren't a member. Wraps the pgx ErrNoRows case so
// callers can treat "not a member" as a zero value without re-handling
// the sentinel.
func (s *Server) viewerGroupRole(ctx context.Context, groupID, viewerID uuid.UUID) (db.GroupMemberRole, error) {
	row, err := s.queries.GetGroupMembership(ctx, db.GetGroupMembershipParams{
		GroupID: groupID,
		UserID:  viewerID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return "", err
	}
	return row.Role, nil
}

// groupRoleRank assigns the privilege hierarchy a numeric weight so we
// can compare two roles with "is at least". The empty role (used for
// non-members) sorts below every named role.
func groupRoleRank(r db.GroupMemberRole) int {
	switch r {
	case db.GroupMemberRoleOwner:
		return 4
	case db.GroupMemberRoleAdmin:
		return 3
	case db.GroupMemberRoleEditor:
		return 2
	case db.GroupMemberRoleMember:
		return 1
	}
	return 0
}

// groupRoleAtLeast reports whether `have` is the same or more privileged
// than `need`. Non-members (empty role) fail every threshold.
func groupRoleAtLeast(have, need db.GroupMemberRole) bool {
	return have != "" && groupRoleRank(have) >= groupRoleRank(need)
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
