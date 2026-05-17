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

// setTagsForNode replaces the node's tag set with the given names. The
// delete and the per-tag attach run inside a single transaction so a
// timeout, network blip, or concurrent save can't leave a partial tag
// set (otherwise the node would briefly carry zero tags or only some of
// them, depending on where the loop stopped).
func (s *Server) setTagsForNode(r *http.Request, nodeID uuid.UUID, names []string) error {
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.queries.WithTx(tx)
	if err := q.DeleteTagsForNode(ctx, nodeID); err != nil {
		return err
	}
	for _, name := range names {
		tagID, err := q.UpsertTag(ctx, name)
		if err != nil {
			return err
		}
		if err := q.AttachTag(ctx, db.AttachTagParams{NodeID: nodeID, TagID: tagID}); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// handleTagsIndex renders /tags: every tag with the count of nodes carrying it.
// Counts are filtered through node_visible_to() so a viewer only sees tags
// (and counts) backed by nodes they're permitted to read — a tag that only
// lives on private nodes is hidden entirely from non-authors.
func (s *Server) handleTagsIndex(w http.ResponseWriter, r *http.Request) {
	tags, err := s.queries.ListAllTagsForViewer(r.Context(), viewerID(r))
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
	nodes, err := s.queries.ListNodesWithTagForViewer(r.Context(), db.ListNodesWithTagForViewerParams{
		TagID:    tag.ID,
		ViewerID: viewerID(r),
	})
	if err != nil {
		s.logger.Error("tag detail: list nodes", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	render(w, r, views.TagDetail(viewerFor(currentUser(r)), tag.Name, nodes))
}
