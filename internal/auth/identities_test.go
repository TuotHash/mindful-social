package auth

import (
	"strings"
	"testing"
)

func TestSuggestIdentity_IgnoresDisplayName(t *testing.T) {
	t.Parallel()

	// Attacker sets DisplayName to "alice" hoping to squat that handle.
	// The fix is to derive the placeholder username from a stable hash of
	// the provider+subject pair instead.
	username, _ := suggestIdentity("oidc", Identity{
		Subject:     "attacker-subject-123",
		DisplayName: "alice",
		Email:       "mallory@example.com",
	})
	if username == "alice" {
		t.Fatalf("suggestIdentity must not use DisplayName as username")
	}
	if !strings.HasPrefix(username, "u_") {
		t.Fatalf("expected u_-prefixed placeholder, got %q", username)
	}
	if len(username) != 2+8 {
		t.Fatalf("expected 10-char placeholder (u_ + 8 hex), got %q", username)
	}
}

func TestSuggestIdentity_DeterministicPerSubject(t *testing.T) {
	t.Parallel()

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
