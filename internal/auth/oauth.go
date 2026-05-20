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
	"net/url"
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
	// SessionID is the OIDC `sid` claim, identifying this specific browser
	// session at the IdP. Empty for IdPs that don't issue it (and for
	// non-OIDC providers like GitHub). When present, we persist it on our
	// own session so a later backchannel logout can target just this
	// device instead of every device the user signed in from.
	SessionID string
	// RawIDToken is the verified, raw-JWS id_token string. We stash it on
	// the session so a later RP-initiated logout can pass it back to the
	// IdP as `id_token_hint`. Empty for non-OIDC providers (GitHub).
	RawIDToken string
}

// LogoutClaims is the subset of an OIDC backchannel logout_token we act on.
// Either Subject or SessionID is non-empty — when SessionID is empty the
// IdP is asking us to log every session belonging to (provider, Subject)
// out at once.
type LogoutClaims struct {
	Subject   string // OIDC `sub` claim, may be empty if `sid` is present
	SessionID string // OIDC `sid` claim, may be empty if `sub` is present
}

// BackchannelLogoutSupporter is implemented by providers that can verify
// an OIDC backchannel logout_token. Non-OIDC providers (GitHub) intentionally
// don't implement it so the route handler can answer 400 cleanly.
type BackchannelLogoutSupporter interface {
	VerifyLogoutToken(ctx context.Context, rawToken string) (LogoutClaims, error)
}

// RPInitiatedLogoutSupporter is implemented by providers that know an
// `end_session_endpoint` for the IdP — i.e. providers wired up for OpenID
// Connect RP-Initiated Logout 1.0. The handler type-asserts to this on
// /logout so the user is bounced through the IdP's logout page in addition
// to having their local session destroyed.
//
// A provider may *implement* the interface yet still return "" when the
// IdP didn't advertise an end-session endpoint at discovery time. The
// caller treats empty the same as "interface not implemented": fall back
// to a local-only logout.
type RPInitiatedLogoutSupporter interface {
	// LogoutURL builds the redirect URL that ends the user's IdP session.
	// idTokenHint is the raw id_token previously issued for this session;
	// most IdPs require it to identify which session to terminate. The
	// post_logout_redirect_uri MUST be pre-registered with the IdP — it
	// won't be honoured otherwise.
	LogoutURL(idTokenHint, postLogoutRedirectURI string) string
}

// backchannelLogoutEvent is the JSON pointer the OIDC spec mandates inside
// the logout_token's `events` claim. Its value is an empty JSON object.
const backchannelLogoutEvent = "http://schemas.openid.net/event/backchannel-logout"

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
	// endSessionURL is the IdP's RP-initiated logout endpoint, read out
	// of the discovery document at construction time. Empty when the IdP
	// (e.g. Google) doesn't advertise one — LogoutURL then returns ""
	// and the caller falls back to a local-only logout.
	endSessionURL string
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
		SID           string `json:"sid"`
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
		SessionID:     claims.SID,
		RawIDToken:    rawID,
	}, nil
}

// LogoutURL builds the RP-initiated logout URL per OpenID Connect
// RP-Initiated Logout 1.0 §3. Returns "" when the IdP didn't advertise
// an end_session_endpoint, when both id_token_hint and post_logout_redirect_uri
// are empty (the spec requires at least one to identify the session), or
// when the discovered URL is malformed (a sanity check; we already
// validated it at construction time).
func (p *oidcProvider) LogoutURL(idTokenHint, postLogoutRedirectURI string) string {
	if p.endSessionURL == "" {
		return ""
	}
	u, err := url.Parse(p.endSessionURL)
	if err != nil {
		return ""
	}
	q := u.Query()
	if idTokenHint != "" {
		q.Set("id_token_hint", idTokenHint)
	}
	if postLogoutRedirectURI != "" {
		q.Set("post_logout_redirect_uri", postLogoutRedirectURI)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// VerifyLogoutToken validates a logout_token per OpenID Connect Back-Channel
// Logout 1.0 §2.6. It reuses the ID-token verifier for signature, issuer,
// audience and expiry checks, then enforces the logout-specific claims:
// the `events` claim must contain the backchannel-logout member, at least
// one of `sub`/`sid` must be present, and `nonce` must NOT be present.
func (p *oidcProvider) VerifyLogoutToken(ctx context.Context, raw string) (LogoutClaims, error) {
	tok, err := p.verifier.Verify(ctx, raw)
	if err != nil {
		return LogoutClaims{}, fmt.Errorf("oidc: verify logout_token: %w", err)
	}
	var claims struct {
		Subject string                     `json:"sub"`
		SID     string                     `json:"sid"`
		Nonce   string                     `json:"nonce"`
		Events  map[string]json.RawMessage `json:"events"`
	}
	if err := tok.Claims(&claims); err != nil {
		return LogoutClaims{}, fmt.Errorf("oidc: parse logout_token: %w", err)
	}
	if claims.Subject == "" && claims.SID == "" {
		return LogoutClaims{}, errors.New("oidc: logout_token missing both sub and sid")
	}
	if claims.Nonce != "" {
		return LogoutClaims{}, errors.New("oidc: logout_token must not contain a nonce claim")
	}
	if _, ok := claims.Events[backchannelLogoutEvent]; !ok {
		return LogoutClaims{}, errors.New("oidc: logout_token events claim missing backchannel-logout member")
	}
	return LogoutClaims{Subject: claims.Subject, SessionID: claims.SID}, nil
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
	// end_session_endpoint isn't in go-oidc's typed struct, so dip into
	// the raw discovery JSON. A missing or malformed entry is fine: we
	// just disable RP-initiated logout for this provider.
	var extra struct {
		EndSessionEndpoint string `json:"end_session_endpoint"`
	}
	_ = prov.Claims(&extra)
	if extra.EndSessionEndpoint != "" {
		if _, err := url.Parse(extra.EndSessionEndpoint); err != nil {
			extra.EndSessionEndpoint = ""
		}
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
		endSessionURL: extra.EndSessionEndpoint,
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
