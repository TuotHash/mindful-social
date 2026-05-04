package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/mindful-social/mindful-social/internal/db"
	"github.com/mindful-social/mindful-social/internal/views"
)

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	recent, err := s.queries.ListRecentNodes(r.Context(), 25)
	if err != nil {
		s.logger.Error("home: list recent", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.Home(viewerFor(currentUser(r)), recent))
}

func (s *Server) handleNodeNew(w http.ResponseWriter, r *http.Request) {
	render(w, r, views.NodeNew(viewerFor(currentUser(r)), "", "", "", "", "", "", ""))
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
	sourceURL := strings.TrimSpace(r.PostFormValue("source_url"))
	rawPin := strings.TrimSpace(r.PostFormValue("pin")) // "" | "supports" | "opposes" | "featured"
	rawTags := r.PostFormValue("tags")

	flash := ""
	nt := db.NodeType(rawType)
	var pinKind db.PinKind
	switch {
	case !nt.Valid():
		flash = "Pick a valid node type."
	case title == "":
		flash = "Title is required."
	case len(title) > 200:
		flash = "Title is too long (max 200 characters)."
	case nt == db.NodeTypeFact && sourceURL == "":
		flash = "Facts need a source URL."
	}
	if flash == "" && rawPin != "" {
		var pinErr string
		pinKind, pinErr = parsePinKind(rawPin, nt)
		if pinErr != "" {
			flash = pinErr
		}
	}
	if flash != "" {
		render(w, r, views.NodeNew(viewerFor(user), flash, rawType, title, body, sourceURL, rawPin, rawTags))
		return
	}

	var srcPtr *string
	if sourceURL != "" {
		srcPtr = &sourceURL
	}

	node, err := s.queries.CreateNode(r.Context(), db.CreateNodeParams{
		Type:      nt,
		Title:     title,
		Body:      body,
		SourceUrl: srcPtr,
		CreatedBy: user.ID,
	})
	if err != nil {
		s.logger.Error("create node", "err", err)
		render(w, r, views.NodeNew(viewerFor(user), "Could not create node. Please try again.", rawType, title, body, sourceURL, rawPin, rawTags))
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
	http.Redirect(w, r, "/nodes/"+node.ID.String(), http.StatusSeeOther)
}

func (s *Server) handleNodeDetail(w http.ResponseWriter, r *http.Request) {
	idStr := chiURLParam(r, "id")
	id, err := uuid.Parse(idStr)
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
		s.logger.Error("node detail: get", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	out, err := s.queries.ListEdgesFromNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node detail: outgoing edges", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	in, err := s.queries.ListEdgesToNode(r.Context(), id)
	if err != nil {
		s.logger.Error("node detail: incoming edges", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	feat, err := s.queries.ListFeaturedEdgesFromNode(r.Context(), id)
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
	fromID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.FeatureEdge(r.Context(), db.FeatureEdgeParams{
		ID:       edgeID,
		FromNode: fromID,
	}); err != nil {
		s.logger.Error("feature edge", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+fromID.String(), http.StatusSeeOther)
}

func (s *Server) handleEdgeUnfeature(w http.ResponseWriter, r *http.Request) {
	fromID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	edgeID, err := uuid.Parse(chiURLParam(r, "edgeID"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := s.queries.UnfeatureEdge(r.Context(), db.UnfeatureEdgeParams{
		ID:       edgeID,
		FromNode: fromID,
	}); err != nil {
		s.logger.Error("unfeature edge", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/nodes/"+fromID.String(), http.StatusSeeOther)
}

func (s *Server) handleNodeEdit(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Error("node edit: get", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
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
	render(w, r, views.NodeEdit(viewerFor(currentUser(r)), node, "", node.Title, node.Body, src, joinTagNames(tags)))
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
		s.logger.Error("node update: get", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	title := strings.TrimSpace(r.PostFormValue("title"))
	body := strings.TrimSpace(r.PostFormValue("body"))
	sourceURL := strings.TrimSpace(r.PostFormValue("source_url"))
	rawTags := r.PostFormValue("tags")

	flash := ""
	switch {
	case title == "":
		flash = "Title is required."
	case len(title) > 200:
		flash = "Title is too long (max 200 characters)."
	case node.Type == db.NodeTypeFact && sourceURL == "":
		flash = "Facts need a source URL."
	}
	if flash != "" {
		render(w, r, views.NodeEdit(viewerFor(user), node, flash, title, body, sourceURL, rawTags))
		return
	}

	var srcPtr *string
	if sourceURL != "" {
		srcPtr = &sourceURL
	}

	_, err = s.queries.UpdateNode(r.Context(), db.UpdateNodeParams{
		ID:        id,
		Title:     title,
		Body:      body,
		SourceUrl: srcPtr,
	})
	if err != nil {
		s.logger.Error("update node", "err", err)
		render(w, r, views.NodeEdit(viewerFor(user), node, "Could not save changes. Please try again.", title, body, sourceURL, rawTags))
		return
	}
	if err := s.setTagsForNode(r, id, parseTagsInput(rawTags)); err != nil {
		s.logger.Error("update node: set tags", "err", err)
	}
	http.Redirect(w, r, "/nodes/"+id.String(), http.StatusSeeOther)
}

func (s *Server) handleEdgeNew(w http.ResponseWriter, r *http.Request) {
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
		s.logger.Error("edge new: get node", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	find := strings.TrimSpace(r.URL.Query().Get("find"))
	candidates, err := s.searchEdgeCandidates(r, id, find)
	if err != nil {
		s.logger.Error("edge new: search candidates", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.EdgeNew(viewerFor(currentUser(r)), node, "", "", find, candidates))
}

// searchEdgeCandidates returns the matches the picker shows. An empty query
// returns no rows — the form's empty state tells the user to type to search.
func (s *Server) searchEdgeCandidates(r *http.Request, sourceID uuid.UUID, query string) ([]views.EdgeCandidate, error) {
	if query == "" {
		return nil, nil
	}
	rows, err := s.queries.PickerSearchNodes(r.Context(), db.PickerSearchNodesParams{
		WebsearchToTsquery: query,
		ID:                 sourceID,
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
	fromID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	fromNode, err := s.queries.GetNode(r.Context(), fromID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("edge create: get from node", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}

	rawKind := strings.TrimSpace(r.PostFormValue("kind"))
	rawToID := strings.TrimSpace(r.PostFormValue("to_id"))

	find := strings.TrimSpace(r.PostFormValue("find"))
	rerender := func(flash string) {
		candidates, lerr := s.searchEdgeCandidates(r, fromID, find)
		if lerr != nil {
			s.logger.Error("edge create: search candidates", "err", lerr)
			http.Error(w, "internal error", http.StatusInternalServerError)
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
	http.Redirect(w, r, "/nodes/"+fromID.String(), http.StatusSeeOther)
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
func displayGroups(out []db.ListEdgesFromNodeRow, in []db.ListEdgesToNodeRow) []views.EdgeGroup {
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

func outgoingRowsOfKind(rows []db.ListEdgesFromNodeRow, kind db.EdgeKind) []views.EdgeRow {
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
			Type:     row.ToType,
			Title:    row.ToTitle,
		})
	}
	return out
}

func incomingRowsOfKind(rows []db.ListEdgesToNodeRow, kind db.EdgeKind) []views.EdgeRow {
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
			Type:     row.FromType,
			Title:    row.FromTitle,
		})
	}
	return out
}

// featuredRows turns the DB projection into the view-layer FeaturedRow,
// looking up the active label from edgeOrder so templates don't need to
// hardcode "supports → Supports" mappings.
func featuredRows(rows []db.ListFeaturedEdgesFromNodeRow) []views.FeaturedRow {
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
			Type:   row.ToType,
			Title:  row.ToTitle,
			Body:   row.ToBody,
		})
	}
	return out
}
