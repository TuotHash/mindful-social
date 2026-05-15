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

	// argumentGraphAuthorSeedLimit caps how many of an author's nodes are
	// used as seeds for the neighborhood walk. Keeping it at the canvas
	// budget avoids feeding the recursive CTE a giant input that would
	// then be trimmed by the outer LIMIT anyway.
	argumentGraphAuthorSeedLimit int32 = argumentGraphNodeLimit

	// argumentGraphSearchMaxHops bounds how far we expand around each
	// search match before shipping the result to the browser. It must
	// keep up with the maximum the client-side depth slider can request
	// — otherwise the slider would run out of data past its midpoint.
	argumentGraphSearchMaxHops int32 = 5
)

func (s *Server) handleArgumentGraph(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	author := strings.TrimSpace(r.URL.Query().Get("author"))
	data, err := s.loadArgumentGraph(r, q, author)
	if err != nil {
		s.logger.Error("argument graph: load", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.ArgumentGraph(viewerFor(currentUser(r)), data, q, author))
}

func (s *Server) handleArgumentGraphData(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	author := strings.TrimSpace(r.URL.Query().Get("author"))
	data, err := s.loadArgumentGraph(r, q, author)
	if err != nil {
		s.logger.Error("argument graph data: load", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) loadArgumentGraph(r *http.Request, query, author string) (views.ArgumentGraphData, error) {
	if query != "" || author != "" {
		return s.searchArgumentGraph(r, query, author)
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

// searchArgumentGraph collects seed node ids from the active filters
// (full-text query, author username, or both) and walks the edges table out
// from each seed up to argumentGraphSearchMaxHops hops. Without that
// expansion the client receives lonely matches and no edges, so the depth
// slider has nothing to walk and the inspector (correctly!) reports zero
// connections. The outer LIMIT in the SQL guarantees we never ship more
// nodes than the canvas can usefully render.
//
// When both filters are set the seeds are intersected: the result is the
// nodes by `author` that also match `query`. That keeps the combined filter
// behaving like an AND from the user's perspective.
func (s *Server) searchArgumentGraph(r *http.Request, query, author string) (views.ArgumentGraphData, error) {
	vid := viewerID(r)

	var (
		seedIDs    []uuid.UUID
		matchedIDs = map[string]struct{}{}
	)

	if query != "" {
		matchRows, err := s.queries.SearchNodes(r.Context(), db.SearchNodesParams{
			Query:       query,
			ResultLimit: searchResultLimit,
			ViewerID:    vid,
		})
		if err != nil {
			return views.ArgumentGraphData{}, err
		}
		seedIDs = make([]uuid.UUID, 0, len(matchRows))
		for _, row := range matchRows {
			seedIDs = append(seedIDs, row.ID)
			matchedIDs[row.ID.String()] = struct{}{}
		}
	}

	if author != "" {
		authorIDs, err := s.queries.ListArgumentGraphSeedsByAuthor(r.Context(), db.ListArgumentGraphSeedsByAuthorParams{
			AuthorUsername: author,
			ViewerID:       vid,
			ResultLimit:    argumentGraphAuthorSeedLimit,
		})
		if err != nil {
			return views.ArgumentGraphData{}, err
		}
		if query == "" {
			seedIDs = authorIDs
			for _, id := range authorIDs {
				matchedIDs[id.String()] = struct{}{}
			}
		} else {
			authorSet := make(map[uuid.UUID]struct{}, len(authorIDs))
			for _, id := range authorIDs {
				authorSet[id] = struct{}{}
			}
			filtered := seedIDs[:0]
			matchedIDs = map[string]struct{}{}
			for _, id := range seedIDs {
				if _, ok := authorSet[id]; ok {
					filtered = append(filtered, id)
					matchedIDs[id.String()] = struct{}{}
				}
			}
			seedIDs = filtered
		}
	}

	if len(seedIDs) == 0 {
		return views.ArgumentGraphData{Nodes: []views.ArgumentGraphNode{}, Edges: []views.ArgumentGraphEdge{}}, nil
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
		Nodes: argumentGraphNodesFromNeighborhoodRows(neighborhoodRows, matchedIDs),
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

func argumentGraphNodesFromNeighborhoodRows(rows []db.ListArgumentGraphNeighborhoodRow, matchedIDs map[string]struct{}) []views.ArgumentGraphNode {
	out := make([]views.ArgumentGraphNode, 0, len(rows))
	for _, row := range rows {
		_, isMatch := matchedIDs[row.ID]
		out = append(out, views.ArgumentGraphNode{
			ID:             row.ID,
			Slug:           row.Slug,
			Type:           row.NodeType,
			Title:          row.Title,
			AuthorUsername: row.AuthorUsername,
			Match:          isMatch,
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
