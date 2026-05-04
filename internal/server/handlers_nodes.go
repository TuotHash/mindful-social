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
	render(w, r, views.NodeNew(viewerFor(currentUser(r)), "", "", "", "", ""))
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

	flash := ""
	nt := db.NodeType(rawType)
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
	if flash != "" {
		render(w, r, views.NodeNew(viewerFor(user), flash, rawType, title, body, sourceURL))
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
		render(w, r, views.NodeNew(viewerFor(user), "Could not create node. Please try again.", rawType, title, body, sourceURL))
		return
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

	user := currentUser(r)
	var commitment *views.CommitmentInfo
	if user != nil && node.Type == db.NodeTypeView {
		row, err := s.queries.GetCommitmentForUserAndView(r.Context(), db.GetCommitmentForUserAndViewParams{
			UserID: user.ID,
			ViewID: id,
		})
		switch {
		case err == nil:
			info := views.CommitmentInfo{ReasoningID: row.ReasoningID}
			if row.ReasoningTitle != nil {
				info.ReasoningTitle = *row.ReasoningTitle
			}
			commitment = &info
		case errors.Is(err, pgx.ErrNoRows):
			// not committed — leave nil
		default:
			s.logger.Error("node detail: commitment lookup", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}

	render(w, r, views.NodeDetail(
		viewerFor(user),
		node,
		featuredRows(feat),
		groupOutgoing(out),
		groupIncoming(in),
		commitment,
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
	render(w, r, views.NodeEdit(viewerFor(currentUser(r)), node, "", node.Title, node.Body, src))
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
		render(w, r, views.NodeEdit(viewerFor(user), node, flash, title, body, sourceURL))
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
		render(w, r, views.NodeEdit(viewerFor(user), node, "Could not save changes. Please try again.", title, body, sourceURL))
		return
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
	candidates, err := s.queries.ListNodesExcept(r.Context(), id)
	if err != nil {
		s.logger.Error("edge new: list candidates", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.EdgeNew(viewerFor(currentUser(r)), node, "", "", candidates))
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

	rerender := func(flash string) {
		candidates, lerr := s.queries.ListNodesExcept(r.Context(), fromID)
		if lerr != nil {
			s.logger.Error("edge create: list candidates", "err", lerr)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		render(w, r, views.EdgeNew(viewerFor(user), fromNode, flash, rawKind, candidates))
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

// groupOutgoing/groupIncoming bucket the flat edge list into the legend
// sections the template expects. Order is fixed (supports first, then
// opposes, refines, cites, relates_to) for visual consistency across pages.
var edgeOrder = []struct {
	kind  db.EdgeKind
	label string
}{
	{db.EdgeKindSupports, "Supports"},
	{db.EdgeKindOpposes, "Opposes"},
	{db.EdgeKindRefines, "Refines"},
	{db.EdgeKindCites, "Cites"},
	{db.EdgeKindRelatesTo, "Relates to"},
}

func groupOutgoing(rows []db.ListEdgesFromNodeRow) []views.EdgeGroup {
	groups := make([]views.EdgeGroup, 0, len(edgeOrder))
	for _, ek := range edgeOrder {
		g := views.EdgeGroup{Label: ek.label, Kind: ek.kind}
		for _, row := range rows {
			if row.Kind == ek.kind {
				g.Rows = append(g.Rows, views.EdgeRow{
					EdgeID:   row.ID,
					Kind:     row.Kind,
					Featured: row.Position != nil,
					ID:       row.ToID,
					Type:     row.ToType,
					Title:    row.ToTitle,
				})
			}
		}
		groups = append(groups, g)
	}
	return groups
}

func groupIncoming(rows []db.ListEdgesToNodeRow) []views.EdgeGroup {
	groups := make([]views.EdgeGroup, 0, len(edgeOrder))
	for _, ek := range edgeOrder {
		g := views.EdgeGroup{Label: ek.label, Kind: ek.kind}
		for _, row := range rows {
			if row.Kind == ek.kind {
				g.Rows = append(g.Rows, views.EdgeRow{
					EdgeID: row.ID,
					Kind:   row.Kind,
					ID:     row.FromID,
					Type:   row.FromType,
					Title:  row.FromTitle,
				})
			}
		}
		groups = append(groups, g)
	}
	return groups
}

// featuredRows turns the DB projection into the view-layer FeaturedRow,
// looking up the human label from edgeOrder so templates don't need to
// hardcode "supports → Supports" mappings.
func featuredRows(rows []db.ListFeaturedEdgesFromNodeRow) []views.FeaturedRow {
	labels := make(map[db.EdgeKind]string, len(edgeOrder))
	for _, ek := range edgeOrder {
		labels[ek.kind] = ek.label
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
