package server

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
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
