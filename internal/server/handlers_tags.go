package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
	"github.com/TuotHash/mindful-social/internal/views"
)

const (
	maxTagsPerNode = 20
	maxTagLength   = 50
)

// parseTagsInput turns a comma-separated user input string into a normalized,
// deduped list of tag names. Each name is lowercased, whitespace and
// disallowed punctuation are folded into hyphens, and adjacent hyphens
// collapse so "Sugar Tax!!" → "sugar-tax". Allowed characters: a–z, 0–9,
// underscore, hyphen. The list is capped at maxTagsPerNode and each tag at
// maxTagLength runes. Returns nil (not an empty slice) when no valid tags
// are produced, so callers can compare against nil naturally.
func parseTagsInput(raw string) []string {
	parts := strings.Split(raw, ",")
	seen := make(map[string]bool, len(parts))
	var out []string
	for _, p := range parts {
		t := normalizeTag(p)
		if t == "" || seen[t] {
			continue
		}
		if len([]rune(t)) > maxTagLength {
			t = string([]rune(t)[:maxTagLength])
		}
		seen[t] = true
		out = append(out, t)
		if len(out) >= maxTagsPerNode {
			break
		}
	}
	return out
}

func normalizeTag(s string) string {
	var b strings.Builder
	prevHyphen := true // suppresses leading hyphens
	for _, r := range strings.ToLower(s) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_':
			b.WriteRune(r)
			prevHyphen = false
		default:
			// Anything else (whitespace, punctuation, accents) becomes a hyphen,
			// but we don't emit consecutive hyphens.
			if !prevHyphen {
				b.WriteByte('-')
				prevHyphen = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// setTagsForNode replaces the node's tag set with the given names. Each name
// is upserted into tags, then attached. Existing tags not in the new set are
// removed. Errors are returned but not reported to the user — tag editing is
// a soft path.
func (s *Server) setTagsForNode(r *http.Request, nodeID uuid.UUID, names []string) error {
	ctx := r.Context()
	if err := s.queries.DeleteTagsForNode(ctx, nodeID); err != nil {
		return err
	}
	for _, name := range names {
		tagID, err := s.queries.UpsertTag(ctx, name)
		if err != nil {
			return err
		}
		if err := s.queries.AttachTag(ctx, db.AttachTagParams{NodeID: nodeID, TagID: tagID}); err != nil {
			return err
		}
	}
	return nil
}

// handleTagsIndex renders /tags: every tag with the count of nodes carrying it.
func (s *Server) handleTagsIndex(w http.ResponseWriter, r *http.Request) {
	tags, err := s.queries.ListAllTags(r.Context())
	if err != nil {
		s.logger.Error("tags index", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.TagsIndex(viewerFor(currentUser(r)), tags))
}

// handleTagDetail renders /tags/{name}: nodes that carry the named tag.
func (s *Server) handleTagDetail(w http.ResponseWriter, r *http.Request) {
	name := strings.ToLower(strings.TrimSpace(chiURLParam(r, "name")))
	if name == "" {
		http.NotFound(w, r)
		return
	}
	tag, err := s.queries.GetTagByName(r.Context(), name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("tag detail: get tag", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	nodes, err := s.queries.ListNodesWithTag(r.Context(), tag.ID)
	if err != nil {
		s.logger.Error("tag detail: list nodes", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.TagDetail(viewerFor(currentUser(r)), tag.Name, nodes))
}
