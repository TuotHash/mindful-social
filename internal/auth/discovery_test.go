package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// servedDiscovery stands up an httptest server that returns the given
// doc at /.well-known/openid-configuration, plus a /custom path for the
// custom-DISCOVERY_URL case. Use the returned URL as the issuer (the
// well-known path is then auto-derived) or .well-known URL.
func servedDiscovery(t *testing.T, doc map[string]any) (issuerBase, wellKnownURL, customURL string) {
	t.Helper()
	mux := http.NewServeMux()
	var srv *httptest.Server
	handler := func(w http.ResponseWriter, r *http.Request) {
		// Patch issuer to the actual host so the validation chain
		// sees a match without each test having to compute the
		// dynamic URL.
		out := make(map[string]any, len(doc))
		for k, v := range doc {
			out[k] = v
		}
		if _, has := out["issuer"]; !has {
			out["issuer"] = srv.URL
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
	mux.HandleFunc("/.well-known/openid-configuration", handler)
	mux.HandleFunc("/custom-discovery", handler)
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL, srv.URL + "/.well-known/openid-configuration", srv.URL + "/custom-discovery"
}

func TestFetchOIDCDiscovery_FromIssuer(t *testing.T) {
	t.Parallel()
	issuer, _, _ := servedDiscovery(t, map[string]any{
		"authorization_endpoint": "https://idp.example/auth",
		"token_endpoint":         "https://idp.example/token",
		"jwks_uri":               "https://idp.example/jwks",
		"end_session_endpoint":   "https://idp.example/logout",
	})

	doc, err := fetchOIDCDiscovery(context.Background(), issuer, "")
	if err != nil {
		t.Fatalf("fetchOIDCDiscovery: %v", err)
	}
	if doc.AuthEndpoint != "https://idp.example/auth" {
		t.Errorf("authorization_endpoint: got %q", doc.AuthEndpoint)
	}
	if doc.EndSessionEndpoint != "https://idp.example/logout" {
		t.Errorf("end_session_endpoint: got %q", doc.EndSessionEndpoint)
	}
}

func TestFetchOIDCDiscovery_FromCustomURL(t *testing.T) {
	t.Parallel()
	issuer, _, custom := servedDiscovery(t, map[string]any{
		"authorization_endpoint": "https://idp.example/auth",
		"token_endpoint":         "https://idp.example/token",
		"jwks_uri":               "https://idp.example/jwks",
	})

	doc, err := fetchOIDCDiscovery(context.Background(), issuer, custom)
	if err != nil {
		t.Fatalf("fetchOIDCDiscovery: %v", err)
	}
	if doc.TokenEndpoint != "https://idp.example/token" {
		t.Errorf("token_endpoint: got %q", doc.TokenEndpoint)
	}
}

func TestFetchOIDCDiscovery_RejectsIssuerMismatch(t *testing.T) {
	t.Parallel()
	// Force the served doc to declare an issuer that won't match the URL.
	_, _, custom := servedDiscovery(t, map[string]any{
		"issuer":                 "https://attacker.example/",
		"authorization_endpoint": "https://attacker.example/auth",
		"token_endpoint":         "https://attacker.example/token",
		"jwks_uri":               "https://attacker.example/jwks",
	})

	_, err := fetchOIDCDiscovery(context.Background(), "https://idp.example/", custom)
	if err == nil {
		t.Fatal("expected issuer-mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "issuer") {
		t.Errorf("error should mention issuer: %v", err)
	}
}

func TestFetchOIDCDiscovery_NeedsIssuerOrURL(t *testing.T) {
	t.Parallel()
	if _, err := fetchOIDCDiscovery(context.Background(), "", ""); err == nil {
		t.Fatal("expected error when both issuer and discoveryURL are empty")
	}
}

func TestFetchOIDCDiscovery_404IsAnError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)

	_, err := fetchOIDCDiscovery(context.Background(), srv.URL, "")
	if err == nil {
		t.Fatal("expected non-200 to surface as error")
	}
}

// ---------- newOIDCProvider via discovery ----------

func TestNewOIDCProvider_DiscoveryHappyPath(t *testing.T) {
	t.Parallel()
	issuer, _, _ := servedDiscovery(t, map[string]any{
		"authorization_endpoint": "https://idp.example/auth",
		"token_endpoint":         "https://idp.example/token",
		"jwks_uri":               "https://idp.example/jwks",
		"end_session_endpoint":   "https://idp.example/logout",
	})

	prov, err := newOIDCProvider(context.Background(), "oidc:test", "Test", oidcSettings{
		Issuer:       issuer,
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURL:  "https://app.example/cb",
	})
	if err != nil {
		t.Fatalf("newOIDCProvider: %v", err)
	}
	op := prov.(*oidcProvider)
	if op.endSessionURL != "https://idp.example/logout" {
		t.Errorf("endSessionURL not picked up from discovery: %q", op.endSessionURL)
	}
	if op.oauth.Endpoint.AuthURL != "https://idp.example/auth" {
		t.Errorf("auth endpoint not set: %q", op.oauth.Endpoint.AuthURL)
	}
}

func TestNewOIDCProvider_DiscoveryURLOverridesWellKnownPath(t *testing.T) {
	t.Parallel()
	issuer, _, custom := servedDiscovery(t, map[string]any{
		"authorization_endpoint": "https://idp.example/auth",
		"token_endpoint":         "https://idp.example/token",
		"jwks_uri":               "https://idp.example/jwks",
	})

	prov, err := newOIDCProvider(context.Background(), "oidc:test", "Test", oidcSettings{
		Issuer:       issuer,
		DiscoveryURL: custom,
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURL:  "https://app.example/cb",
	})
	if err != nil {
		t.Fatalf("newOIDCProvider: %v", err)
	}
	op := prov.(*oidcProvider)
	if op.oauth.Endpoint.TokenURL != "https://idp.example/token" {
		t.Errorf("token endpoint not set: %q", op.oauth.Endpoint.TokenURL)
	}
}

func TestNewOIDCProvider_EndSessionOverrideWins(t *testing.T) {
	t.Parallel()
	issuer, _, _ := servedDiscovery(t, map[string]any{
		"authorization_endpoint": "https://idp.example/auth",
		"token_endpoint":         "https://idp.example/token",
		"jwks_uri":               "https://idp.example/jwks",
		"end_session_endpoint":   "https://idp.example/discovered-logout",
	})

	prov, err := newOIDCProvider(context.Background(), "oidc:test", "Test", oidcSettings{
		Issuer:             issuer,
		ClientID:           "client",
		ClientSecret:       "secret",
		RedirectURL:        "https://app.example/cb",
		EndSessionEndpoint: "https://idp.example/operator-set-logout",
	})
	if err != nil {
		t.Fatalf("newOIDCProvider: %v", err)
	}
	op := prov.(*oidcProvider)
	if op.endSessionURL != "https://idp.example/operator-set-logout" {
		t.Errorf("end-session override didn't win: got %q", op.endSessionURL)
	}
}

func TestNewOIDCProvider_FullManualSkipsDiscovery(t *testing.T) {
	t.Parallel()
	// Server that returns 500 for any discovery hit; if our code touches
	// it we'll know.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "discovery should not have been called", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	prov, err := newOIDCProvider(context.Background(), "oidc:test", "Test", oidcSettings{
		Issuer:        srv.URL, // discovery would 500
		ClientID:      "client",
		ClientSecret:  "secret",
		RedirectURL:   "https://app.example/cb",
		AuthEndpoint:  "https://idp.example/auth",
		TokenEndpoint: "https://idp.example/token",
		JWKSURI:       "https://idp.example/jwks",
		// EndSessionEndpoint optional, omitted on purpose.
	})
	if err != nil {
		t.Fatalf("newOIDCProvider full-manual: %v", err)
	}
	op := prov.(*oidcProvider)
	if op.oauth.Endpoint.AuthURL != "https://idp.example/auth" {
		t.Errorf("auth endpoint not honoured: %q", op.oauth.Endpoint.AuthURL)
	}
	if op.endSessionURL != "" {
		t.Errorf("end_session should stay empty when neither override nor discovery set it: %q", op.endSessionURL)
	}
}

func TestNewOIDCProvider_RejectsMissingIssuer(t *testing.T) {
	t.Parallel()
	_, err := newOIDCProvider(context.Background(), "oidc:test", "Test", oidcSettings{
		ClientID:      "client",
		ClientSecret:  "secret",
		AuthEndpoint:  "https://idp.example/auth",
		TokenEndpoint: "https://idp.example/token",
		JWKSURI:       "https://idp.example/jwks",
	})
	if err == nil {
		t.Fatal("expected error when issuer is missing, got nil")
	}
}

func TestNewOIDCProvider_PropagatesDiscoveryFailure(t *testing.T) {
	t.Parallel()
	// Discovery doc lies about its issuer — fetchOIDCDiscovery must
	// reject, and newOIDCProvider must surface the error.
	_, _, custom := servedDiscovery(t, map[string]any{
		"issuer":                 "https://attacker.example/",
		"authorization_endpoint": "https://attacker.example/auth",
		"token_endpoint":         "https://attacker.example/token",
		"jwks_uri":               "https://attacker.example/jwks",
	})
	_, err := newOIDCProvider(context.Background(), "oidc:test", "Test", oidcSettings{
		Issuer:       fmt.Sprintf("https://idp.example/%s", "real"),
		DiscoveryURL: custom,
		ClientID:     "client",
		ClientSecret: "secret",
		RedirectURL:  "https://app.example/cb",
	})
	if err == nil {
		t.Fatal("expected newOIDCProvider to refuse mismatched discovery, got nil")
	}
}
