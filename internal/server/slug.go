package server

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/TuotHash/mindful-social/internal/db"
)

var slugRunRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify converts a node title into a URL-safe slug. Lowercases, collapses
// runs of non-alphanumerics into single '-', trims leading/trailing '-',
// caps at 80 characters. Empty or "new" (would collide with /nodes/new) fall
// back to the literal "node".
func slugify(title string) string {
	s := strings.ToLower(strings.TrimSpace(title))
	s = slugRunRe.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 80 {
		s = strings.TrimRight(s[:80], "-")
	}
	if s == "" || s == "new" {
		return "node"
	}
	return s
}

// uniqueSlug returns a slug not already taken on the nodes table. Tries the
// base, then base-2, base-3, …. There is a small race window between the
// check and the next CreateNode insert; the unique index on the column makes
// the worst case a 23505 error which the caller can surface as flash text.
func (s *Server) uniqueSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 2; i < 1000; i++ {
		_, err := s.queries.GetNodeBySlug(ctx, candidate)
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", errors.New("exhausted slug candidates")
}

// nodeSlugConstraint matches the unique-index name from migration 00007.
// Used to identify the specific 23505 we recover from in
// createNodeWithUniqueSlug (other unique violations should still surface
// as errors).
const nodeSlugConstraint = "nodes_slug_idx"

// createNodeWithUniqueSlug runs uniqueSlug + CreateNode in a retry loop so
// a concurrent insert that steals the candidate slug between our SELECT
// and INSERT doesn't surface as a generic "Could not create post" flash.
// build receives each fresh candidate so callers don't have to mutate a
// shared params struct.
func (s *Server) createNodeWithUniqueSlug(ctx context.Context, base string, build func(slug string) db.CreateNodeParams) (db.Node, error) {
	for attempt := 0; attempt < 5; attempt++ {
		slug, err := s.uniqueSlug(ctx, base)
		if err != nil {
			return db.Node{}, err
		}
		node, err := s.queries.CreateNode(ctx, build(slug))
		if err == nil {
			return node, nil
		}
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == nodeSlugConstraint {
			// Race: a parallel insert took the slug uniqueSlug just
			// reported free. uniqueSlug will skip past it on the next
			// attempt and we try again.
			continue
		}
		return db.Node{}, err
	}
	return db.Node{}, errors.New("slug retries exhausted")
}

func (s *Server) uniqueGroupSlug(ctx context.Context, base string) (string, error) {
	candidate := base
	for i := 2; i < 1000; i++ {
		_, err := s.queries.GetGroupBySlug(ctx, candidate)
		if errors.Is(err, pgx.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}
	return "", errors.New("exhausted group slug candidates")
}
