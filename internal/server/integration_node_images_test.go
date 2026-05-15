package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNodeImageUpload_SavesAgainstRootTopic exercises the happy path: a
// signed-in user POSTs a PNG to /nodes/{topic-id}/images, the handler
// stores it on disk and in node_images, and the response carries a
// /uploads/... filePath EasyMDE can splice into the markdown.
func TestNodeImageUpload_SavesAgainstRootTopic(t *testing.T) {
	s := integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "A topic", "")

	resp := uploadImage(t, c, "/nodes/"+topicID.String()+"/images", "pic.png", tinyPNG(t))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload: status %d", resp.StatusCode)
	}

	var body struct {
		Data  *struct{ FilePath string } `json:"data"`
		Error string                     `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "" {
		t.Fatalf("upload error: %s", body.Error)
	}
	if body.Data == nil || body.Data.FilePath == "" {
		t.Fatalf("upload response missing filePath: %+v", body)
	}
	wantPrefix := "/uploads/topics/" + topicID.String() + "/"
	if !strings.HasPrefix(body.Data.FilePath, wantPrefix) {
		t.Fatalf("filePath %q missing prefix %q", body.Data.FilePath, wantPrefix)
	}

	// The image file must actually exist on disk under the configured
	// UploadDir so the /uploads/* route can serve it.
	relPath := strings.TrimPrefix(body.Data.FilePath, "/uploads/")
	diskPath := filepath.Join(s.cfg.UploadDir, relPath)
	info, err := os.Stat(diskPath)
	if err != nil {
		t.Fatalf("stat %q: %v", diskPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("stored image is empty: %s", diskPath)
	}
}

func TestNodeImageUpload_SavesDraftForNewNode(t *testing.T) {
	s := integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")

	resp := uploadImage(t, c, "/nodes/new/images", "pic.png", tinyPNG(t))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload: status %d", resp.StatusCode)
	}

	var body struct {
		Data  *struct{ FilePath string } `json:"data"`
		Error string                     `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Error != "" {
		t.Fatalf("upload error: %s", body.Error)
	}
	if body.Data == nil || body.Data.FilePath == "" {
		t.Fatalf("upload response missing filePath: %+v", body)
	}
	if !strings.HasPrefix(body.Data.FilePath, "/uploads/drafts/") {
		t.Fatalf("filePath %q missing draft prefix", body.Data.FilePath)
	}

	relPath := strings.TrimPrefix(body.Data.FilePath, "/uploads/")
	diskPath := filepath.Join(s.cfg.UploadDir, relPath)
	info, err := os.Stat(diskPath)
	if err != nil {
		t.Fatalf("stat %q: %v", diskPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("stored image is empty: %s", diskPath)
	}
}

// TestNodeImageUpload_RejectsAnonymous confirms the route lives behind
// requireUser; an unauthenticated client gets redirected to /login rather
// than the JSON success path.
func TestNodeImageUpload_RejectsAnonymous(t *testing.T) {
	integrationDB(t)
	host := newClient(t)
	signup(t, host, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, host, "topic", "A topic", "")

	anon := newClient(t)
	resp := uploadImage(t, anon, "/nodes/"+topicID.String()+"/images", "pic.png", tinyPNG(t))
	defer resp.Body.Close()
	// The middleware redirects unauthenticated requests to /login (which
	// returns 200 once followed). The cookie jar stays empty, so a
	// subsequent /nodes call would still be anonymous — that's enough to
	// prove the handler itself was bypassed.
	if resp.Request.URL.Path == "/nodes/"+topicID.String()+"/images" {
		t.Fatalf("anonymous upload reached the handler: status %d", resp.StatusCode)
	}
}

// TestNodeImageUpload_RejectsNonImage covers the format gate: a text/plain
// blob should come back as a JSON error, not a 200 with a filePath.
func TestNodeImageUpload_RejectsNonImage(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "A topic", "")

	resp := uploadImage(t, c, "/nodes/"+topicID.String()+"/images", "not-an-image.txt", []byte("hello"))
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("non-image upload unexpectedly succeeded: status %d", resp.StatusCode)
	}
}

// tinyPNG renders a single-pixel PNG so tests can submit a real image
// without bundling a fixture file.
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 128, G: 64, B: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// uploadImage POSTs a multipart form with `image` set to the given bytes
// and the CSRF token forwarded via the X-CSRF-Token header (the channel
// EasyMDE uses in production).
func uploadImage(t *testing.T, c *http.Client, path, filename string, data []byte) *http.Response {
	t.Helper()
	tok := fetchCSRFToken(t, c)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("image", filename)
	if err != nil {
		t.Fatalf("multipart: %v", err)
	}
	if _, err := io.Copy(part, bytes.NewReader(data)); err != nil {
		t.Fatalf("multipart copy: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("multipart close: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, testTS.URL+path, &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-CSRF-Token", tok)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	return resp
}
