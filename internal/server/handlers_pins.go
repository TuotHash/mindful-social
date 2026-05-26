package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

// handlePinForm renders the "Pin to profile" page for a node. A pin is
// now a pure stance toggle (Support / Oppose / Resonate); attaching
// evidence happens through the typed-edge graph via the Connect form,
// not here.
func (s *Server) handlePinForm(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}

	current, hasCurrent := s.lookupPin(w, r, user.ID, node.ID)
	if !hasCurrent && current == nil {
		return
	}
	formKind := initialPinForm(node, current)

	if isHTMX(r) {
		render(w, r, views.PinModal(node, "", formKind))
		return
	}
	render(w, r, views.PinForm(viewerFor(user), node, "", formKind))
}

func (s *Server) handlePinSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	rawKind := strings.TrimSpace(r.PostFormValue("kind"))

	kind, kindErr := parsePinKind(rawKind, node.Type)
	if kindErr != "" {
		s.rerenderPinForm(w, r, user, node, kindErr, rawKind)
		return
	}

	if _, err := s.queries.SetPin(r.Context(), db.SetPinParams{
		UserID: user.ID,
		NodeID: node.ID,
		Kind:   kind,
	}); err != nil {
		s.logger.Error("pin set", "err", err)
		s.rerenderPinForm(w, r, user, node, "Could not save your pin. Please try again.", rawKind)
		return
	}
	nodeID := node.ID
	s.notifyBestEffort(r.Context(), node.CreatedBy, user.ID, notifKindPinOnNode, &nodeID)
	// htmx submits get a full-page navigation back to the node, which closes
	// the modal and shows the new banner. Non-htmx submits get the usual 303.
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/nodes/"+node.Slug)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

// handleStanceSet powers the three one-click buttons on a node page
// (Support / Oppose / Resonate). Clicking the button that matches the
// viewer's current pin removes it; clicking a different button replaces
// the existing pin (or creates one when there is none). All three kinds
// are mutually exclusive — there is at most one pin per (user, node).
func (s *Server) handleStanceSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	kind, kindErr := parsePinKind(strings.TrimSpace(r.PostFormValue("kind")), node.Type)
	if kindErr != "" {
		s.renderError(w, r, http.StatusBadRequest)
		return
	}

	current, ok := s.lookupPin(w, r, user.ID, node.ID)
	if !ok {
		return
	}

	if current != nil && current.Kind == kind {
		if _, err := s.queries.DeletePin(r.Context(), db.DeletePinParams{
			UserID: user.ID,
			NodeID: node.ID,
		}); err != nil {
			s.logger.Error("stance toggle off", "err", err)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
	} else {
		if _, err := s.queries.SetPin(r.Context(), db.SetPinParams{
			UserID: user.ID,
			NodeID: node.ID,
			Kind:   kind,
		}); err != nil {
			s.logger.Error("stance set", "err", err)
			s.renderError(w, r, http.StatusInternalServerError)
			return
		}
		nodeID := node.ID
		s.notifyBestEffort(r.Context(), node.CreatedBy, user.ID, notifKindPinOnNode, &nodeID)
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

func (s *Server) handlePinDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	rows, err := s.queries.DeletePin(r.Context(), db.DeletePinParams{
		UserID: user.ID,
		NodeID: node.ID,
	})
	if err != nil {
		s.logger.Error("pin delete", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	if rows == 0 {
		s.notFound(w, r)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

// parsePinKind validates the form kind value against the node type.
// Returns the typed kind on success, or a flash message on failure.
func parsePinKind(raw string, nodeType db.NodeType) (db.PinKind, string) {
	pk := db.PinKind(raw)
	if !pk.Valid() {
		return "", "Pick a valid stance."
	}
	if (pk == db.PinKindSupports || pk == db.PinKindOpposes) && nodeType != db.NodeTypeView {
		return "", "Support and oppose only apply to views. Use 'resonate' for other node types."
	}
	return pk, ""
}

// initialPinForm returns the kind to pre-select on the form. Uses the
// existing pin kind if present; otherwise defaults to supports for views
// and featured for all other node types.
func initialPinForm(node db.Node, current *db.GetPinForUserAndNodeRow) string {
	if current != nil {
		return string(current.Kind)
	}
	if node.Type == db.NodeTypeView {
		return string(db.PinKindSupports)
	}
	return string(db.PinKindFeatured)
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
		s.renderError(w, r, http.StatusInternalServerError)
		return nil, false
	}
}

func (s *Server) rerenderPinForm(w http.ResponseWriter, r *http.Request, user *db.User, node db.Node, flash, formKind string) {
	if isHTMX(r) {
		render(w, r, views.PinModal(node, flash, formKind))
		return
	}
	render(w, r, views.PinForm(viewerFor(user), node, flash, formKind))
}
