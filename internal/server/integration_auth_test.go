package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/TuotHash/mindful-social/internal/auth"
)

func TestSignup_Success(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	u := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")
	if u.Username != "alice" {
		t.Fatalf("expected username alice, got %q", u.Username)
	}
	// After signup the session is logged in; the home page topbar shows the
	// username link.
	body := readBody(t, get(t, c, "/"))
	if !strings.Contains(body, ">alice<") {
		t.Fatalf("home after signup did not show username link; body excerpt: %s", snippet(body))
	}
}

func TestSignup_DuplicateEmail(t *testing.T) {
	integrationDB(t)
	c1 := newClient(t)
	signup(t, c1, "alice", "dup@example.com", "correct horse battery staple")

	// Second signup with the same email lands back on /signup with a flash.
	c2 := newClient(t)
	resp := formPost(t, c2, "/signup", url.Values{
		"username": {"alice2"},
		"email":    {"dup@example.com"},
		"password": {"correct horse battery staple"},
	})
	body := readBody(t, resp)
	if !strings.HasSuffix(resp.Request.URL.Path, "/signup") {
		t.Fatalf("expected to stay on /signup, ended on %s", resp.Request.URL.Path)
	}
	if !strings.Contains(body, "already exists") {
		t.Fatalf("expected duplicate-user flash, got: %s", snippet(body))
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")

	c2 := newClient(t)
	resp := formPost(t, c2, "/login", url.Values{
		"email":    {"alice@example.com"},
		"password": {"wrong password attempt"},
	})
	body := readBody(t, resp)
	if !strings.HasSuffix(resp.Request.URL.Path, "/login") {
		t.Fatalf("expected to stay on /login, ended on %s", resp.Request.URL.Path)
	}
	if !strings.Contains(body, "Invalid email or password") {
		t.Fatalf("expected invalid-login flash, got: %s", snippet(body))
	}
}

func TestLogout_ClearsSession(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")

	resp := formPost(t, c, "/logout", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// After logout the home page shows Sign in / Get started, not the username.
	body := readBody(t, get(t, c, "/"))
	if strings.Contains(body, ">alice<") {
		t.Fatalf("home after logout still shows username; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Sign in") {
		t.Fatalf("home after logout missing Sign in link; excerpt: %s", snippet(body))
	}
}

// Mallory controls an OIDC tenant and signs in claiming alice@example.com.
// The IdP does not assert email_verified, so FindOrCreateOAuthUser must
// refuse to link Mallory's identity to Alice's existing account.
func TestOAuth_UnverifiedEmailDoesNotLinkToExistingUser(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	alice := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")

	user, err := testServer.authSvc.FindOrCreateOAuthUser(t.Context(), "oidc", auth.Identity{
		Subject:       "mallory-subject-1",
		Email:         "alice@example.com",
		EmailVerified: false,
		DisplayName:   "alice",
	})
	if err != nil {
		t.Fatalf("FindOrCreateOAuthUser: %v", err)
	}
	if user.ID == alice.ID {
		t.Fatalf("unverified OIDC email took over alice's account (user_id=%s)", user.ID)
	}
	if user.Username == "alice" {
		t.Fatalf("DisplayName 'alice' was used as username, squatting the handle")
	}
}

// Same flow but with email_verified=true behaves as before: the new OIDC
// identity links to the existing user, no second account is created.
func TestOAuth_VerifiedEmailLinksToExistingUser(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	alice := signupAndGetUser(t, c, "alice", "alice@example.com", "correct horse battery staple")

	user, err := testServer.authSvc.FindOrCreateOAuthUser(t.Context(), "oidc", auth.Identity{
		Subject:       "alice-real-subject",
		Email:         "alice@example.com",
		EmailVerified: true,
		DisplayName:   "Alice",
	})
	if err != nil {
		t.Fatalf("FindOrCreateOAuthUser: %v", err)
	}
	if user.ID != alice.ID {
		t.Fatalf("verified OIDC email did not link to existing alice (got user_id=%s, want %s)", user.ID, alice.ID)
	}
}

func TestRequireUser_RedirectsAnonymous(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	resp := get(t, c, "/nodes/new")
	defer resp.Body.Close()
	// The handler 303s to /login; the client follows and lands on /login.
	if !strings.HasSuffix(resp.Request.URL.Path, "/login") {
		t.Fatalf("expected redirect to /login from /nodes/new, ended on %s", resp.Request.URL.Path)
	}
}
