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
	"strings"

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

// oidcSettings is the union of everything an OIDC provider can be
// configured with. The only always-required fields are Issuer, ClientID,
// ClientSecret, and RedirectURL — every endpoint URL can either come
// from the discovery document or be set explicitly to override (or
// replace) discovery.
//
// Modes the same struct supports:
//
//   - Discovery (default): set Issuer; we GET <Issuer>/.well-known/openid-configuration.
//   - Custom discovery URL: set Issuer AND DiscoveryURL; we GET the URL
//     and validate the doc's `issuer` claim matches Issuer.
//   - Full manual: set Issuer plus AuthEndpoint + TokenEndpoint + JWKSURI;
//     discovery is skipped entirely. UserInfoEndpoint and EndSessionEndpoint
//     remain optional.
//
// Any individual endpoint set on the struct wins over a discovered value,
// so operators can override one piece without going full-manual.
type oidcSettings struct {
	Issuer        string
	DiscoveryURL  string
	ClientID      string
	ClientSecret  string
	RedirectURL   string
	UsernameClaim string
	Scopes        []string

	// Endpoint overrides; any field set here replaces the discovered value.
	// Setting all three of AuthEndpoint, TokenEndpoint, and JWKSURI skips
	// discovery entirely.
	AuthEndpoint       string
	TokenEndpoint      string
	JWKSURI            string
	UserInfoEndpoint   string
	EndSessionEndpoint string
}

// oidcDiscoveryDoc mirrors the fields of the OIDC discovery document we
// care about. Unknown fields are ignored — we deliberately don't shadow
// every field go-oidc tracks, only the ones this codebase uses.
type oidcDiscoveryDoc struct {
	Issuer             string   `json:"issuer"`
	AuthEndpoint       string   `json:"authorization_endpoint"`
	TokenEndpoint      string   `json:"token_endpoint"`
	JWKSURI            string   `json:"jwks_uri"`
	UserInfoEndpoint   string   `json:"userinfo_endpoint"`
	EndSessionEndpoint string   `json:"end_session_endpoint"`
	SigningAlgs        []string `json:"id_token_signing_alg_values_supported"`
}

// fetchOIDCDiscovery GETs the discovery document from `discoveryURL` if
// non-empty, otherwise from `<expectedIssuer>/.well-known/openid-configuration`.
// When expectedIssuer is non-empty, the doc's `issuer` claim must match —
// without that anchor, a bogus DISCOVERY_URL could route us to an
// attacker-controlled IdP.
func fetchOIDCDiscovery(ctx context.Context, expectedIssuer, discoveryURL string) (oidcDiscoveryDoc, error) {
	var doc oidcDiscoveryDoc

	fetchURL := discoveryURL
	if fetchURL == "" {
		if expectedIssuer == "" {
			return doc, errors.New("oidc: discovery requires either issuer or discovery URL")
		}
		fetchURL = strings.TrimRight(expectedIssuer, "/") + "/.well-known/openid-configuration"
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fetchURL, nil)
	if err != nil {
		return doc, fmt.Errorf("oidc: discovery request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return doc, fmt.Errorf("oidc: fetch discovery from %s: %w", fetchURL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return doc, fmt.Errorf("oidc: discovery from %s: status %d", fetchURL, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return doc, fmt.Errorf("oidc: decode discovery: %w", err)
	}
	if expectedIssuer != "" && doc.Issuer != expectedIssuer {
		return doc, fmt.Errorf("oidc: discovery doc issuer %q does not match configured issuer %q", doc.Issuer, expectedIssuer)
	}
	return doc, nil
}

func newOIDCProvider(ctx context.Context, key, label string, s oidcSettings) (Provider, error) {
	if s.ClientID == "" || s.ClientSecret == "" {
		return nil, fmt.Errorf("oidc[%s]: missing client_id or client_secret", key)
	}
	if s.Issuer == "" {
		// We always need an issuer string for ID-token validation, even
		// in full-manual mode — the `iss` claim of any token we accept
		// must match this value.
		return nil, fmt.Errorf("oidc[%s]: missing issuer", key)
	}

	authEndpoint := s.AuthEndpoint
	tokenEndpoint := s.TokenEndpoint
	jwksURI := s.JWKSURI
	userInfoEndpoint := s.UserInfoEndpoint
	endSessionEndpoint := s.EndSessionEndpoint
	var algorithms []string

	// Discovery is needed only when at least one of the three critical
	// endpoints (auth, token, jwks) is missing. The other two (userinfo,
	// end_session) we'll backfill from discovery too if we end up doing
	// it, but their absence alone doesn't trigger a fetch.
	if authEndpoint == "" || tokenEndpoint == "" || jwksURI == "" || s.DiscoveryURL != "" {
		doc, err := fetchOIDCDiscovery(ctx, s.Issuer, s.DiscoveryURL)
		if err != nil {
			return nil, err
		}
		if authEndpoint == "" {
			authEndpoint = doc.AuthEndpoint
		}
		if tokenEndpoint == "" {
			tokenEndpoint = doc.TokenEndpoint
		}
		if jwksURI == "" {
			jwksURI = doc.JWKSURI
		}
		if userInfoEndpoint == "" {
			userInfoEndpoint = doc.UserInfoEndpoint
		}
		if endSessionEndpoint == "" {
			endSessionEndpoint = doc.EndSessionEndpoint
		}
		algorithms = doc.SigningAlgs
	}

	if authEndpoint == "" || tokenEndpoint == "" || jwksURI == "" {
		return nil, fmt.Errorf("oidc[%s]: missing required endpoint after discovery+overrides (need auth, token, jwks)", key)
	}
	if endSessionEndpoint != "" {
		if _, err := url.Parse(endSessionEndpoint); err != nil {
			// A bad end_session_endpoint disables RP-initiated logout
			// for this provider but mustn't take down sign-in.
			endSessionEndpoint = ""
		}
	}

	scopes := s.Scopes
	if len(scopes) == 0 {
		scopes = []string{oidc.ScopeOpenID, "email", "profile"}
	}
	usernameClaim := s.UsernameClaim
	if usernameClaim == "" {
		usernameClaim = "preferred_username"
	}

	// One provider construction path for every mode: go-oidc's
	// ProviderConfig.NewProvider accepts the endpoints + algs verbatim,
	// and the resulting verifier handles JWKS + issuer validation
	// identically to one built via discovery.
	provCfg := &oidc.ProviderConfig{
		IssuerURL:   s.Issuer,
		AuthURL:     authEndpoint,
		TokenURL:    tokenEndpoint,
		JWKSURL:     jwksURI,
		UserInfoURL: userInfoEndpoint,
		Algorithms:  algorithms,
	}
	prov := provCfg.NewProvider(ctx)
	return &oidcProvider{
		key:      key,
		label:    label,
		verifier: prov.Verifier(&oidc.Config{ClientID: s.ClientID}),
		oauth: &oauth2.Config{
			ClientID:     s.ClientID,
			ClientSecret: s.ClientSecret,
			Endpoint:     oauth2.Endpoint{AuthURL: authEndpoint, TokenURL: tokenEndpoint},
			RedirectURL:  s.RedirectURL,
			Scopes:       scopes,
		},
		usernameClaim: usernameClaim,
		endSessionURL: endSessionEndpoint,
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
