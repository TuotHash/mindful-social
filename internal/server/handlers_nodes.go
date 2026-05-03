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

	render(w, r, views.NodeDetail(
		viewerFor(currentUser(r)),
		node,
		groupOutgoing(out),
		groupIncoming(in),
	))
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
					Kind:  row.Kind,
					ID:    row.ToID,
					Type:  row.ToType,
					Title: row.ToTitle,
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
					Kind:  row.Kind,
					ID:    row.FromID,
					Type:  row.FromType,
					Title: row.FromTitle,
				})
			}
		}
		groups = append(groups, g)
	}
	return groups
}
