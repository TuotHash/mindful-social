package server

import "testing"

func TestIdentityLabel(t *testing.T) {
	cases := []struct {
		provider, want string
	}{
		{"password", "Password"},
		{"google", "Google"},
		{"github", "GitHub"},
		{"oidc:work", "Work via OIDC"},
		{"oidc:community", "Community via OIDC"},
		{"oidc:ALLCAPS", "ALLCAPS via OIDC"},
		{"oidc:", "OIDC"},
		{"unknown_provider", "unknown_provider"}, // unknown stays raw
	}
	for _, c := range cases {
		t.Run(c.provider, func(t *testing.T) {
			got := identityLabel(c.provider)
			if got != c.want {
				t.Fatalf("identityLabel(%q) = %q, want %q", c.provider, got, c.want)
			}
		})
	}
}
