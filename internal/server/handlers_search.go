package server

import (
	"net/http"
	"strings"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const searchResultLimit = 50

// handleSearch renders /search?q=...: full-text matches across node titles
// and bodies, ranked by relevance.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	viewer := viewerFor(currentUser(r))
	if q == "" {
		render(w, r, views.SearchResults(viewer, "", nil))
		return
	}
	rows, err := s.queries.SearchNodes(r.Context(), db.SearchNodesParams{
		Query:       q,
		ResultLimit: searchResultLimit,
		ViewerID:    viewerID(r),
	})
	if err != nil {
		s.logger.Error("search", "err", err, "q", q)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.SearchResults(viewer, q, searchHits(rows)))
}

func searchHits(rows []db.SearchNodesRow) []views.SearchHit {
	out := make([]views.SearchHit, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.SearchHit{
			ID:      row.ID,
			Slug:    row.Slug,
			Type:    row.Type,
			Title:   row.Title,
			Excerpt: parseHighlightedExcerpt(row.Excerpt),
		})
	}
	return out
}

// Sentinel tokens emitted by ts_headline (see queries/nodes.sql). Chosen
// so they cannot collide with normal user-authored markup, since the body
// is rendered through templ's auto-escape after splitting.
const (
	hlStart = "«HL»"
	hlEnd   = "«/HL»"
)

func parseHighlightedExcerpt(s string) []views.ExcerptPart {
	if s == "" {
		return nil
	}
	parts := make([]views.ExcerptPart, 0, 4)
	rest := s
	for len(rest) > 0 {
		i := strings.Index(rest, hlStart)
		if i < 0 {
			parts = append(parts, views.ExcerptPart{Text: rest})
			break
		}
		if i > 0 {
			parts = append(parts, views.ExcerptPart{Text: rest[:i]})
		}
		rest = rest[i+len(hlStart):]
		j := strings.Index(rest, hlEnd)
		if j < 0 {
			parts = append(parts, views.ExcerptPart{Text: rest, Match: true})
			break
		}
		parts = append(parts, views.ExcerptPart{Text: rest[:j], Match: true})
		rest = rest[j+len(hlEnd):]
	}
	return parts
}
