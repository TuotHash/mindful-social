package server

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/TuotHash/mindful-social/internal/db"
)

// Uploads attached to a public topic are reachable by anonymous viewers
// and by other signed-in users. The handler stamps a private cache control
// (because the URL may still leak via referers/screenshots) and the
// sandbox CSP that keeps polyglot bytes from executing.
func TestUploads_PublicTopicIsViewable(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "A topic", "")

	uploadResp := uploadImage(t, c, "/nodes/"+topicID.String()+"/images", "pic.png", tinyPNG(t))
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("upload: status %d", uploadResp.StatusCode)
	}
	path := decodeFilePath(t, uploadResp)

	// Logged-out fetch succeeds because the topic is public.
	anon := newClient(t)
	resp := get(t, anon, path)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("anonymous fetch of public topic asset: status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("missing nosniff header: %q", got)
	}
	if got := resp.Header.Get("Content-Security-Policy"); !strings.Contains(got, "sandbox") {
		t.Errorf("CSP missing sandbox: %q", got)
	}
	resp.Body.Close()
}

// A private topic's uploads are 404 to everyone except the author. Without
// the visibility gate, the old behavior was to serve the bytes plus a
// public 1-day cache to anyone who learned the URL.
func TestUploads_PrivateTopicHidesFromOthers(t *testing.T) {
	integrationDB(t)
	alice := newClient(t)
	signup(t, alice, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, alice, "topic", "A topic", "")

	uploadResp := uploadImage(t, alice, "/nodes/"+topicID.String()+"/images", "pic.png", tinyPNG(t))
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("upload: status %d", uploadResp.StatusCode)
	}
	path := decodeFilePath(t, uploadResp)

	// Flip the topic to private after the upload so the URL we have was
	// once-public — this reproduces the leak scenario.
	if _, err := testServer.queries.UpdateNode(t.Context(), db.UpdateNodeParams{
		ID:         topicID,
		Title:      "A topic",
		Body:       "",
		SourceUrl:  nil,
		Visibility: db.VisibilityKindPrivate,
		EditPolicy: db.NodeActionPolicyAuthor,
	}); err != nil {
		t.Fatalf("flip to private: %v", err)
	}

	// Anonymous viewer: 404.
	anon := newClient(t)
	if got := get(t, anon, path).StatusCode; got != http.StatusNotFound {
		t.Errorf("anonymous fetch of private topic asset: status %d, want 404", got)
	}

	// Unrelated logged-in viewer: 404.
	mallory := newClient(t)
	signup(t, mallory, "mallory", "mallory@example.com", "correct horse battery staple")
	if got := get(t, mallory, path).StatusCode; got != http.StatusNotFound {
		t.Errorf("unrelated viewer fetch of private topic asset: status %d, want 404", got)
	}

	// Author: still 200, with the private cache control.
	resp := get(t, alice, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("author fetch: status %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Cache-Control"); got != "private, no-store" {
		t.Errorf("private cache control = %q, want private, no-store", got)
	}
}

// Drafts have no node binding at upload time, so we gate on "logged in"
// rather than visibility. Anonymous fetches 404 even though the URL is
// guessable in principle (it isn't — 16 random hex chars).
func TestUploads_DraftRequiresLogin(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")

	uploadResp := uploadImage(t, c, "/nodes/new/images", "pic.png", tinyPNG(t))
	defer uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusOK {
		t.Fatalf("upload: status %d", uploadResp.StatusCode)
	}
	path := decodeFilePath(t, uploadResp)

	anon := newClient(t)
	if got := get(t, anon, path).StatusCode; got != http.StatusNotFound {
		t.Errorf("anonymous fetch of draft: status %d, want 404", got)
	}

	resp := get(t, c, path)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("uploader fetch of own draft: status %d", resp.StatusCode)
	}
}

// Path traversal attempts under any scope must 404 rather than escape the
// upload root.
func TestUploads_RejectsPathTraversal(t *testing.T) {
	s := integrationDB(t)
	// Drop a sibling file outside the configured upload dir to confirm we
	// can't reach it via traversal. The temp parent is shared by the test
	// binary; we write inside a sibling temp dir to keep cleanup tidy.
	parent := filepath.Dir(s.cfg.UploadDir)
	sibling := filepath.Join(parent, "outside-uploads-target")
	if err := os.WriteFile(sibling, []byte("secret"), 0o600); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(sibling) })

	c := newClient(t)
	for _, path := range []string{
		"/uploads/profiles/../outside-uploads-target",
		"/uploads/topics/" + strings.Repeat("0", 36) + "/../../outside-uploads-target",
	} {
		if got := get(t, c, path).StatusCode; got != http.StatusNotFound {
			t.Errorf("traversal %s: status %d, want 404", path, got)
		}
	}
}

func decodeFilePath(t *testing.T, resp *http.Response) string {
	t.Helper()
	body := readBody(t, resp)
	const marker = `"filePath":"`
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatalf("upload response missing filePath: %s", body)
	}
	rest := body[i+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("upload response malformed filePath: %s", body)
	}
	return rest[:end]
}

