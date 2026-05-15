package server

import (
	"net/http"
	"strings"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const searchResultLimit = 50

// handleSearch renders /search?q=...: full-text matches across node titles
// and bodies, plus matching usernames and groups. Groups results are gated
// by the same visibility branches that ListVisibleGroups uses, so search
// can't leak a private group's existence to a non-member.
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	viewer := viewerFor(currentUser(r))
	if q == "" {
		render(w, r, views.SearchResults(viewer, "", nil, nil, nil))
		return
	}
	nodeRows, err := s.queries.SearchNodes(r.Context(), db.SearchNodesParams{
		Query:       q,
		ResultLimit: searchResultLimit,
		ViewerID:    viewerID(r),
	})
	if err != nil {
		s.logger.Error("search nodes", "err", err, "q", q)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	userRows, err := s.queries.SearchUsers(r.Context(), db.SearchUsersParams{
		Query:       q,
		ResultLimit: searchResultLimit,
	})
	if err != nil {
		s.logger.Error("search users", "err", err, "q", q)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	groupRows, err := s.queries.SearchGroups(r.Context(), db.SearchGroupsParams{
		Query:       q,
		ResultLimit: searchResultLimit,
		ViewerID:    viewerID(r),
	})
	if err != nil {
		s.logger.Error("search groups", "err", err, "q", q)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.SearchResults(viewer, q, searchHits(nodeRows), userHits(userRows), groupHits(groupRows)))
}

func searchHits(rows []db.SearchNodesRow) []views.SearchHit {
	out := make([]views.SearchHit, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.SearchHit{
			ID:             row.ID,
			Slug:           row.Slug,
			Type:           row.Type,
			Title:          row.Title,
			AuthorUsername: row.AuthorUsername,
			Excerpt:        parseHighlightedExcerpt(row.Excerpt),
		})
	}
	return out
}

func userHits(rows []db.SearchUsersRow) []views.UserSearchHit {
	out := make([]views.UserSearchHit, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.UserSearchHit{
			ID:       row.ID,
			Username: row.Username,
		})
	}
	return out
}

func groupHits(rows []db.SearchGroupsRow) []views.GroupSearchHit {
	out := make([]views.GroupSearchHit, 0, len(rows))
	for _, row := range rows {
		out = append(out, views.GroupSearchHit{
			ID:          row.ID,
			Slug:        row.Slug,
			Name:        row.Name,
			Description: row.Description,
			Visibility:  row.Visibility,
			MemberCount: row.MemberCount,
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
