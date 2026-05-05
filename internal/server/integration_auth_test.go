package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
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

	// After logout the home page shows Log in / Sign up, not the username.
	body := readBody(t, get(t, c, "/"))
	if strings.Contains(body, ">alice<") {
		t.Fatalf("home after logout still shows username; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Log in") {
		t.Fatalf("home after logout missing Log in link; excerpt: %s", snippet(body))
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
