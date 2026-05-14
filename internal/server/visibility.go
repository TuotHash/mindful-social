package server

import (
	"context"
	"strings"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
)

// parseVisibility decodes the form value coming from the visibility selector
// and validates it against the user's lists. Recognised forms:
//
//	"public"             → public
//	"connections"        → connections (mutual followers)
//	"list:<list-uuid>"   → list (uuid must be one of the user's owned lists)
//	"private"            → private (author only)
//
// Returns (kind, listID, errMsg). On any error, errMsg is set and the other
// fields are zero-valued.
func parseVisibility(raw string, ownerID uuid.UUID, lists []db.AudienceList) (db.VisibilityKind, *uuid.UUID, string) {
	switch raw {
	case "", "public":
		return db.VisibilityKindPublic, nil, ""
	case "connections":
		return db.VisibilityKindConnections, nil, ""
	case "private":
		return db.VisibilityKindPrivate, nil, ""
	}
	if strings.HasPrefix(raw, "list:") {
		idStr := strings.TrimPrefix(raw, "list:")
		id, err := uuid.Parse(idStr)
		if err != nil {
			return "", nil, "Invalid list selection."
		}
		for _, l := range lists {
			if l.ID == id && l.OwnerID == ownerID {
				idCopy := id
				return db.VisibilityKindList, &idCopy, ""
			}
		}
		return "", nil, "Pick one of your own lists."
	}
	return "", nil, "Invalid visibility option."
}

// parseNodeVisibility extends parseVisibility with first-class group
// audiences. It returns both possible target ids; only one may be non-nil.
func parseNodeVisibility(raw string, ownerID uuid.UUID, lists []db.AudienceList, groups []db.ListGroupsForUserRow) (db.VisibilityKind, *uuid.UUID, *uuid.UUID, string) {
	if strings.HasPrefix(raw, "group:") {
		idStr := strings.TrimPrefix(raw, "group:")
		id, err := uuid.Parse(idStr)
		if err != nil {
			return "", nil, nil, "Invalid group selection."
		}
		for _, g := range groups {
			if g.ID == id {
				idCopy := id
				return db.VisibilityKindGroup, nil, &idCopy, ""
			}
		}
		return "", nil, nil, "Pick one of your groups."
	}
	kind, listID, msg := parseVisibility(raw, ownerID, lists)
	return kind, listID, nil, msg
}

// formatVisibility is the inverse of parseVisibility: turns a stored
// (kind, list_id) pair back into the form value the selector expects.
func formatVisibility(kind db.VisibilityKind, listID *uuid.UUID) string {
	switch kind {
	case db.VisibilityKindList:
		if listID == nil {
			return "public" // shouldn't happen — the CHECK constraint forbids it
		}
		return "list:" + listID.String()
	case db.VisibilityKindConnections, db.VisibilityKindPrivate, db.VisibilityKindPublic:
		return string(kind)
	}
	return "public"
}

func formatNodeVisibility(kind db.VisibilityKind, listID, groupID *uuid.UUID) string {
	if kind == db.VisibilityKindGroup {
		if groupID == nil {
			return "public"
		}
		return "group:" + groupID.String()
	}
	return formatVisibility(kind, listID)
}

// canViewNode delegates to the SQL node_visible_to() function for use on the
// node-detail page where we fetch the row first and then decide whether to
// show it. Listings already filter at the DB layer. The SQL function is the
// source of truth because it also walks parent_node_id and intersects all
// ancestor visibility restrictions.
func (s *Server) canViewNode(ctx context.Context, node db.Node, viewerID *uuid.UUID) (bool, error) {
	return s.queries.CanViewNode(ctx, db.CanViewNodeParams{
		ID:       node.ID,
		ViewerID: viewerID,
	})
}
