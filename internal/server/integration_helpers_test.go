package server

import (
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
)

// newClient returns an http.Client with a cookie jar so session cookies
// flow between requests. Redirects are followed by default — assertions
// look at the final URL via resp.Request.URL.
func newClient(t *testing.T) *http.Client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	return &http.Client{Jar: jar}
}

// formPost sends an application/x-www-form-urlencoded POST and returns the
// final response (after redirects). The caller is responsible for closing.
func formPost(t *testing.T, c *http.Client, path string, form url.Values) *http.Response {
	t.Helper()
	resp, err := c.PostForm(testTS.URL+path, form)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	return resp
}

// get sends a GET and returns the final response.
func get(t *testing.T, c *http.Client, path string) *http.Response {
	t.Helper()
	resp, err := c.Get(testTS.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

// readBody slurps and closes a response body, returning the contents.
func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return string(b)
}

// signup posts /signup with the given credentials and returns the client.
// Fails the test if the resulting page indicates a signup error.
func signup(t *testing.T, c *http.Client, username, email, password string) {
	t.Helper()
	resp := formPost(t, c, "/signup", url.Values{
		"username": {username},
		"email":    {email},
		"password": {password},
	})
	body := readBody(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("signup %s: status %d, body=%s", username, resp.StatusCode, body)
	}
	// On success the server redirects to /, which the client followed; the
	// final body contains the home page. On failure the form re-renders
	// with a flash and stays on /signup.
	if strings.Contains(resp.Request.URL.Path, "/signup") {
		t.Fatalf("signup %s: stayed on /signup, body excerpt: %s", username, snippet(body))
	}
}

// signupAndGetUser is signup + a DB lookup so callers can assert on the
// resulting user row (id, created_at, etc.) without a second HTTP round-trip.
func signupAndGetUser(t *testing.T, c *http.Client, username, email, password string) db.User {
	t.Helper()
	signup(t, c, username, email, password)
	u, err := testServer.queries.GetUserByEmail(t.Context(), email)
	if err != nil {
		t.Fatalf("get user by email %q: %v", email, err)
	}
	return u
}

// createNode is a convenience for tests that need a node to operate on.
// Returns the node's id. Always logs in `c` first via signup if needed.
func createNode(t *testing.T, c *http.Client, nodeType, title, body string) uuid.UUID {
	t.Helper()
	resp := formPost(t, c, "/nodes", url.Values{
		"type":  {nodeType},
		"title": {title},
		"body":  {body},
	})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create node: status %d body=%s", resp.StatusCode, string(raw))
	}
	// Handler redirects to /nodes/{slug}; the client followed it so the
	// final URL path is /nodes/{slug}. We look the slug up in the DB to
	// return a UUID — keeps the existing test call sites working with
	// id.String()-style URL construction (resolveNode accepts either form).
	parts := strings.Split(strings.TrimPrefix(resp.Request.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[0] != "nodes" {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("create node: unexpected redirect to %s, body=%s", resp.Request.URL.Path, string(raw))
	}
	node, err := testServer.queries.GetNodeBySlug(t.Context(), parts[1])
	if err != nil {
		t.Fatalf("create node: lookup by slug %q: %v", parts[1], err)
	}
	return node.ID
}

// snippet trims long bodies for failure messages. Helps keep test output
// readable when the full HTML response is several KB.
func snippet(s string) string {
	if len(s) > 240 {
		return s[:240] + "…"
	}
	return s
}
