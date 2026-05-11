package auth

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strings"
)

// LoadProviders reads OAuth config from the environment and returns the set
// of providers that are fully configured. Anything missing is logged and
// skipped, so the app starts fine with zero providers configured (only
// password auth available).
//
// baseURL is used to build callback URLs; it must be the public origin the
// browser sees (e.g., https://mindful.example.org).
//
// Recognized env vars:
//
//   GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
//   GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET
//   OIDC_PROVIDERS = "work,community"      (comma-separated keys)
//   OIDC_<KEY>_ISSUER, OIDC_<KEY>_CLIENT_ID, OIDC_<KEY>_CLIENT_SECRET,
//     OIDC_<KEY>_LABEL (optional, defaults to the key, title-cased),
//     OIDC_<KEY>_USERNAME_CLAIM (optional, defaults to "preferred_username";
//       the ID-token claim used to seed the username on first sign-in)
func LoadProviders(ctx context.Context, logger *slog.Logger, baseURL string) (*Registry, error) {
	if baseURL == "" {
		return &Registry{providers: map[string]Provider{}}, nil
	}
	if _, err := url.Parse(baseURL); err != nil {
		return nil, fmt.Errorf("invalid PUBLIC_BASE_URL %q: %w", baseURL, err)
	}
	r := &Registry{providers: map[string]Provider{}}

	// Google (OIDC)
	if id, secret := os.Getenv("GOOGLE_CLIENT_ID"), os.Getenv("GOOGLE_CLIENT_SECRET"); id != "" && secret != "" {
		p, err := newOIDCProvider(ctx, "google", "Google",
			"https://accounts.google.com",
			id, secret,
			callbackURL(baseURL, "google"),
			"", nil)
		if err != nil {
			logger.Warn("oauth: google init failed", "err", err)
		} else {
			r.add(p)
		}
	}

	// GitHub (plain OAuth2)
	if id, secret := os.Getenv("GITHUB_CLIENT_ID"), os.Getenv("GITHUB_CLIENT_SECRET"); id != "" && secret != "" {
		r.add(newGitHubProvider(id, secret, callbackURL(baseURL, "github")))
	}

	// Custom OIDC providers (Authelia, Authentik, Keycloak, Zitadel, …)
	for _, key := range splitCSV(os.Getenv("OIDC_PROVIDERS")) {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		envKey := strings.ToUpper(key)
		issuer := os.Getenv("OIDC_" + envKey + "_ISSUER")
		id := os.Getenv("OIDC_" + envKey + "_CLIENT_ID")
		secret := os.Getenv("OIDC_" + envKey + "_CLIENT_SECRET")
		label := os.Getenv("OIDC_" + envKey + "_LABEL")
		usernameClaim := os.Getenv("OIDC_" + envKey + "_USERNAME_CLAIM")
		if label == "" {
			label = strings.Title(key) //nolint:staticcheck
		}
		if issuer == "" || id == "" || secret == "" {
			logger.Warn("oauth: skipping incomplete OIDC provider", "key", key)
			continue
		}
		p, err := newOIDCProvider(ctx, "oidc:"+key, label, issuer, id, secret,
			callbackURL(baseURL, "oidc:"+key), usernameClaim, nil)
		if err != nil {
			logger.Warn("oauth: OIDC init failed", "key", key, "err", err)
			continue
		}
		r.add(p)
	}

	if len(r.order) == 0 {
		logger.Info("oauth: no providers configured (password auth only)")
	} else {
		logger.Info("oauth: providers configured", "providers", r.order)
	}
	return r, nil
}

func (r *Registry) add(p Provider) {
	if _, exists := r.providers[p.Key()]; exists {
		return
	}
	r.providers[p.Key()] = p
	r.order = append(r.order, p.Key())
}

func callbackURL(base, key string) string {
	base = strings.TrimRight(base, "/")
	return base + "/auth/callback/" + url.PathEscape(key)
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
