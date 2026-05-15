package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	argumentGraphNodeLimit int32 = 120
	argumentGraphEdgeLimit int32 = 320
)

func (s *Server) handleArgumentGraph(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data, err := s.loadArgumentGraph(r, q)
	if err != nil {
		s.logger.Error("argument graph: load", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.ArgumentGraph(viewerFor(currentUser(r)), data, q))
}

func (s *Server) handleArgumentGraphData(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	data, err := s.loadArgumentGraph(r, q)
	if err != nil {
		s.logger.Error("argument graph data: load", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) loadArgumentGraph(r *http.Request, query string) (views.ArgumentGraphData, error) {
	if query != "" {
		return s.searchArgumentGraph(r, query)
	}

	vid := viewerID(r)
	nodeRows, err := s.queries.ListArgumentGraphNodesForViewer(r.Context(), db.ListArgumentGraphNodesForViewerParams{
		ViewerID:    vid,
		ResultLimit: argumentGraphNodeLimit,
	})
	if err != nil {
		return views.ArgumentGraphData{}, err
	}
	edgeRows, err := s.queries.ListArgumentGraphEdgesForViewer(r.Context(), db.ListArgumentGraphEdgesForViewerParams{
		ViewerID:  vid,
		NodeLimit: argumentGraphNodeLimit,
		EdgeLimit: argumentGraphEdgeLimit,
	})
	if err != nil {
		return views.ArgumentGraphData{}, err
	}
	return views.ArgumentGraphData{
		Nodes: argumentGraphNodesFromRows(nodeRows),
		Edges: argumentGraphEdgesFromRows(edgeRows),
	}, nil
}

func (s *Server) searchArgumentGraph(r *http.Request, query string) (views.ArgumentGraphData, error) {
	vid := viewerID(r)
	nodeRows, err := s.queries.SearchNodes(r.Context(), db.SearchNodesParams{
		Query:       query,
		ResultLimit: searchResultLimit,
		ViewerID:    vid,
	})
	if err != nil {
		return views.ArgumentGraphData{}, err
	}
	if len(nodeRows) == 0 {
		return views.ArgumentGraphData{Nodes: []views.ArgumentGraphNode{}, Edges: []views.ArgumentGraphEdge{}}, nil
	}

	nodeIDs := make([]uuid.UUID, 0, len(nodeRows))
	for _, row := range nodeRows {
		nodeIDs = append(nodeIDs, row.ID)
	}
	edgeRows, err := s.queries.ListArgumentGraphEdgesForNodeIDs(r.Context(), db.ListArgumentGraphEdgesForNodeIDsParams{
		NodeIds:   nodeIDs,
		ViewerID:  vid,
		EdgeLimit: argumentGraphEdgeLimit,
	})
	if err != nil {
		return views.ArgumentGraphData{}, err
	}
	return views.ArgumentGraphData{
		Nodes: argumentGraphNodesFromSearchRows(nodeRows),
		Edges: argumentGraphEdgesFromNodeIDRows(edgeRows),
	}, nil
}

func argumentGraphNodesFromRows(rows []db.ListArgumentGraphNodesForViewerRow) []views.ArgumentGraphNode {
	out := make([]views.ArgumentGraphNode, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ArgumentGraphNode{
			ID:             row.ID,
			Slug:           row.Slug,
			Type:           row.NodeType,
			Title:          row.Title,
			AuthorUsername: row.AuthorUsername,
		})
	}
	return out
}

func argumentGraphNodesFromSearchRows(rows []db.SearchNodesRow) []views.ArgumentGraphNode {
	out := make([]views.ArgumentGraphNode, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ArgumentGraphNode{
			ID:             row.ID.String(),
			Slug:           row.Slug,
			Type:           string(row.Type),
			Title:          row.Title,
			AuthorUsername: row.AuthorUsername,
		})
	}
	return out
}

func argumentGraphEdgesFromRows(rows []db.ListArgumentGraphEdgesForViewerRow) []views.ArgumentGraphEdge {
	out := make([]views.ArgumentGraphEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ArgumentGraphEdge{
			ID:     row.ID,
			FromID: row.FromID,
			ToID:   row.ToID,
			Kind:   row.Kind,
		})
	}
	return out
}

func argumentGraphEdgesFromNodeIDRows(rows []db.ListArgumentGraphEdgesForNodeIDsRow) []views.ArgumentGraphEdge {
	out := make([]views.ArgumentGraphEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ArgumentGraphEdge{
			ID:     row.ID,
			FromID: row.FromID,
			ToID:   row.ToID,
			Kind:   row.Kind,
		})
	}
	return out
}
