package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// TestNodeRevisions_CreateEditRevert walks the full revision lifecycle:
// create writes rev 1, edit writes rev 2, revert to rev 1 writes rev 3 and
// restores the original content on the live node.
func TestNodeRevisions_CreateEditRevert(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "Original title", "Original body.")

	body := readBody(t, get(t, c, "/nodes/"+id.String()+"/history"))
	if !strings.Contains(body, "Revision 1") {
		t.Fatalf("history missing rev 1 after create; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Original title") {
		t.Fatalf("history missing original title; excerpt: %s", snippet(body))
	}

	resp := formPost(t, c, "/nodes/"+id.String(), url.Values{
		"title": {"Edited title"},
		"body":  {"Edited body."},
	})
	resp.Body.Close()

	body = readBody(t, get(t, c, "/nodes/"+id.String()+"/history"))
	if !strings.Contains(body, "Revision 2") {
		t.Fatalf("history missing rev 2 after edit; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Edited title") {
		t.Fatalf("history missing edited title; excerpt: %s", snippet(body))
	}

	resp = formPost(t, c, "/nodes/"+id.String()+"/history/1/revert", url.Values{})
	if resp.StatusCode != http.StatusOK {
		raw := readBody(t, resp)
		t.Fatalf("revert: status %d body=%s", resp.StatusCode, snippet(raw))
	}
	resp.Body.Close()

	detail := readBody(t, get(t, c, "/nodes/"+id.String()))
	if !strings.Contains(detail, "Original title") {
		t.Fatalf("after revert, detail missing original title; excerpt: %s", snippet(detail))
	}
	if !strings.Contains(detail, "Original body") {
		t.Fatalf("after revert, detail missing original body; excerpt: %s", snippet(detail))
	}

	body = readBody(t, get(t, c, "/nodes/"+id.String()+"/history"))
	if !strings.Contains(body, "Revision 3") {
		t.Fatalf("history missing rev 3 after revert; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Reverted to revision 1") {
		t.Fatalf("history missing revert summary; excerpt: %s", snippet(body))
	}
}

// TestNodeRevisions_NoOpEditSkipsRevision asserts that re-saving the form
// with no content changes does NOT write a duplicate revision.
func TestNodeRevisions_NoOpEditSkipsRevision(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	id := createNode(t, c, "view", "Stable title", "Stable body.")

	resp := formPost(t, c, "/nodes/"+id.String(), url.Values{
		"title": {"Stable title"},
		"body":  {"Stable body."},
	})
	resp.Body.Close()

	body := readBody(t, get(t, c, "/nodes/"+id.String()+"/history"))
	if strings.Contains(body, "Revision 2") {
		t.Fatalf("no-op edit unexpectedly produced rev 2; excerpt: %s", snippet(body))
	}
	if !strings.Contains(body, "Revision 1") {
		t.Fatalf("history missing rev 1; excerpt: %s", snippet(body))
	}
}
