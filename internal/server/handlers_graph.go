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

	// argumentGraphSearchMaxHops bounds how far we expand around each
	// search match before shipping the result to the browser. It must
	// keep up with the maximum the client-side depth slider can request
	// — otherwise the slider would run out of data past its midpoint.
	argumentGraphSearchMaxHops int32 = 5
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

// searchArgumentGraph hits the full-text/fuzzy node search for `query`,
// then walks the edges table out from each match to argumentGraphSearchMaxHops
// hops. Without that expansion the client receives a lonely match and no
// edges, so the depth slider has nothing to walk and the inspector
// (correctly!) reports zero connections. The outer LIMIT in the SQL
// guarantees we never ship more nodes than the canvas can usefully render.
func (s *Server) searchArgumentGraph(r *http.Request, query string) (views.ArgumentGraphData, error) {
	vid := viewerID(r)
	matchRows, err := s.queries.SearchNodes(r.Context(), db.SearchNodesParams{
		Query:       query,
		ResultLimit: searchResultLimit,
		ViewerID:    vid,
	})
	if err != nil {
		return views.ArgumentGraphData{}, err
	}
	if len(matchRows) == 0 {
		return views.ArgumentGraphData{Nodes: []views.ArgumentGraphNode{}, Edges: []views.ArgumentGraphEdge{}}, nil
	}

	seedIDs := make([]uuid.UUID, 0, len(matchRows))
	for _, row := range matchRows {
		seedIDs = append(seedIDs, row.ID)
	}

	neighborhoodRows, err := s.queries.ListArgumentGraphNeighborhood(r.Context(), db.ListArgumentGraphNeighborhoodParams{
		SeedIds:     seedIDs,
		ViewerID:    vid,
		MaxHops:     argumentGraphSearchMaxHops,
		ResultLimit: argumentGraphNodeLimit,
	})
	if err != nil {
		return views.ArgumentGraphData{}, err
	}

	nodeIDs := make([]uuid.UUID, 0, len(neighborhoodRows))
	for _, row := range neighborhoodRows {
		id, parseErr := uuid.Parse(row.ID)
		if parseErr != nil {
			continue
		}
		nodeIDs = append(nodeIDs, id)
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
		Nodes: argumentGraphNodesFromNeighborhoodRows(neighborhoodRows),
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

func argumentGraphNodesFromNeighborhoodRows(rows []db.ListArgumentGraphNeighborhoodRow) []views.ArgumentGraphNode {
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
