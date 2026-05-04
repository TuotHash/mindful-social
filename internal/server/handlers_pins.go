package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/mindful-social/mindful-social/internal/db"
	"github.com/mindful-social/mindful-social/internal/views"
)

// handlePinForm renders the "Pin to profile" page for a node: kind selector
// (Support / Oppose / Feature for views; Feature only for other types) plus
// an optional reasoning picker drawn from the user's authored reasonings.
// If a pin already exists, the form is pre-filled with the current values so
// the same page works for "set" and "change".
func (s *Server) handlePinForm(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	node, err := s.queries.GetNode(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("pin form: get node", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	current, hasCurrent := s.lookupPin(w, r, user.ID, id)
	if !hasCurrent && current == nil {
		// lookupPin already wrote an error response.
		return
	}
	formKind, formReasoningID := initialPinForm(node, current)

	reasonings, err := s.queries.ListReasoningsAuthoredBy(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("pin form: list reasonings", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.PinForm(viewerFor(user), node, "", formKind, formReasoningID, reasoningOptions(reasonings)))
}

func (s *Server) handlePinSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	node, err := s.queries.GetNode(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("pin set: get node", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rawKind := strings.TrimSpace(r.PostFormValue("kind"))
	rawReasoningID := strings.TrimSpace(r.PostFormValue("reasoning_id"))

	kind, kindErr := parsePinKind(rawKind, node.Type)
	if kindErr != "" {
		s.rerenderPinForm(w, r, user, node, kindErr, rawKind, rawReasoningID)
		return
	}
	var reasoningPtr *uuid.UUID
	if rawReasoningID != "" {
		rid, err := uuid.Parse(rawReasoningID)
		if err != nil {
			s.rerenderPinForm(w, r, user, node, "Pick a valid reasoning, or none.", rawKind, rawReasoningID)
			return
		}
		reasoningPtr = &rid
	}

	if err := s.queries.SetPin(r.Context(), db.SetPinParams{
		UserID:      user.ID,
		NodeID:      id,
		Kind:        kind,
		ReasoningID: reasoningPtr,
	}); err != nil {
		s.logger.Error("pin set", "err", err)
		s.rerenderPinForm(w, r, user, node, "Could not save your pin. Please try again.", rawKind, rawReasoningID)
		return
	}
	http.Redirect(w, r, "/nodes/"+id.String(), http.StatusSeeOther)
}

func (s *Server) handlePinDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	id, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.DeletePin(r.Context(), db.DeletePinParams{
		UserID: user.ID,
		NodeID: id,
	}); err != nil {
		s.logger.Error("pin delete", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+id.String(), http.StatusSeeOther)
}

// parsePinKind validates the form kind value against the node type.
// Returns the typed kind on success, or a flash message on failure.
func parsePinKind(raw string, nodeType db.NodeType) (db.PinKind, string) {
	pk := db.PinKind(raw)
	if !pk.Valid() {
		return "", "Pick a valid stance."
	}
	if (pk == db.PinKindSupports || pk == db.PinKindOpposes) && nodeType != db.NodeTypeView {
		return "", "Support and oppose only apply to views. Use 'feature' for other node types."
	}
	return pk, ""
}

// initialPinForm picks the kind and reasoning to pre-select on the form. If
// the user already pinned this node, use those; otherwise default to a
// type-appropriate kind (supports for views, featured for other types).
func initialPinForm(node db.Node, current *db.GetPinForUserAndNodeRow) (string, string) {
	if current != nil {
		rid := ""
		if current.ReasoningID != nil {
			rid = current.ReasoningID.String()
		}
		return string(current.Kind), rid
	}
	if node.Type == db.NodeTypeView {
		return string(db.PinKindSupports), ""
	}
	return string(db.PinKindFeatured), ""
}

// lookupPin returns the user's current pin on a node (nil if none).
// Returns (nil, true) when the user is not pinned; (nil, false) on a real
// error (in which case it has already written the error response).
func (s *Server) lookupPin(w http.ResponseWriter, r *http.Request, userID, nodeID uuid.UUID) (*db.GetPinForUserAndNodeRow, bool) {
	row, err := s.queries.GetPinForUserAndNode(r.Context(), db.GetPinForUserAndNodeParams{
		UserID: userID,
		NodeID: nodeID,
	})
	switch {
	case err == nil:
		return &row, true
	case errors.Is(err, pgx.ErrNoRows):
		return nil, true
	default:
		s.logger.Error("pin lookup", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
}

func (s *Server) rerenderPinForm(w http.ResponseWriter, r *http.Request, user *db.User, node db.Node, flash, formKind, formReasoningID string) {
	reasonings, err := s.queries.ListReasoningsAuthoredBy(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("pin rerender: list reasonings", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.PinForm(viewerFor(user), node, flash, formKind, formReasoningID, reasoningOptions(reasonings)))
}

func reasoningOptions(rows []db.ListReasoningsAuthoredByRow) []views.ReasoningOption {
	out := make([]views.ReasoningOption, 0, len(rows))
	for _, r := range rows {
		out = append(out, views.ReasoningOption{ID: r.ID, Title: r.Title})
	}
	return out
}
