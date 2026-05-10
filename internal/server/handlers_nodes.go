package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

// resolveNode loads a node by the URL param "id". The param may be either a
// UUID or a slug — UUID is tried first (parses as a uuid.UUID), slug is the
// fallback. On not-found, any other error, or visibility denial the function
// writes the response and returns ok=false; the caller should return
// immediately. Hidden nodes are reported as 404 so existence isn't leaked.
func (s *Server) resolveNode(w http.ResponseWriter, r *http.Request) (db.Node, bool) {
	raw := chiURLParam(r, "id")
	if raw == "" {
		http.NotFound(w, r)
		return db.Node{}, false
	}
	var node db.Node
	var err error
	if id, parseErr := uuid.Parse(raw); parseErr == nil {
		node, err = s.queries.GetNode(r.Context(), id)
	} else {
		node, err = s.queries.GetNodeBySlug(r.Context(), raw)
	}
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return db.Node{}, false
		}
		s.logger.Error("resolve node", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return db.Node{}, false
	}
	visible, err := s.canViewNode(r.Context(), node, viewerID(r))
	if err != nil {
		s.logger.Error("resolve node: visibility", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return db.Node{}, false
	}
	if !visible {
		http.NotFound(w, r)
		return db.Node{}, false
	}
	return node, true
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	recent, err := s.queries.ListRecentNodesForViewer(r.Context(), db.ListRecentNodesForViewerParams{
		Limit:    25,
		ViewerID: viewerID(r),
	})
	if err != nil {
		s.logger.Error("home: list recent", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.Home(viewerFor(currentUser(r)), recent))
}

func (s *Server) handleNodeNew(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	lists, err := s.queries.ListAudienceLists(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("node new: lists", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if isHTMX(r) {
		render(w, r, views.NodeNewModal("", "view", "", "", "", "", "public", lists))
		return
	}
	render(w, r, views.NodeNew(viewerFor(user), "", "view", "", "", "", "", "public", lists))
}

func (s *Server) handleNodeCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rawType := strings.TrimSpace(r.PostFormValue("type"))
	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	rawPin := strings.TrimSpace(r.PostFormValue("pin")) // "" | "supports" | "opposes" | "featured"
	rawTags := r.PostFormValue("tags")
	rawVisibility := strings.TrimSpace(r.PostFormValue("visibility"))
	if rawVisibility == "" {
		rawVisibility = "public"
	}

	lists, listsErr := s.queries.ListAudienceLists(r.Context(), user.ID)
	if listsErr != nil {
		s.logger.Error("create node: lists", "err", listsErr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	rerender := func(flash string) {
		if isHTMX(r) {
			render(w, r, views.NodeNewModal(flash, rawType, title, body, rawPin, rawTags, rawVisibility, lists))
			return
		}
		render(w, r, views.NodeNew(viewerFor(user), flash, rawType, title, body, rawPin, rawTags, rawVisibility, lists))
	}

	flash := ""
	nt := db.NodeType(rawType)
	var pinKind db.PinKind
	switch {
	// The Post path only creates topics or views. Reasoning and evidence
	// are created later as connections off an existing topic or view.
	case nt != db.NodeTypeTopic && nt != db.NodeTypeView:
		flash = "Pick a type: View or Topic."
	case title == "":
		flash = "Title is required."
	case len(title) > 200:
		flash = "Title is too long (max 200 characters)."
	}
	if flash == "" && rawPin != "" {
		var pinErr string
		pinKind, pinErr = parsePinKind(rawPin, nt)
		if pinErr != "" {
			flash = pinErr
		}
	}
	visKind, visListID, visErr := parseVisibility(rawVisibility, user.ID, lists)
	if flash == "" && visErr != "" {
		flash = visErr
	}
	if flash != "" {
		rerender(flash)
		return
	}

	slug, err := s.uniqueSlug(r.Context(), slugify(title))
	if err != nil {
		s.logger.Error("create node: unique slug", "err", err)
		rerender("Could not create post. Please try again.")
		return
	}

	node, err := s.queries.CreateNode(r.Context(), db.CreateNodeParams{
		Type:             nt,
		Title:            title,
		Body:             body,
		SourceUrl:        nil,
		CreatedBy:        user.ID,
		Slug:             slug,
		Visibility:       visKind,
		VisibilityListID: visListID,
	})
	if err != nil {
		s.logger.Error("create node", "err", err)
		rerender("Could not create post. Please try again.")
		return
	}
	if rawPin != "" {
		if err := s.queries.SetPin(r.Context(), db.SetPinParams{
			UserID: user.ID,
			NodeID: node.ID,
			Kind:   pinKind,
		}); err != nil {
			// Node created successfully; the pin failed. Log and continue —
			// user can pin from the detail page.
			s.logger.Error("create node: pin", "err", err)
		}
	}
	if names := parseTagsInput(rawTags); len(names) > 0 {
		if err := s.setTagsForNode(r, node.ID, names); err != nil {
			s.logger.Error("create node: set tags", "err", err)
		}
	}
	// Modal submit: ask htmx to do a full navigation to the new post so the
	// modal closes and the page actually changes. Non-htmx submits get the
	// usual 303 redirect.
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/nodes/"+node.Slug)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

func (s *Server) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID

	vid := viewerID(r)
	out, err := s.queries.ListEdgesFromNodeForViewer(r.Context(), db.ListEdgesFromNodeForViewerParams{
		FromNode: id,
		ViewerID: vid,
	})
	if err != nil {
		s.logger.Error("node detail: outgoing edges", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	in, err := s.queries.ListEdgesToNodeForViewer(r.Context(), db.ListEdgesToNodeForViewerParams{
		ToNode:   id,
		ViewerID: vid,
	})
	if err != nil {
		s.logger.Error("node detail: incoming edges", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	feat, err := s.queries.ListFeaturedEdgesFromNodeForViewer(r.Context(), db.ListFeaturedEdgesFromNodeForViewerParams{
		FromNode: id,
		ViewerID: vid,
	})
	if err != nil {
		s.logger.Error("node detail: featured edges", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	tags, err := s.queries.ListTagsForNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node detail: list tags", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user := currentUser(r)
	var pin *views.PinInfo
	if user != nil {
		row, ok := s.lookupPin(w, r, user.ID, id)
		if !ok {
			return // error already written
		}
		if row != nil {
			info := views.PinInfo{Kind: row.Kind, ReasoningID: row.ReasoningID}
			if row.ReasoningTitle != nil {
				info.ReasoningTitle = *row.ReasoningTitle
			}
			if row.ReasoningSlug != nil {
				info.ReasoningSlug = *row.ReasoningSlug
			}
			pin = &info
		}
	}

	render(w, r, views.NodeDetail(
		viewerFor(user),
		node,
		featuredRows(feat),
		displayGroups(out, in),
		pin,
		tags,
	))
}

func (s *Server) handleEdgeFeature(w http.ResponseWriter, r *http.Request) {
	fromNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.FeatureEdge(r.Context(), db.FeatureEdgeParams{
		ID:       edgeID,
		FromNode: fromNode.ID,
	}); err != nil {
		s.logger.Error("feature edge", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+fromNode.Slug, http.StatusSeeOther)
}

func (s *Server) handleEdgeUnfeature(w http.ResponseWriter, r *http.Request) {
	fromNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.UnfeatureEdge(r.Context(), db.UnfeatureEdgeParams{
		ID:       edgeID,
		FromNode: fromNode.ID,
	}); err != nil {
		s.logger.Error("unfeature edge", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+fromNode.Slug, http.StatusSeeOther)
}

func (s *Server) handleNodeDeleteConfirm(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID
	if node.CreatedBy != user.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	edgeCount, err := s.queries.CountEdgesForNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node delete confirm: count edges", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	otherPinCount, err := s.queries.CountOtherUserPinsForNode(r.Context(), db.CountOtherUserPinsForNodeParams{
		NodeID: id,
		UserID: user.ID,
	})
	if err != nil {
		s.logger.Error("node delete confirm: count pins", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.NodeDelete(viewerFor(user), node, edgeCount, otherPinCount))
}

func (s *Server) handleNodeDelete(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if node.CreatedBy != user.ID {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := s.queries.DeleteNode(r.Context(), node.ID); err != nil {
		s.logger.Error("node delete", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/users/"+user.Username, http.StatusSeeOther)
}

func (s *Server) handleEdgeDelete(w http.ResponseWriter, r *http.Request) {
	// pageNode is the node whose page hosted the form — used for the redirect
	// only. Edges can be deleted from either endpoint, so we don't constrain
	// the delete to from_node.
	pageNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.DeleteEdge(r.Context(), edgeID); err != nil {
		s.logger.Error("delete edge", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+pageNode.Slug, http.StatusSeeOther)
}

func (s *Server) handleNodeEdit(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID
	src := ""
	if node.SourceUrl != nil {
		src = *node.SourceUrl
	}
	tags, err := s.queries.ListTagsForNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node edit: list tags", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	lists, err := s.queries.ListAudienceLists(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("node edit: lists", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.NodeEdit(viewerFor(user), node, "", node.Title, node.Body, src, joinTagNames(tags), formatVisibility(node.Visibility, node.VisibilityListID), lists))
}

// joinTagNames flattens a tag list into the comma-separated string the form
// field expects.
func joinTagNames(tags []db.Tag) string {
	names := make([]string, 0, len(tags))
	for _, t := range tags {
		names = append(names, t.Name)
	}
	return strings.Join(names, ", ")
}

func (s *Server) handleNodeUpdate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	sourceURL := strings.TrimSpace(r.PostFormValue("source_url"))
	rawTags := r.PostFormValue("tags")
	rawVisibility := strings.TrimSpace(r.PostFormValue("visibility"))
	if rawVisibility == "" {
		rawVisibility = formatVisibility(node.Visibility, node.VisibilityListID)
	}

	lists, listsErr := s.queries.ListAudienceLists(r.Context(), user.ID)
	if listsErr != nil {
		s.logger.Error("update node: lists", "err", listsErr)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	flash := ""
	switch {
	case title == "":
		flash = "Title is required."
	case len(title) > 200:
		flash = "Title is too long (max 200 characters)."
	case node.Type == db.NodeTypeEvidence && sourceURL == "":
		flash = "Evidence needs a source URL."
	}
	visKind, visListID, visErr := parseVisibility(rawVisibility, user.ID, lists)
	if flash == "" && visErr != "" {
		flash = visErr
	}
	if flash != "" {
		render(w, r, views.NodeEdit(viewerFor(user), node, flash, title, body, sourceURL, rawTags, rawVisibility, lists))
		return
	}

	var srcPtr *string
	if sourceURL != "" {
		srcPtr = &sourceURL
	}

	_, err := s.queries.UpdateNode(r.Context(), db.UpdateNodeParams{
		ID:               id,
		Title:            title,
		Body:             body,
		SourceUrl:        srcPtr,
		Visibility:       visKind,
		VisibilityListID: visListID,
	})
	if err != nil {
		s.logger.Error("update node", "err", err)
		render(w, r, views.NodeEdit(viewerFor(user), node, "Could not save changes. Please try again.", title, body, sourceURL, rawTags, rawVisibility, lists))
		return
	}
	if err := s.setTagsForNode(r, id, parseTagsInput(rawTags)); err != nil {
		s.logger.Error("update node: set tags", "err", err)
	}
	http.Redirect(w, r, "/nodes/"+node.Slug, http.StatusSeeOther)
}

func (s *Server) handleEdgeNew(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID
	find := strings.TrimSpace(r.URL.Query().Get("find"))
	candidates, err := s.searchEdgeCandidates(r, id, find)
	if err != nil {
		s.logger.Error("edge new: search candidates", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// htmx-driven Connect button opens the form in a modal; direct URL
	// hits (and no-JS clients) get the standalone full page.
	if isHTMX(r) {
		render(w, r, views.EdgeNewModal(node, "", "", find, candidates))
		return
	}
	render(w, r, views.EdgeNew(viewerFor(currentUser(r)), node, "", "", find, candidates))
}

// handleEdgePicker returns just the candidate-picker fragment, used by HTMX
// for live search-as-you-type. The full form lives at /edges/new; this
// endpoint serves only the part that needs to update on each keystroke.
func (s *Server) handleEdgePicker(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	id := node.ID
	find := strings.TrimSpace(r.URL.Query().Get("find"))
	candidates, err := s.searchEdgeCandidates(r, id, find)
	if err != nil {
		s.logger.Error("edge picker fragment: search candidates", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.CandidatePicker(find, candidates))
}

// searchEdgeCandidates returns the matches the picker shows. An empty or
// whitespace-only query returns no rows; the form's empty state tells the
// user to type to search. Otherwise the trigram %> operator handles the
// match — pg_trgm takes plain text, so user input goes through unchanged
// (no escaping needed and no operators to inject).
func (s *Server) searchEdgeCandidates(r *http.Request, sourceID uuid.UUID, query string) ([]views.EdgeCandidate, error) {
	q := strings.TrimSpace(query)
	if q == "" {
		return nil, nil
	}
	rows, err := s.queries.PickerSearchNodes(r.Context(), db.PickerSearchNodesParams{
		Query:    q,
		SourceID: sourceID,
		ViewerID: viewerID(r),
	})
	if err != nil {
		return nil, err
	}
	out := make([]views.EdgeCandidate, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.EdgeCandidate{ID: row.ID, Type: row.Type, Title: row.Title})
	}
	return out, nil
}

func (s *Server) handleEdgeCreate(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	if user == nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	fromNode, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	fromID := fromNode.ID
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rawKind := strings.TrimSpace(r.PostFormValue("kind"))
	rawToID := strings.TrimSpace(r.PostFormValue("to_id"))

	find := strings.TrimSpace(r.PostFormValue("find"))
	// rerender re-displays the form with a flash. When the request came
	// from the modal (HX-Request), respond with the modal fragment so
	// htmx swaps it back into #modal in place; otherwise re-render the
	// full page.
	rerender := func(flash string) {
		candidates, lerr := s.searchEdgeCandidates(r, fromID, find)
		if lerr != nil {
			s.logger.Error("edge create: search candidates", "err", lerr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if isHTMX(r) {
			render(w, r, views.EdgeNewModal(fromNode, flash, rawKind, find, candidates))
			return
		}
		render(w, r, views.EdgeNew(viewerFor(user), fromNode, flash, rawKind, find, candidates))
	}

	ek := db.EdgeKind(rawKind)
	if !ek.Valid() {
		rerender("Pick a valid relationship kind.")
		return
	}
	toID, err := uuid.Parse(rawToID)
	if err != nil {
		rerender("Select a target node.")
		return
	}
	if toID == fromID {
		rerender("A node cannot connect to itself.")
		return
	}

	_, err = s.queries.CreateEdge(r.Context(), db.CreateEdgeParams{
		FromNode:  fromID,
		ToNode:    toID,
		Kind:      ek,
		CreatedBy: user.ID,
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			rerender("That connection already exists.")
			return
		}
		s.logger.Error("edge create", "err", err)
		rerender("Could not create connection. Please try again.")
		return
	}
	// On success from htmx, ask htmx to do a full page navigation back to
	// the node — that closes the modal and shows the new edge in the
	// connections list. Non-htmx submits get the usual 303 redirect.
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/nodes/"+fromNode.Slug)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/nodes/"+fromNode.Slug, http.StatusSeeOther)
}

// edgeOrder fixes the canonical kind sequence for the legend and pins active
// vs. passive labels for each kind. relates_to is symmetric so its passive
// form matches its active one and the two directions render in a single bucket.
var edgeOrder = []struct {
	kind         db.EdgeKind
	activeLabel  string
	passiveLabel string
}{
	{db.EdgeKindSupports, "Supports", "Supported by"},
	{db.EdgeKindOpposes, "Opposes", "Opposed by"},
	{db.EdgeKindRefines, "Refines", "Refined by"},
	{db.EdgeKindCites, "Cites", "Cited by"},
	{db.EdgeKindRelatesTo, "Relates to", "Relates to"},
}

// displayGroups combines outgoing and incoming edges into one ordered list
// of legend sections. For asymmetric kinds the two directions become two
// separate sections with active and passive labels ("Supports" /
// "Supported by"). For relates_to (symmetric) both directions merge into a
// single "Relates to" section. Featured outgoing edges are excluded — they
// render inline in the "Key reasoning" section above the legend.
func displayGroups(out []db.ListEdgesFromNodeForViewerRow, in []db.ListEdgesToNodeForViewerRow) []views.EdgeGroup {
	var groups []views.EdgeGroup
	for _, ek := range edgeOrder {
		outRows := outgoingRowsOfKind(out, ek.kind)
		inRows := incomingRowsOfKind(in, ek.kind)

		if ek.kind == db.EdgeKindRelatesTo {
			merged := append(append([]views.EdgeRow{}, outRows...), inRows...)
			if len(merged) > 0 {
				groups = append(groups, views.EdgeGroup{
					Kind:  ek.kind,
					Label: ek.activeLabel,
					Rows:  merged,
				})
			}
			continue
		}
		if len(outRows) > 0 {
			groups = append(groups, views.EdgeGroup{Kind: ek.kind, Label: ek.activeLabel, Rows: outRows})
		}
		if len(inRows) > 0 {
			groups = append(groups, views.EdgeGroup{Kind: ek.kind, Label: ek.passiveLabel, Rows: inRows})
		}
	}
	return groups
}

func outgoingRowsOfKind(rows []db.ListEdgesFromNodeForViewerRow, kind db.EdgeKind) []views.EdgeRow {
	var out []views.EdgeRow
	for _, row := range rows {
		if row.Kind != kind || row.Position != nil {
			continue
		}
		out = append(out, views.EdgeRow{
			EdgeID:   row.ID,
			Kind:     row.Kind,
			Outgoing: true,
			ID:       row.ToID,
			Slug:     row.ToSlug,
			Type:     row.ToType,
			Title:    row.ToTitle,
		})
	}
	return out
}

func incomingRowsOfKind(rows []db.ListEdgesToNodeForViewerRow, kind db.EdgeKind) []views.EdgeRow {
	var out []views.EdgeRow
	for _, row := range rows {
		if row.Kind != kind {
			continue
		}
		out = append(out, views.EdgeRow{
			EdgeID:   row.ID,
			Kind:     row.Kind,
			Outgoing: false,
			ID:       row.FromID,
			Slug:     row.FromSlug,
			Type:     row.FromType,
			Title:    row.FromTitle,
		})
	}
	return out
}

// featuredRows turns the DB projection into the view-layer FeaturedRow,
// looking up the active label from edgeOrder so templates don't need to
// hardcode "supports → Supports" mappings.
func featuredRows(rows []db.ListFeaturedEdgesFromNodeForViewerRow) []views.FeaturedRow {
	labels := make(map[db.EdgeKind]string, len(edgeOrder))
	for _, ek := range edgeOrder {
		labels[ek.kind] = ek.activeLabel
	}
	out := make([]views.FeaturedRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.FeaturedRow{
			EdgeID: row.ID,
			Kind:   row.Kind,
			Label:  labels[row.Kind],
			ID:     row.ToID,
			Slug:   row.ToSlug,
			Type:   row.ToType,
			Title:  row.ToTitle,
			Body:   row.ToBody,
		})
	}
	return out
}
