package auth

import (
	"strings"
	"testing"
)

func TestSuggestIdentity_UsesDisplayNameWhenValid(t *testing.T) {
	t.Parallel()

	// A clean preferred_username should land on the new user row as-is so
	// they don't have to rename after their first sign-in.
	username, _ := suggestIdentity("oidc", Identity{
		Subject:     "sub-123",
		DisplayName: "pxct",
	})
	if username != "pxct" {
		t.Fatalf("expected DisplayName 'pxct' to be used, got %q", username)
	}
}

func TestSuggestIdentity_SanitisesDisplayName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw  string
		want string
	}{
		{"John Doe", "JohnDoe"},
		{"alice@example.com", "aliceexample.com"},
		{"  pxct  ", "pxct"},
		{"--alice--", "alice"},
		{"🌟dragon🌟", "dragon"},
	}
	for _, c := range cases {
		got, _ := suggestIdentity("oidc", Identity{
			Subject:     "sub-" + c.raw,
			DisplayName: c.raw,
		})
		if got != c.want {
			t.Errorf("DisplayName %q: got %q, want %q", c.raw, got, c.want)
		}
	}
}

func TestSuggestIdentity_FallsBackOnEmptyOrInvalidDisplayName(t *testing.T) {
	t.Parallel()

	// Empty, too-short-after-sanitising, or all-punctuation handles must
	// fall through to the u_<hash> placeholder rather than dropping a
	// blank username on the user row.
	for _, raw := range []string{"", "ab", "@@@@@", "..-.."} {
		username, _ := suggestIdentity("oidc", Identity{
			Subject:     "sub-fb",
			DisplayName: raw,
		})
		if !strings.HasPrefix(username, "u_") || len(username) != 2+8 {
			t.Errorf("DisplayName %q: expected u_<8hex> fallback, got %q", raw, username)
		}
	}
}

func TestSuggestIdentity_DeterministicPerSubjectFallback(t *testing.T) {
	t.Parallel()

	// When DisplayName is empty the username comes from a hash of
	// (provider, subject). Same input → same output; different inputs →
	// different outputs. Locks in the property that re-attempted signups
	// with the same identity don't churn through new placeholder
	// usernames each try.
	a, _ := suggestIdentity("github", Identity{Subject: "12345"})
	b, _ := suggestIdentity("github", Identity{Subject: "12345"})
	if a != b {
		t.Fatalf("expected same subject to map to same username, got %q vs %q", a, b)
	}

	c, _ := suggestIdentity("github", Identity{Subject: "67890"})
	if a == c {
		t.Fatalf("expected different subjects to map to different usernames, both %q", a)
	}

	d, _ := suggestIdentity("oidc", Identity{Subject: "12345"})
	if a == d {
		t.Fatalf("expected provider to be part of the hash, both providers gave %q", a)
	}
}

func TestSuggestIdentity_UsesVerifiedEmail(t *testing.T) {
	t.Parallel()

	_, email := suggestIdentity("oidc", Identity{
		Subject:       "sub-1",
		Email:         "alice@example.com",
		EmailVerified: true,
	})
	if email != "alice@example.com" {
		t.Fatalf("expected verified email, got %q", email)
	}
}

func TestSuggestIdentity_DropsUnverifiedEmail(t *testing.T) {
	t.Parallel()

	// An IdP-asserted-but-unverified email must not land on the new user
	// row, or it would block real signups under that address and double as
	// an unwarranted proof of ownership.
	_, email := suggestIdentity("oidc", Identity{
		Subject:       "sub-1",
		Email:         "victim@example.com",
		EmailVerified: false,
	})
	if email == "victim@example.com" {
		t.Fatalf("unverified email must not propagate to the new user")
	}
	if !strings.HasSuffix(email, "@no-email.local") {
		t.Fatalf("expected placeholder email, got %q", email)
	}
}
