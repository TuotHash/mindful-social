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
// the findings already attached to the pin (if any), and a live-search
// picker over visible finding nodes to attach more.
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

	var attached []views.PinFinding
	if current != nil {
		rs, err := s.queries.ListFindingsForPin(r.Context(), db.ListFindingsForPinParams{
			PinID:    current.ID,
			ViewerID: viewerID(r),
		})
		if err != nil {
			s.logger.Error("pin form: list findings", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		attached = pinFindingsFromRows(rs)
	}

	candidates, err := s.searchFindingCandidates(r, "")
	if err != nil {
		s.logger.Error("pin form: search findings", "err", err)
		candidates = nil
	}
	if isHTMX(r) {
		render(w, r, views.PinModal(node, "", formKind, "", nil, attached, candidates))
		return
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
	rawFindingIDs := r.PostForm["finding_ids"]
	rawFindFinding := strings.TrimSpace(r.PostFormValue("find_finding"))

	kind, kindErr := parsePinKind(rawKind, node.Type)
	if kindErr != "" {
		s.rerenderPinForm(w, r, user, node, kindErr, rawKind, rawFindFinding, rawFindingIDs)
		return
	}

	findingUUIDs := make([]uuid.UUID, 0, len(rawFindingIDs))
	for _, raw := range rawFindingIDs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		fid, err := uuid.Parse(raw)
		if err != nil {
			s.rerenderPinForm(w, r, user, node, "Invalid finding selection.", rawKind, rawFindFinding, rawFindingIDs)
			return
		}
		findingUUIDs = append(findingUUIDs, fid)
	}
	// Attached findings only make sense on views (the stance + reasoning
	// pattern). Drop them silently for other node types; the UI hides the
	// picker there anyway.
	if node.Type != db.NodeTypeView {
		findingUUIDs = nil
	}

	pinID, err := s.queries.SetPin(r.Context(), db.SetPinParams{
		UserID: user.ID,
		NodeID: node.ID,
		Kind:   kind,
	})
	if err != nil {
		s.logger.Error("pin set", "err", err)
		s.rerenderPinForm(w, r, user, node, "Could not save your pin. Please try again.", rawKind, rawFindFinding, rawFindingIDs)
		return
	}
	if err := s.replacePinFindings(r, pinID, findingUUIDs); err != nil {
		s.logger.Error("pin set: replace findings", "err", err)
		s.rerenderPinForm(w, r, user, node, "Saved the stance but could not update findings. Please try again.", rawKind, rawFindFinding, rawFindingIDs)
		return
	}
	// htmx submits get a full-page navigation back to the node, which closes
	// the modal and shows the new banner. Non-htmx submits get the usual 303.
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/nodes/"+node.Slug)
		w.WriteHeader(http.StatusOK)
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

// handleFindingPicker is the HTMX endpoint that re-renders the finding
// candidate list as the user types in the search box on the pin form.
func (s *Server) handleFindingPicker(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	find := strings.TrimSpace(r.URL.Query().Get("find_finding"))
	candidates, err := s.searchFindingCandidates(r, find)
	if err != nil {
		s.logger.Error("finding picker", "err", err)
		candidates = nil
	}
	user := currentUser(r)
	// Pull the currently-attached findings so they're filtered out of the
	// candidate list — no value in offering an "attach" button for one the
	// user already has attached.
	var attached []views.PinFinding
	if user != nil {
		current, hasCurrent := s.lookupPin(w, r, user.ID, node.ID)
		if !hasCurrent && current == nil {
			return
		}
		if current != nil {
			rs, err := s.queries.ListFindingsForPin(r.Context(), db.ListFindingsForPinParams{
				PinID:    current.ID,
				ViewerID: viewerID(r),
			})
			if err == nil {
				attached = pinFindingsFromRows(rs)
			}
		}
	}
	selected := r.URL.Query()["finding_ids"]
	render(w, r, views.FindingCandidatePicker(find, selected, attached, candidates))
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

func (s *Server) rerenderPinForm(w http.ResponseWriter, r *http.Request, user *db.User, node db.Node, flash, formKind, findFinding string, selectedIDs []string) {
	current, hasCurrent := s.lookupPin(w, r, user.ID, node.ID)
	if !hasCurrent && current == nil {
		return
	}
	var attached []views.PinFinding
	if current != nil {
		if rs, err := s.queries.ListFindingsForPin(r.Context(), db.ListFindingsForPinParams{
			PinID:    current.ID,
			ViewerID: viewerID(r),
		}); err == nil {
			attached = pinFindingsFromRows(rs)
		}
	}
	candidates, _ := s.searchFindingCandidates(r, findFinding)
	if isHTMX(r) {
		render(w, r, views.PinModal(node, flash, formKind, findFinding, selectedIDs, attached, candidates))
		return
	}
	render(w, r, views.PinForm(viewerFor(user), node, flash, formKind, findFinding, selectedIDs, attached, candidates))
}

// searchFindingCandidates returns visible finding nodes matching the
// given query (or recent findings when query is empty).
func (s *Server) searchFindingCandidates(r *http.Request, find string) ([]views.FindingCandidate, error) {
	rows, err := s.queries.SearchFindings(r.Context(), db.SearchFindingsParams{
		Query:    find,
		ViewerID: viewerID(r),
	})
	if err != nil {
		return nil, err
	}
	out := make([]views.FindingCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.FindingCandidate{ID: row.ID, Slug: row.Slug, Title: row.Title})
	}
	return out, nil
}

// replacePinFindings clears the pin's existing findings and inserts the
// new set. Not transactional — a partial failure leaves the pin in an
// inconsistent state, but the user can retry by re-submitting the form.
func (s *Server) replacePinFindings(r *http.Request, pinID uuid.UUID, findingIDs []uuid.UUID) error {
	if err := s.queries.DeleteFindingsForPin(r.Context(), pinID); err != nil {
		return err
	}
	for _, fid := range findingIDs {
		if err := s.queries.AddPinFinding(r.Context(), db.AddPinFindingParams{
			PinID:     pinID,
			FindingID: fid,
		}); err != nil {
			return err
		}
	}
	return nil
}

func pinFindingsFromRows(rows []db.ListFindingsForPinRow) []views.PinFinding {
	out := make([]views.PinFinding, 0, len(rows))
	for _, r := range rows {
		out = append(out, views.PinFinding{ID: r.ID, Slug: r.Slug, Title: r.Title})
	}
	return out
}
