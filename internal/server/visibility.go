package server

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
)

// parseNodeVisibility decodes the form value coming from the visibility
// selector. Recognised forms:
//
//	"public"             → public
//	"connections"        → connections (mutual followers)
//	"group:<group-uuid>" → group (uuid must be one of the user's groups)
//	"private"            → private (author only)
//
// Returns (kind, groupID, errMsg). On any error errMsg is set and the
// id is nil. Audience Lists are gone; "list:..." values are rejected.
func parseNodeVisibility(raw string, _ uuid.UUID, groups []db.ListGroupsForUserRow) (db.VisibilityKind, *uuid.UUID, string) {
	switch raw {
	case "", "public":
		return db.VisibilityKindPublic, nil, ""
	case "connections":
		return db.VisibilityKindConnections, nil, ""
	case "private":
		return db.VisibilityKindPrivate, nil, ""
	}
	if strings.HasPrefix(raw, "group:") {
		idStr := strings.TrimPrefix(raw, "group:")
		id, err := uuid.Parse(idStr)
		if err != nil {
			return "", nil, "Invalid group selection."
		}
		for _, g := range groups {
			if g.ID == id {
				idCopy := id
				return db.VisibilityKindGroup, &idCopy, ""
			}
		}
		return "", nil, "Pick one of your groups."
	}
	return "", nil, "Invalid visibility option."
}

// formatNodeVisibility is the inverse of parseNodeVisibility: turns a
// stored (kind, group_id) pair back into the form value the selector
// expects.
func formatNodeVisibility(kind db.VisibilityKind, groupID *uuid.UUID) string {
	switch kind {
	case db.VisibilityKindGroup:
		if groupID == nil {
			return "public" // shouldn't happen — the CHECK constraint forbids it
		}
		return "group:" + groupID.String()
	case db.VisibilityKindConnections, db.VisibilityKindPrivate, db.VisibilityKindPublic:
		return string(kind)
	}
	return "public"
}

// canViewNode delegates to the SQL node_visible_to() function for use on
// the node-detail page where we fetch the row first and then decide
// whether to show it. Listings already filter at the DB layer. The SQL
// function is the source of truth because it also walks parent_node_id
// and intersects all ancestor visibility restrictions.
func (s *Server) canViewNode(ctx context.Context, node db.Node, viewerID *uuid.UUID) (bool, error) {
	return s.queries.CanViewNode(ctx, db.CanViewNodeParams{
		ID:       node.ID,
		ViewerID: viewerID,
	})
}
