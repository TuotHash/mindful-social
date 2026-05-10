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

// canViewNode mirrors the SQL node_visible_to() function for use on the
// node-detail page where we fetch the row first and then decide whether to
// show it. Listings already filter at the DB layer; this helper exists so
// the detail page doesn't need a second round-trip.
//
// viewerID is nil for logged-out visitors. The author can always see their
// own nodes regardless of visibility.
func (s *Server) canViewNode(ctx context.Context, node db.Node, viewerID *uuid.UUID) (bool, error) {
	if node.Visibility == db.VisibilityKindPublic {
		return true, nil
	}
	if viewerID == nil {
		return false, nil
	}
	if *viewerID == node.CreatedBy {
		return true, nil
	}
	switch node.Visibility {
	case db.VisibilityKindConnections:
		state, err := s.queries.GetFollowState(ctx, db.GetFollowStateParams{
			ViewerID:  *viewerID,
			ProfileID: node.CreatedBy,
		})
		if err != nil {
			return false, err
		}
		return state.ViewerFollows && state.FollowsViewer, nil
	case db.VisibilityKindList:
		if node.VisibilityListID == nil {
			return false, nil
		}
		row, err := s.queries.IsListMember(ctx, db.IsListMemberParams{
			ListID:       *node.VisibilityListID,
			MemberUserID: *viewerID,
		})
		if err != nil {
			return false, err
		}
		return row, nil
	case db.VisibilityKindPrivate:
		return false, nil
	}
	return false, nil
}
