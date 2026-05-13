package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestNodeCreate_RendersOnDetail(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "Open source is good", "Pro-FOSS stance.")

	body := readBody(t, get(t, c, "/nodes/"+id.String()))
	if !strings.Contains(body, "Open source is good") {
		t.Fatalf("detail page missing title; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Pro-FOSS stance.") {
		t.Fatalf("detail page missing body; excerpt: %s", snippet(body))
	}
}

func TestNodeDelete_AuthorOnly(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	signup(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, alice, "topic", "A topic", "")

	bob := newClient(t)
	signup(t, bob, "bob", "bob@example.com", "correct horse battery staple")
	resp := formPost(t, bob, "/nodes/"+id.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("non-author delete: expected 403, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Node still exists.
	resp = get(t, alice, "/nodes/"+id.String())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("node still expected to exist, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Author can delete.
	resp = formPost(t, alice, "/nodes/"+id.String()+"/delete", url.Values{})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("author delete: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	// Detail page now 404s.
	resp = get(t, alice, "/nodes/"+id.String())
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("after delete: expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestNodeCreate_RejectsReasoningAndEvidence(t *testing.T) {
	// The Post path is intentionally limited to topics and views — reasoning
	// and evidence are created later as connections off an existing node.
	// Direct POSTs of those types should re-render with a flash, not create
	// a row.
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")

	for _, ty := range []string{"reasoning", "evidence"} {
		resp := formPost(t, c, "/nodes", url.Values{
			"type":  {ty},
			"title": {"Should not create"},
		})
		body := readBody(t, resp)
		if !strings.Contains(body, "View or Topic") {
			t.Fatalf("type=%s: expected flash about View or Topic, got: %s", ty, snippet(body))
		}
		if strings.HasPrefix(resp.Request.URL.Path, "/nodes/") && resp.Request.URL.Path != "/nodes" {
			t.Fatalf("type=%s: unexpected redirect to %s", ty, resp.Request.URL.Path)
		}
	}
}

func TestNodeUpdate_HTMXRequestsGetFullRedirect(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "Original title", "Original body")
	node, err := testServer.queries.GetNode(t.Context(), id)
	if err != nil {
		t.Fatalf("get node: %v", err)
	}

	form := url.Values{
		"title": {"Updated title"},
		"body":  {"Updated body"},
	}
	form.Set("gorilla.csrf.Token", fetchCSRFToken(t, c))
	req, err := http.NewRequest(http.MethodPost, testTS.URL+"/nodes/"+node.Slug, strings.NewReader(form.Encode()))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("HX-Request", "true")

	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("POST update: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("htmx update status = %d, want 200", resp.StatusCode)
	}
	if got, want := resp.Header.Get("HX-Redirect"), "/nodes/"+node.Slug; got != want {
		t.Fatalf("HX-Redirect = %q, want %q", got, want)
	}
}
