package auth

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

// newTestOIDCProvider returns a bare oidcProvider with just the field
// LogoutURL reads. Avoids the full discovery dance — we test URL
// composition, not the IdP integration.
func newTestOIDCProvider(endSessionURL string) *oidcProvider {
	return &oidcProvider{
		key:           "oidc:test",
		label:         "Test",
		endSessionURL: endSessionURL,
	}
}

func TestLogoutURL_EmptyWhenNoEndSession(t *testing.T) {
	t.Parallel()
	p := newTestOIDCProvider("")
	if got := p.LogoutURL("id-token", "https://app.example/"); got != "" {
		t.Errorf("expected empty URL when end_session_endpoint missing, got %q", got)
	}
}

func TestLogoutURL_ComposesQueryParams(t *testing.T) {
	t.Parallel()
	p := newTestOIDCProvider("https://idp.test/logout")
	got := p.LogoutURL("the-id-token", "https://app.example/")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if u.Scheme != "https" || u.Host != "idp.test" || u.Path != "/logout" {
		t.Errorf("base URL not preserved: got %q", got)
	}
	q := u.Query()
	if q.Get("id_token_hint") != "the-id-token" {
		t.Errorf("id_token_hint: got %q, want %q", q.Get("id_token_hint"), "the-id-token")
	}
	if q.Get("post_logout_redirect_uri") != "https://app.example/" {
		t.Errorf("post_logout_redirect_uri: got %q, want %q", q.Get("post_logout_redirect_uri"), "https://app.example/")
	}
}

func TestLogoutURL_PreservesExistingQuery(t *testing.T) {
	t.Parallel()
	// Some IdPs publish end_session_endpoint with a tenant parameter
	// baked in (e.g. ?realm=foo). Don't clobber it.
	p := newTestOIDCProvider("https://idp.test/logout?realm=foo")
	got := p.LogoutURL("tok", "https://app.example/")

	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	if q.Get("realm") != "foo" {
		t.Errorf("pre-existing query param 'realm' dropped: got %q", got)
	}
	if q.Get("id_token_hint") != "tok" {
		t.Errorf("id_token_hint missing: got %q", got)
	}
}

func TestLogoutURL_OmitsBlankParams(t *testing.T) {
	t.Parallel()
	p := newTestOIDCProvider("https://idp.test/logout")
	got := p.LogoutURL("", "")
	// Empty hint and post-logout redirect: don't add empty-string params
	// to the URL. The IdP either takes the user to a generic logged-out
	// page or refuses — neither outcome is helped by ?id_token_hint=.
	if strings.Contains(got, "id_token_hint=") {
		t.Errorf("URL contains empty id_token_hint: %q", got)
	}
	if strings.Contains(got, "post_logout_redirect_uri=") {
		t.Errorf("URL contains empty post_logout_redirect_uri: %q", got)
	}
}

// oidcProvider implements the interface — verify statically so the
// handler's type assertion can't silently break.
func TestOIDCProviderImplementsRPInitiatedLogoutSupporter(t *testing.T) {
	t.Parallel()
	var _ RPInitiatedLogoutSupporter = (*oidcProvider)(nil)
}

// ---------- session storage ----------

func TestRecordOIDCLogin_StoresIDToken(t *testing.T) {
	t.Parallel()
	sm := newMemSessionManager()
	ctx, err := sm.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	RecordOIDCLogin(ctx, sm, "oidc:work", "user-1", "sid-1", "raw.jwt.token")

	prov, idToken := OIDCSessionLogout(ctx, sm)
	if prov != "oidc:work" {
		t.Errorf("provider: got %q, want %q", prov, "oidc:work")
	}
	if idToken != "raw.jwt.token" {
		t.Errorf("idToken: got %q, want %q", idToken, "raw.jwt.token")
	}
}

func TestRecordOIDCLogin_EmptyIDTokenLeavesKeyUnset(t *testing.T) {
	t.Parallel()
	sm := newMemSessionManager()
	ctx, err := sm.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	RecordOIDCLogin(ctx, sm, "github", "42", "", "")

	_, idToken := OIDCSessionLogout(ctx, sm)
	if idToken != "" {
		t.Errorf("expected empty idToken for plain-OAuth2 login, got %q", idToken)
	}
}

func TestOIDCSessionLogout_EmptyForPasswordLogin(t *testing.T) {
	t.Parallel()
	sm := newMemSessionManager()
	ctx, err := sm.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Password login never calls RecordOIDCLogin: provider key stays
	// empty, signalling the handler to skip the IdP-logout bounce.
	prov, idToken := OIDCSessionLogout(ctx, sm)
	if prov != "" || idToken != "" {
		t.Errorf("expected empty (provider, idToken) for password session, got (%q, %q)", prov, idToken)
	}
}
