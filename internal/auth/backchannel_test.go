package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/alexedwards/scs/v2/memstore"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

const (
	testIssuer   = "https://idp.test/"
	testClientID = "mindful-social-test"
)

// testIdP is a self-contained signing IdP wired to a static key set. The
// resulting oidcProvider verifies tokens that idp.sign produced, so the
// happy path and every rejection rule can be driven without a real
// network IdP.
type testIdP struct {
	key      *rsa.PrivateKey
	signer   jose.Signer
	provider *oidcProvider
}

func newTestIdP(t *testing.T) *testIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: key},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("jose.NewSigner: %v", err)
	}
	keySet := &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}
	verifier := oidc.NewVerifier(testIssuer, keySet, &oidc.Config{
		ClientID:             testClientID,
		SupportedSigningAlgs: []string{"RS256"},
	})
	return &testIdP{
		key:    key,
		signer: signer,
		provider: &oidcProvider{
			key:           "oidc:test",
			label:         "Test",
			verifier:      verifier,
			oauth:         &oauth2.Config{},
			usernameClaim: "preferred_username",
		},
	}
}

func (idp *testIdP) sign(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	if err != nil {
		t.Fatalf("marshal claims: %v", err)
	}
	sig, err := idp.signer.Sign(payload)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	out, err := sig.CompactSerialize()
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	return out
}

// baseLogoutClaims is a spec-compliant logout_token payload. Tests mutate
// individual fields to drive each rejection path.
func baseLogoutClaims() map[string]any {
	now := time.Now().Unix()
	return map[string]any{
		"iss": testIssuer,
		"aud": testClientID,
		"iat": now,
		"exp": now + 120,
		"jti": "jti-1",
		"sub": "user-42",
		"sid": "session-7",
		"events": map[string]any{
			"http://schemas.openid.net/event/backchannel-logout": map[string]any{},
		},
	}
}

func TestVerifyLogoutToken_Success(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	raw := idp.sign(t, baseLogoutClaims())

	got, err := idp.provider.VerifyLogoutToken(context.Background(), raw)
	if err != nil {
		t.Fatalf("VerifyLogoutToken: %v", err)
	}
	if got.Subject != "user-42" {
		t.Errorf("subject: got %q, want %q", got.Subject, "user-42")
	}
	if got.SessionID != "session-7" {
		t.Errorf("sid: got %q, want %q", got.SessionID, "session-7")
	}
}

func TestVerifyLogoutToken_SIDOnlyIsAllowed(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	delete(c, "sub")
	raw := idp.sign(t, c)

	got, err := idp.provider.VerifyLogoutToken(context.Background(), raw)
	if err != nil {
		t.Fatalf("VerifyLogoutToken: %v", err)
	}
	if got.Subject != "" {
		t.Errorf("expected empty sub, got %q", got.Subject)
	}
	if got.SessionID != "session-7" {
		t.Errorf("sid: got %q", got.SessionID)
	}
}

func TestVerifyLogoutToken_RejectsNonceClaim(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	c["nonce"] = "n-0S6_WzA2Mj"
	raw := idp.sign(t, c)

	_, err := idp.provider.VerifyLogoutToken(context.Background(), raw)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "nonce") {
		t.Errorf("error message should mention nonce: %v", err)
	}
}

func TestVerifyLogoutToken_RejectsMissingEvents(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	delete(c, "events")
	raw := idp.sign(t, c)

	if _, err := idp.provider.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVerifyLogoutToken_RejectsWrongEvent(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	c["events"] = map[string]any{"http://schemas.openid.net/event/something-else": map[string]any{}}
	raw := idp.sign(t, c)

	if _, err := idp.provider.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVerifyLogoutToken_RejectsMissingSubAndSID(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	delete(c, "sub")
	delete(c, "sid")
	raw := idp.sign(t, c)

	if _, err := idp.provider.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVerifyLogoutToken_RejectsWrongAudience(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	c["aud"] = "someone-else"
	raw := idp.sign(t, c)

	if _, err := idp.provider.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVerifyLogoutToken_RejectsExpired(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	c["exp"] = time.Now().Add(-1 * time.Hour).Unix()
	raw := idp.sign(t, c)

	if _, err := idp.provider.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVerifyLogoutToken_RejectsWrongIssuer(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)
	c := baseLogoutClaims()
	c["iss"] = "https://attacker.example/"
	raw := idp.sign(t, c)

	if _, err := idp.provider.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestVerifyLogoutToken_RejectsBadSignature(t *testing.T) {
	t.Parallel()
	idp := newTestIdP(t)

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: other},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		t.Fatalf("jose.NewSigner: %v", err)
	}
	payload, _ := json.Marshal(baseLogoutClaims())
	sig, _ := signer.Sign(payload)
	raw, _ := sig.CompactSerialize()

	if _, err := idp.provider.VerifyLogoutToken(context.Background(), raw); err == nil {
		t.Fatal("expected error, got nil")
	}
}

// ---------- RevokeOIDCSessions ----------

// makeSession creates a stored session via Load + Put + Commit so tests can
// stand up multiple sessions without faking HTTP roundtrips. Returns the
// token so the test can reload that exact session later.
func makeSession(t *testing.T, sm *scs.SessionManager, userID, provider, subject, sid string) string {
	t.Helper()
	ctx, err := sm.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("sm.Load: %v", err)
	}
	if userID != "" {
		sm.Put(ctx, sessionUserKey, userID)
	}
	RecordOIDCLogin(ctx, sm, provider, subject, sid)
	tok, _, err := sm.Commit(ctx)
	if err != nil {
		t.Fatalf("sm.Commit: %v", err)
	}
	return tok
}

// sessionAlive checks the store still holds a session under the given token.
// Returns false when the token is missing (i.e. session was destroyed).
func sessionAlive(t *testing.T, sm *scs.SessionManager, token string) bool {
	t.Helper()
	ctx, err := sm.Load(context.Background(), token)
	if err != nil {
		t.Fatalf("sm.Load: %v", err)
	}
	// scs.Load creates a fresh session when the token isn't in the store.
	// The recreated session carries no values, so the OIDC subject is empty
	// when the original was destroyed.
	return sm.GetString(ctx, sessionOIDCSubjectKey) != ""
}

func newMemSessionManager() *scs.SessionManager {
	sm := scs.New()
	sm.Store = memstore.New()
	sm.Lifetime = time.Hour
	return sm
}

func TestRevokeOIDCSessions_TargetsProviderAndSubject(t *testing.T) {
	t.Parallel()
	sm := newMemSessionManager()

	aliceA := makeSession(t, sm, "u-alice", "oidc:work", "alice", "sid-a")
	aliceB := makeSession(t, sm, "u-alice", "oidc:work", "alice", "sid-b")
	bob := makeSession(t, sm, "u-bob", "oidc:work", "bob", "sid-x")
	aliceOther := makeSession(t, sm, "u-alice", "oidc:other", "alice", "sid-c")

	// No sid → revoke every session for (oidc:work, alice).
	n, err := RevokeOIDCSessions(context.Background(), sm, "oidc:work", "alice", "")
	if err != nil {
		t.Fatalf("RevokeOIDCSessions: %v", err)
	}
	if n != 2 {
		t.Errorf("revoked count: got %d, want 2", n)
	}
	if sessionAlive(t, sm, aliceA) {
		t.Error("alice session A should be destroyed")
	}
	if sessionAlive(t, sm, aliceB) {
		t.Error("alice session B should be destroyed")
	}
	if !sessionAlive(t, sm, bob) {
		t.Error("bob session should survive")
	}
	if !sessionAlive(t, sm, aliceOther) {
		t.Error("alice's other-provider session should survive")
	}
}

func TestRevokeOIDCSessions_NarrowsToSID(t *testing.T) {
	t.Parallel()
	sm := newMemSessionManager()

	aliceA := makeSession(t, sm, "u-alice", "oidc:work", "alice", "sid-a")
	aliceB := makeSession(t, sm, "u-alice", "oidc:work", "alice", "sid-b")

	// sid set → only the matching session is destroyed.
	n, err := RevokeOIDCSessions(context.Background(), sm, "oidc:work", "alice", "sid-a")
	if err != nil {
		t.Fatalf("RevokeOIDCSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("revoked count: got %d, want 1", n)
	}
	if sessionAlive(t, sm, aliceA) {
		t.Error("alice session A should be destroyed")
	}
	if !sessionAlive(t, sm, aliceB) {
		t.Error("alice session B should survive (different sid)")
	}
}

func TestRevokeOIDCSessions_RequiresProviderAndSubject(t *testing.T) {
	t.Parallel()
	sm := newMemSessionManager()

	if _, err := RevokeOIDCSessions(context.Background(), sm, "", "alice", ""); err == nil {
		t.Error("expected error when provider is empty")
	}
	if _, err := RevokeOIDCSessions(context.Background(), sm, "oidc:work", "", ""); err == nil {
		t.Error("expected error when subject is empty")
	}
}
