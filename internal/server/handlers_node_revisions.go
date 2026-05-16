package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

// snapshotNodeRevision writes one row into node_revisions for the supplied
// content. Errors are only logged — failing to snapshot must never undo the
// user's actual save. tagNames should be the names currently attached to the
// node (i.e. the result of parseTagsInput applied to the form value).
func (s *Server) snapshotNodeRevision(
	ctx context.Context,
	nodeID uuid.UUID,
	editorID *uuid.UUID,
	title, body, summary string,
	tagNames []string,
) {
	if tagNames == nil {
		tagNames = []string{}
	}
	if _, err := s.queries.CreateNodeRevision(ctx, db.CreateNodeRevisionParams{
		NodeID:      nodeID,
		Title:       title,
		Body:        body,
		TagNames:    tagNames,
		EditedBy:    editorID,
		EditSummary: summary,
	}); err != nil {
		s.logger.Error("snapshot node revision", "err", err, "node_id", nodeID)
	}
}

// sameStringSet reports whether a and b contain the same elements ignoring
// order and duplicates. Used to detect no-op saves so we don't litter the
// history with identical revisions.
func sameStringSet(a, b []string) bool {
	set := make(map[string]struct{}, len(a))
	for _, s := range a {
		set[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := set[s]; !ok {
			return false
		}
		delete(set, s)
	}
	return len(set) == 0
}

// parseRevisionParam reads {revision} from the URL and validates it as a
// positive integer fitting in int32 (matching the schema column type).
func parseRevisionParam(r *http.Request) (int32, string) {
	raw := chiURLParam(r, "revision")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, "Invalid revision number."
	}
	return int32(n), ""
}

// handleNodeHistory renders the list of all revisions for a node, newest
// first. Visible to anyone who can view the node; the Revert button is gated
// on edit permission inside the template.
func (s *Server) handleNodeHistory(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	rows, err := s.queries.ListNodeRevisions(r.Context(), node.ID)
	if err != nil {
		s.logger.Error("list revisions", "err", err, "node_id", node.ID)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	user := currentUser(r)
	canEdit := false
	if user != nil {
		canEdit, _ = s.canEditNode(r.Context(), node, user)
	}
	revisions := make([]views.RevisionRow, 0, len(rows))
	for _, row := range rows {
		revisions = append(revisions, views.RevisionRow{
			Revision:       row.Revision,
			Title:          row.Title,
			TagNames:       row.TagNames,
			EditorUsername: row.EditorUsername,
			EditedAt:       row.EditedAt,
			EditSummary:    row.EditSummary,
		})
	}
	render(w, r, views.NodeHistory(viewerFor(user), node, revisions, canEdit))
}

// handleNodeRevisionView renders one historical snapshot — same body
// rendering as the live node page, with metadata about the editor and a
// revert button (if the viewer has edit rights).
func (s *Server) handleNodeRevisionView(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	rev, perr := parseRevisionParam(r)
	if perr != "" {
		http.Error(w, perr, http.StatusBadRequest)
		return
	}
	row, err := s.queries.GetNodeRevision(r.Context(), db.GetNodeRevisionParams{
		NodeID:   node.ID,
		Revision: rev,
	})
	if err != nil {
		http.NotFound(w, r)
		return
	}
	user := currentUser(r)
	canEdit := false
	if user != nil {
		canEdit, _ = s.canEditNode(r.Context(), node, user)
	}
	view := views.RevisionDetail{
		Revision:       row.Revision,
		Title:          row.Title,
		Body:           row.Body,
		TagNames:       row.TagNames,
		EditorUsername: row.EditorUsername,
		EditedAt:       row.EditedAt,
		EditSummary:    row.EditSummary,
	}
	render(w, r, views.NodeRevisionView(viewerFor(user), node, view, canEdit))
}

// handleNodeRevert rolls a node back to a prior revision by writing a NEW
// revision with the old content. Wiki-open: anyone with edit rights on the
// node can revert. The old revisions stay in the table — every change is
// monotonic, so reverts can themselves be reverted.
func (s *Server) handleNodeRevert(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	allowed, err := s.canEditNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("revert: policy check", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !allowed {
		http.Error(w, "You don't have permission to edit this node.", http.StatusForbidden)
		return
	}
	rev, perr := parseRevisionParam(r)
	if perr != "" {
		http.Error(w, perr, http.StatusBadRequest)
		return
	}
	row, err := s.queries.GetNodeRevision(r.Context(), db.GetNodeRevisionParams{
		NodeID:   node.ID,
		Revision: rev,
	})
	if err != nil {
		http.Error(w, "Revision not found.", http.StatusNotFound)
		return
	}
	// Slug, visibility, policies, group, and source_url are governance-level
	// or URL-stable and aren't tracked in revisions — preserve them as-is.
	if _, err := s.queries.UpdateNode(r.Context(), db.UpdateNodeParams{
		ID:                node.ID,
		Title:             row.Title,
		Body:              row.Body,
		SourceUrl:         node.SourceUrl,
		Visibility:        node.Visibility,
		VisibilityGroupID: node.VisibilityGroupID,
		GroupID:           node.GroupID,
		EditPolicy:        node.EditPolicy,
	}); err != nil {
		s.logger.Error("revert: update node", "err", err, "node_id", node.ID)
		http.Error(w, "Could not revert.", http.StatusInternalServerError)
		return
	}
	if err := s.setTagsForNode(r, node.ID, row.TagNames); err != nil {
		s.logger.Error("revert: set tags", "err", err, "node_id", node.ID)
	}
	summary := fmt.Sprintf("Reverted to revision %d.", rev)
	s.snapshotNodeRevision(r.Context(), node.ID, &user.ID, row.Title, row.Body, summary, row.TagNames)
	s.logger.Info("node reverted", "node_id", node.ID, "to_revision", rev, "user_id", user.ID)
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}
