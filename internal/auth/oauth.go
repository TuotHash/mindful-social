package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/endpoints"
)

// Provider abstracts over OAuth2/OIDC IdPs. Each provider knows how to:
//   - hand back the authorize URL,
//   - exchange a code for tokens,
//   - extract the user's stable subject + email + display name.
//
// Generic OIDC providers use the OIDC discovery + ID-token path.
// Non-OIDC providers (GitHub) implement Identify by hitting their user API
// directly.
type Provider interface {
	Key() string
	Label() string
	AuthURL(state string) string
	Identify(ctx context.Context, code string) (Identity, error)
}

// Identity is the per-login result from a provider, just enough to look up
// or create a user.
type Identity struct {
	Subject       string // stable, opaque user id at the IdP
	Email         string // may be empty
	EmailVerified bool   // true only when the provider explicitly attests it
	DisplayName   string // may be empty
}

// ----- OIDC (generic, also Google) -----

type oidcProvider struct {
	key      string
	label    string
	verifier *oidc.IDTokenVerifier
	oauth    *oauth2.Config
	// usernameClaim is the ID-token claim used to seed the new user's
	// username on first sign-in. Defaults to "preferred_username"; can
	// be set to "name", "nickname", "email", "sub", or any custom
	// string claim the IdP issues.
	usernameClaim string
}

func (p *oidcProvider) Key() string   { return p.key }
func (p *oidcProvider) Label() string { return p.label }

func (p *oidcProvider) AuthURL(state string) string {
	return p.oauth.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func (p *oidcProvider) Identify(ctx context.Context, code string) (Identity, error) {
	tok, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: exchange: %w", err)
	}
	rawID, ok := tok.Extra("id_token").(string)
	if !ok {
		return Identity{}, errors.New("oidc: response missing id_token")
	}
	idTok, err := p.verifier.Verify(ctx, rawID)
	if err != nil {
		return Identity{}, fmt.Errorf("oidc: verify id_token: %w", err)
	}
	var claims struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := idTok.Claims(&claims); err != nil {
		return Identity{}, fmt.Errorf("oidc: parse claims: %w", err)
	}
	// Read the configured username claim out of the raw claim set so
	// it can be any string-valued field the IdP issues (not just one
	// of the standard names).
	var raw map[string]any
	if err := idTok.Claims(&raw); err != nil {
		return Identity{}, fmt.Errorf("oidc: parse claims: %w", err)
	}
	display, _ := raw[p.usernameClaim].(string)
	if display == "" {
		display = claims.Name
	}
	return Identity{
		Subject:       claims.Sub,
		Email:         claims.Email,
		EmailVerified: claims.EmailVerified,
		DisplayName:   display,
	}, nil
}

func newOIDCProvider(ctx context.Context, key, label, issuer, clientID, clientSecret, redirectURL, usernameClaim string, scopes []string) (Provider, error) {
	prov, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discover %s: %w", issuer, err)
	}
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	if usernameClaim == "" {
		usernameClaim = "preferred_username"
	}
	return &oidcProvider{
		key:      key,
		label:    label,
		verifier: prov.Verifier(&oidc.Config{ClientID: clientID}),
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     prov.Endpoint(),
			RedirectURL:  redirectURL,
			Scopes:       scopes,
		},
		usernameClaim: usernameClaim,
	}, nil
}

// ----- GitHub (OAuth2 only, no OIDC) -----

type githubProvider struct {
	oauth *oauth2.Config
}

func (p *githubProvider) Key() string   { return "github" }
func (p *githubProvider) Label() string { return "GitHub" }

func (p *githubProvider) AuthURL(state string) string {
	return p.oauth.AuthCodeURL(state, oauth2.AccessTypeOnline)
}

func (p *githubProvider) Identify(ctx context.Context, code string) (Identity, error) {
	tok, err := p.oauth.Exchange(ctx, code)
	if err != nil {
		return Identity{}, fmt.Errorf("github: exchange: %w", err)
	}
	client := p.oauth.Client(ctx, tok)

	// /user gives us id, login, name, and sometimes a public email. The
	// public email is not necessarily verified — GitHub lets users mark any
	// email public — so we never treat it as verified.
	var userResp struct {
		ID    int64  `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
		Email string `json:"email"`
	}
	if err := getJSON(ctx, client, "https://api.github.com/user", &userResp); err != nil {
		return Identity{}, err
	}

	// Always pull /user/emails. The user:email scope guarantees access; if
	// it fails we treat the response as if there were no verified primary.
	var email string
	var emailVerified bool
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := getJSON(ctx, client, "https://api.github.com/user/emails", &emails); err == nil {
		for _, e := range emails {
			if e.Primary && e.Verified {
				email = e.Email
				emailVerified = true
				break
			}
		}
	}
	if email == "" && userResp.Email != "" {
		// Last-resort fallback: surface whatever email GitHub returned on
		// /user so the new account has a stable handle for the user. Mark
		// it unverified so the downstream linker doesn't treat it as proof
		// of address ownership.
		email = userResp.Email
		emailVerified = false
	}

	display := userResp.Name
	if display == "" {
		display = userResp.Login
	}
	return Identity{
		Subject:       strconv.FormatInt(userResp.ID, 10),
		Email:         email,
		EmailVerified: emailVerified,
		DisplayName:   display,
	}, nil
}

func newGitHubProvider(clientID, clientSecret, redirectURL string) Provider {
	return &githubProvider{
		oauth: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			Endpoint:     endpoints.GitHub,
			RedirectURL:  redirectURL,
			Scopes:       []string{"read:user", "user:email"},
		},
	}
}

// ----- registry + state generation -----

// Registry is an ordered, key-addressable bundle of providers. Keys mirror
// the path used in /auth/callback/{key}.
type Registry struct {
	order     []string
	providers map[string]Provider
}

func (r *Registry) Get(key string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	p, ok := r.providers[key]
	return p, ok
}

// All returns providers in insertion order — used by templates to render the
// "Sign in with…" buttons consistently across pages.
func (r *Registry) All() []Provider {
	if r == nil {
		return nil
	}
	out := make([]Provider, 0, len(r.order))
	for _, k := range r.order {
		out = append(out, r.providers[k])
	}
	return out
}

func (r *Registry) Empty() bool { return r == nil || len(r.order) == 0 }

// NewState returns a high-entropy random string used as the OAuth `state`
// parameter to bind authorize-redirects to callbacks.
func NewState() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

// ----- helpers -----

func getJSON(ctx context.Context, client *http.Client, url string, into any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("GET %s: status %d: %s", url, resp.StatusCode, string(body))
	}
	return json.NewDecoder(resp.Body).Decode(into)
}
