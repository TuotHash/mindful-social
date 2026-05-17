package server

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
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

// TestNodeImageUpload_RejectsDecompressionBomb covers the pre-decode
// header check: a PNG whose IHDR declares dimensions beyond our pixel
// budget must be rejected before image.Decode allocates the full pixel
// buffer (which would OOM the process on a 40000x40000 RGBA).
func TestNodeImageUpload_RejectsDecompressionBomb(t *testing.T) {
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "A topic", "")

	bomb := forgedHugePNGHeader(t, 40000, 40000)
	resp := uploadImage(t, c, "/nodes/"+topicID.String()+"/images", "bomb.png", bomb)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("decompression bomb: status %d, want 413", resp.StatusCode)
	}
}

// TestNodeImageUpload_GifPolyglotStripsTrailingBytes covers the GIF
// passthrough hardening: bytes hidden after the GIF trailer must not
// survive storage. image.Decode validates the GIF header and stops at
// the trailer, accepting <valid_GIF><HTML_or_JS> polyglots; the re-encode
// through gif.DecodeAll/EncodeAll discards them.
func TestNodeImageUpload_GifPolyglotStripsTrailingBytes(t *testing.T) {
	s := integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "A topic", "")

	polyglot := tinyGIF(t)
	const sentinel = "<script>alert(\"xss\")</script>"
	polyglot = append(polyglot, []byte(sentinel)...)

	resp := uploadImage(t, c, "/nodes/"+topicID.String()+"/images", "tracker.gif", polyglot)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("gif polyglot upload: status %d", resp.StatusCode)
	}
	path := decodeFilePath(t, resp)
	stored, err := os.ReadFile(filepath.Join(s.cfg.UploadDir, strings.TrimPrefix(path, "/uploads/")))
	if err != nil {
		t.Fatalf("read stored gif: %v", err)
	}
	if bytes.Contains(stored, []byte(sentinel)) {
		t.Fatalf("polyglot trailer survived storage (%d bytes stored)", len(stored))
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

// tinyGIF renders a single-pixel single-frame GIF.
func tinyGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// forgedHugePNGHeader fabricates a PNG that only contains the signature
// plus a valid IHDR claiming the given width and height. image.DecodeConfig
// reads just enough to extract those dimensions; image.Decode would fail
// later because there is no IDAT, but the decompression-bomb gate trips
// first.
func forgedHugePNGHeader(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A})
	var ihdr bytes.Buffer
	ihdr.Write([]byte("IHDR"))
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(w))
	_ = binary.Write(&ihdr, binary.BigEndian, uint32(h))
	ihdr.WriteByte(8) // bit depth
	ihdr.WriteByte(2) // color type RGB
	ihdr.WriteByte(0) // compression
	ihdr.WriteByte(0) // filter
	ihdr.WriteByte(0) // interlace
	_ = binary.Write(&buf, binary.BigEndian, uint32(13))
	buf.Write(ihdr.Bytes())
	_ = binary.Write(&buf, binary.BigEndian, crc32.ChecksumIEEE(ihdr.Bytes()))
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
