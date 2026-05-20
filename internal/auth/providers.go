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
//	GOOGLE_CLIENT_ID, GOOGLE_CLIENT_SECRET
//	GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET
//
//	OIDC_PROVIDERS = "work,community"          comma-separated keys
//
//	# Always required, per OIDC provider:
//	OIDC_<KEY>_ISSUER                          the IdP's issuer URL
//	OIDC_<KEY>_CLIENT_ID
//	OIDC_<KEY>_CLIENT_SECRET
//
//	# Optional, per OIDC provider:
//	OIDC_<KEY>_LABEL                           display name (default: title-cased key)
//	OIDC_<KEY>_USERNAME_CLAIM                  ID-token claim used to seed the
//	                                           username (default: preferred_username)
//	OIDC_<KEY>_DISCOVERY_URL                   override the .well-known fetch URL
//	                                           (useful for IdPs at non-spec paths)
//	OIDC_<KEY>_AUTHORIZATION_ENDPOINT          override discovered values; setting
//	OIDC_<KEY>_TOKEN_ENDPOINT                  all three of authorization/token/
//	OIDC_<KEY>_JWKS_URI                        jwks skips discovery entirely.
//	OIDC_<KEY>_USERINFO_ENDPOINT
//	OIDC_<KEY>_END_SESSION_ENDPOINT            for RP-initiated logout when the
//	                                           IdP doesn't advertise it in discovery
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
		p, err := newOIDCProvider(ctx, "google", "Google", oidcSettings{
			Issuer:       "https://accounts.google.com",
			ClientID:     id,
			ClientSecret: secret,
			RedirectURL:  callbackURL(baseURL, "google"),
		})
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
		get := func(name string) string { return os.Getenv("OIDC_" + envKey + "_" + name) }

		label := get("LABEL")
		if label == "" {
			label = strings.Title(key) //nolint:staticcheck
		}
		settings := oidcSettings{
			Issuer:             get("ISSUER"),
			DiscoveryURL:       get("DISCOVERY_URL"),
			ClientID:           get("CLIENT_ID"),
			ClientSecret:       get("CLIENT_SECRET"),
			RedirectURL:        callbackURL(baseURL, "oidc:"+key),
			UsernameClaim:      get("USERNAME_CLAIM"),
			AuthEndpoint:       get("AUTHORIZATION_ENDPOINT"),
			TokenEndpoint:      get("TOKEN_ENDPOINT"),
			JWKSURI:            get("JWKS_URI"),
			UserInfoEndpoint:   get("USERINFO_ENDPOINT"),
			EndSessionEndpoint: get("END_SESSION_ENDPOINT"),
		}
		if settings.Issuer == "" || settings.ClientID == "" || settings.ClientSecret == "" {
			logger.Warn("oauth: skipping incomplete OIDC provider", "key", key)
			continue
		}
		p, err := newOIDCProvider(ctx, "oidc:"+key, label, settings)
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
