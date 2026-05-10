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

// handlePinForm renders the "Pin to profile" page for a node: kind selector,
// the reasonings already attached to the pin (if any), and a live-search
// picker over visible reasoning nodes to attach more.
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

	var attached []views.PinReasoning
	if current != nil {
		rs, err := s.queries.ListReasoningsForPin(r.Context(), current.ID)
		if err != nil {
			s.logger.Error("pin form: list reasonings", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		attached = pinReasoningsFromRows(rs)
	}

	candidates, err := s.searchReasoningCandidates(r, "")
	if err != nil {
		s.logger.Error("pin form: search reasonings", "err", err)
		candidates = nil
	}
	render(w, r, views.PinForm(viewerFor(user), node, "", formKind, "", nil, attached, candidates))
}

func (s *Server) handlePinSet(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rawKind := strings.TrimSpace(r.PostFormValue("kind"))
	rawReasoningIDs := r.PostForm["reasoning_ids"]
	rawFindReasoning := strings.TrimSpace(r.PostFormValue("find_reasoning"))

	kind, kindErr := parsePinKind(rawKind, node.Type)
	if kindErr != "" {
		s.rerenderPinForm(w, r, user, node, kindErr, rawKind, rawFindReasoning, rawReasoningIDs)
		return
	}

	reasoningUUIDs := make([]uuid.UUID, 0, len(rawReasoningIDs))
	for _, raw := range rawReasoningIDs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		rid, err := uuid.Parse(raw)
		if err != nil {
			s.rerenderPinForm(w, r, user, node, "Invalid reasoning selection.", rawKind, rawFindReasoning, rawReasoningIDs)
			return
		}
		reasoningUUIDs = append(reasoningUUIDs, rid)
	}
	// Support/oppose-only "reasonings" make no sense for non-view targets; we
	// silently drop them since the UI hides the picker for those.
	if node.Type != db.NodeTypeView {
		reasoningUUIDs = nil
	}

	pinID, err := s.queries.SetPin(r.Context(), db.SetPinParams{
		UserID: user.ID,
		NodeID: node.ID,
		Kind:   kind,
	})
	if err != nil {
		s.logger.Error("pin set", "err", err)
		s.rerenderPinForm(w, r, user, node, "Could not save your pin. Please try again.", rawKind, rawFindReasoning, rawReasoningIDs)
		return
	}
	if err := s.replacePinReasonings(r, pinID, reasoningUUIDs); err != nil {
		s.logger.Error("pin set: replace reasonings", "err", err)
		s.rerenderPinForm(w, r, user, node, "Saved the stance but could not update reasonings. Please try again.", rawKind, rawFindReasoning, rawReasoningIDs)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

func (s *Server) handlePinDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if err := s.queries.DeletePin(r.Context(), db.DeletePinParams{
		UserID: user.ID,
		NodeID: node.ID,
	}); err != nil {
		s.logger.Error("pin delete", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

// handleReasoningPicker is the HTMX endpoint that re-renders the reasoning
// candidate list as the user types in the search box on the pin form.
func (s *Server) handleReasoningPicker(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	find := strings.TrimSpace(r.URL.Query().Get("find_reasoning"))
	candidates, err := s.searchReasoningCandidates(r, find)
	if err != nil {
		s.logger.Error("reasoning picker", "err", err)
		candidates = nil
	}
	user := currentUser(r)
	// Pull the currently-attached reasonings so they're filtered out of the
	// candidate list — no value in offering an "attach" button for one the
	// user already has attached.
	var attached []views.PinReasoning
	if user != nil {
		current, hasCurrent := s.lookupPin(w, r, user.ID, node.ID)
		if !hasCurrent && current == nil {
			return
		}
		if current != nil {
			rs, err := s.queries.ListReasoningsForPin(r.Context(), current.ID)
			if err == nil {
				attached = pinReasoningsFromRows(rs)
			}
		}
	}
	selected := r.URL.Query()["reasoning_ids"]
	render(w, r, views.ReasoningCandidatePicker(find, selected, attached, candidates))
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
		http.Error(w, "internal error", http.StatusInternalServerError)
		return nil, false
	}
}

func (s *Server) rerenderPinForm(w http.ResponseWriter, r *http.Request, user *db.User, node db.Node, flash, formKind, findReasoning string, selectedIDs []string) {
	current, hasCurrent := s.lookupPin(w, r, user.ID, node.ID)
	if !hasCurrent && current == nil {
		return
	}
	var attached []views.PinReasoning
	if current != nil {
		if rs, err := s.queries.ListReasoningsForPin(r.Context(), current.ID); err == nil {
			attached = pinReasoningsFromRows(rs)
		}
	}
	candidates, _ := s.searchReasoningCandidates(r, findReasoning)
	render(w, r, views.PinForm(viewerFor(user), node, flash, formKind, findReasoning, selectedIDs, attached, candidates))
}

// searchReasoningCandidates returns visible reasoning nodes matching the
// given query (or recent reasonings when query is empty).
func (s *Server) searchReasoningCandidates(r *http.Request, find string) ([]views.ReasoningCandidate, error) {
	rows, err := s.queries.SearchReasonings(r.Context(), db.SearchReasoningsParams{
		Query:    find,
		ViewerID: viewerID(r),
	})
	if err != nil {
		return nil, err
	}
	out := make([]views.ReasoningCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ReasoningCandidate{ID: row.ID, Slug: row.Slug, Title: row.Title})
	}
	return out, nil
}

// replacePinReasonings clears the pin's existing reasonings and inserts the
// new set. Not transactional — a partial failure leaves the pin in an
// inconsistent state, but the user can retry by re-submitting the form.
func (s *Server) replacePinReasonings(r *http.Request, pinID uuid.UUID, reasoningIDs []uuid.UUID) error {
	if err := s.queries.DeleteReasoningsForPin(r.Context(), pinID); err != nil {
		return err
	}
	for _, rid := range reasoningIDs {
		if err := s.queries.AddPinReasoning(r.Context(), db.AddPinReasoningParams{
			PinID:       pinID,
			ReasoningID: rid,
		}); err != nil {
			return err
		}
	}
	return nil
}

func pinReasoningsFromRows(rows []db.ListReasoningsForPinRow) []views.PinReasoning {
	out := make([]views.PinReasoning, 0, len(rows))
	for _, r := range rows {
		out = append(out, views.PinReasoning{ID: r.ID, Slug: r.Slug, Title: r.Title})
	}
	return out
}
