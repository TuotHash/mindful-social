package server

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	argumentGraphNodeLimit int32 = 120
	argumentGraphEdgeLimit int32 = 320

	// argumentGraphSeedLimit caps how many filtered seeds are fed to the
	// neighborhood walk. Keeping it at the canvas budget avoids feeding the
	// recursive CTE a giant input that would then be trimmed by the outer
	// LIMIT anyway.
	argumentGraphSeedLimit int32 = argumentGraphNodeLimit

	// argumentGraphSearchMaxHops bounds how far we expand around each
	// search match before shipping the result to the browser. It must
	// keep up with the maximum the client-side depth slider can request
	// — otherwise the slider would run out of data past its midpoint.
	argumentGraphSearchMaxHops int32 = 5
)

// argumentGraphFilters holds the parsed graph-viewer filter inputs. The
// zero value means "no filters active"; the search path is short-circuited
// when isActive() is false.
type argumentGraphFilters struct {
	Query      string
	Author     string
	Group      string
	Tags       []string
	Since      time.Time
	Sourced    *bool
	Visibility db.VisibilityKind
}

func (f argumentGraphFilters) isActive() bool {
	return f.Query != "" ||
		f.Author != "" ||
		f.Group != "" ||
		len(f.Tags) > 0 ||
		!f.Since.IsZero() ||
		f.Sourced != nil ||
		f.Visibility != ""
}

// hasSeedPredicate reports whether the filters carry any predicate that the
// seed query needs to apply itself (i.e. anything other than the free-text
// q, which goes through SearchNodes).
func (f argumentGraphFilters) hasSeedPredicate() bool {
	return f.Author != "" ||
		f.Group != "" ||
		len(f.Tags) > 0 ||
		!f.Since.IsZero() ||
		f.Sourced != nil ||
		f.Visibility != ""
}

// parseGraphFilters reads the filter set from the request's query string.
// Unknown / invalid values are silently dropped — there is no error path
// the user can act on, and the worst case is "filter has no effect".
func parseGraphFilters(r *http.Request) argumentGraphFilters {
	q := r.URL.Query()
	f := argumentGraphFilters{
		Query:  strings.TrimSpace(q.Get("q")),
		Author: strings.TrimSpace(q.Get("author")),
		Group:  strings.TrimSpace(q.Get("group")),
	}
	for _, t := range q["tag"] {
		t = strings.TrimSpace(strings.ToLower(t))
		if t != "" {
			f.Tags = append(f.Tags, t)
		}
	}
	switch q.Get("since") {
	case "7d":
		f.Since = time.Now().Add(-7 * 24 * time.Hour)
	case "30d":
		f.Since = time.Now().Add(-30 * 24 * time.Hour)
	case "90d":
		f.Since = time.Now().Add(-90 * 24 * time.Hour)
	}
	switch q.Get("sourced") {
	case "yes":
		v := true
		f.Sourced = &v
	case "no":
		v := false
		f.Sourced = &v
	}
	switch q.Get("visibility") {
	case string(db.VisibilityKindPublic),
		string(db.VisibilityKindConnections),
		string(db.VisibilityKindGroup),
		string(db.VisibilityKindPrivate):
		f.Visibility = db.VisibilityKind(q.Get("visibility"))
	}
	return f
}

func (s *Server) handleArgumentGraph(w http.ResponseWriter, r *http.Request) {
	filters := parseGraphFilters(r)
	data, err := s.loadArgumentGraph(r, filters)
	if err != nil {
		s.logger.Error("argument graph: load", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.ArgumentGraph(viewerFor(currentUser(r)), data, graphFiltersForView(filters)))
}

func (s *Server) handleArgumentGraphData(w http.ResponseWriter, r *http.Request) {
	filters := parseGraphFilters(r)
	data, err := s.loadArgumentGraph(r, filters)
	if err != nil {
		s.logger.Error("argument graph data: load", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(data)
}

// graphFiltersForView projects the parsed filter set onto the wire format
// the templ layer expects. Strings round-trip as-is; the date and sourced
// filters collapse back to their canonical query-string token.
func graphFiltersForView(f argumentGraphFilters) views.ArgumentGraphFilters {
	out := views.ArgumentGraphFilters{
		Query:      f.Query,
		Author:     f.Author,
		Group:      f.Group,
		Tags:       f.Tags,
		Visibility: string(f.Visibility),
	}
	switch {
	case f.Since.IsZero():
		out.Since = ""
	case time.Since(f.Since) <= 7*24*time.Hour+time.Hour:
		out.Since = "7d"
	case time.Since(f.Since) <= 30*24*time.Hour+time.Hour:
		out.Since = "30d"
	case time.Since(f.Since) <= 90*24*time.Hour+time.Hour:
		out.Since = "90d"
	}
	if f.Sourced != nil {
		if *f.Sourced {
			out.Sourced = "yes"
		} else {
			out.Sourced = "no"
		}
	}
	return out
}

func (s *Server) loadArgumentGraph(r *http.Request, filters argumentGraphFilters) (views.ArgumentGraphData, error) {
	if filters.isActive() {
		return s.searchArgumentGraph(r, filters)
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

// searchArgumentGraph collects seed node ids by intersecting every active
// filter and walks the edges table out from each seed up to
// argumentGraphSearchMaxHops hops. Without that expansion the client
// receives lonely matches and no edges, so the depth slider has nothing to
// walk and the inspector (correctly!) reports zero connections.
//
// The free-text query goes through SearchNodes (tsvector + trigram). All
// other predicates go through ListArgumentGraphSeeds. When both paths run
// the matched set is the intersection — combined filters behave like AND
// from the user's perspective.
func (s *Server) searchArgumentGraph(r *http.Request, f argumentGraphFilters) (views.ArgumentGraphData, error) {
	vid := viewerID(r)

	var (
		seedIDs    []uuid.UUID
		matchedIDs = map[string]struct{}{}
	)

	if f.Query != "" {
		matchRows, err := s.queries.SearchNodes(r.Context(), db.SearchNodesParams{
			Query:       f.Query,
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

	if f.hasSeedPredicate() {
		filterIDs, err := s.queries.ListArgumentGraphSeeds(r.Context(), seedQueryParams(f, vid))
		if err != nil {
			return views.ArgumentGraphData{}, err
		}
		if f.Query == "" {
			seedIDs = filterIDs
			matchedIDs = map[string]struct{}{}
			for _, id := range filterIDs {
				matchedIDs[id.String()] = struct{}{}
			}
		} else {
			filterSet := make(map[uuid.UUID]struct{}, len(filterIDs))
			for _, id := range filterIDs {
				filterSet[id] = struct{}{}
			}
			intersect := seedIDs[:0]
			matchedIDs = map[string]struct{}{}
			for _, id := range seedIDs {
				if _, ok := filterSet[id]; ok {
					intersect = append(intersect, id)
					matchedIDs[id.String()] = struct{}{}
				}
			}
			seedIDs = intersect
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

// seedQueryParams builds the ListArgumentGraphSeeds parameter struct from the
// active filter set. Unset filters are passed as NULL (or as the empty array
// for tag_names) so the SQL predicate short-circuits to TRUE.
func seedQueryParams(f argumentGraphFilters, viewerID *uuid.UUID) db.ListArgumentGraphSeedsParams {
	params := db.ListArgumentGraphSeedsParams{
		ViewerID:    viewerID,
		TagNames:    f.Tags,
		ResultLimit: argumentGraphSeedLimit,
	}
	if f.Author != "" {
		a := f.Author
		params.AuthorUsername = &a
	}
	if f.Group != "" {
		g := f.Group
		params.GroupSlug = &g
	}
	if !f.Since.IsZero() {
		params.Since = pgtype.Timestamptz{Time: f.Since, Valid: true}
	}
	if f.Sourced != nil {
		v := *f.Sourced
		params.Sourced = &v
	}
	if f.Visibility != "" {
		v := f.Visibility
		params.Visibility = &v
	}
	if params.TagNames == nil {
		params.TagNames = []string{}
	}
	return params
}

func argumentGraphNodesFromRows(rows []db.ListArgumentGraphNodesForViewerRow) []views.ArgumentGraphNode {
	out := make([]views.ArgumentGraphNode, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.ArgumentGraphNode{
			ID:             row.ID,
			Slug:           row.Slug,
			Type:           row.NodeType,
			Title:          row.Title,
			Body:           row.Body,
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
			Body:           row.Body,
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
