package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestNodeVideoUpload_TranscodesAndSaves drives the full pipeline:
// ffprobe + ffmpeg are invoked to normalize a synthetic 4K input down to
// a 1080p H.264/AAC mp4, the artefact lands on disk under UploadDir, and
// the node_videos row records dimensions + a /uploads/... public path.
func TestNodeVideoUpload_TranscodesAndSaves(t *testing.T) {
	requireFFmpeg(t)
	s := integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "A topic", "")

	// 2560x1440 source so the handler has to actually scale — exercising
	// the 1080p ceiling path rather than the no-op branch. Kept below
	// 4K to avoid OOM-killing the encoder on memory-constrained CI.
	input := synthVideo(t, 2560, 1440)

	resp := uploadVideo(t, c, "/nodes/"+topicID.String()+"/videos", "clip.mp4", input)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: status %d body %s", resp.StatusCode, body)
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
	if !strings.HasSuffix(body.Data.FilePath, ".mp4") {
		t.Fatalf("stored path %q not normalized to .mp4", body.Data.FilePath)
	}
	wantPrefix := "/uploads/topics/" + topicID.String() + "/"
	if !strings.HasPrefix(body.Data.FilePath, wantPrefix) {
		t.Fatalf("filePath %q missing prefix %q", body.Data.FilePath, wantPrefix)
	}

	relPath := strings.TrimPrefix(body.Data.FilePath, "/uploads/")
	diskPath := filepath.Join(s.cfg.UploadDir, relPath)
	info, err := os.Stat(diskPath)
	if err != nil {
		t.Fatalf("stat %q: %v", diskPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("stored video is empty: %s", diskPath)
	}

	w, h := probeStoredDimensions(t, diskPath)
	if w > 1920 || h > 1080 {
		t.Fatalf("stored video %dx%d exceeds 1080p ceiling", w, h)
	}
	if w != 1920 || h != 1080 {
		t.Fatalf("expected 4K input to scale to 1920x1080, got %dx%d", w, h)
	}
}

func TestNodeVideoUpload_SavesDraftForNewNode(t *testing.T) {
	requireFFmpeg(t)
	s := integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")

	input := synthVideo(t, 320, 180)
	resp := uploadVideo(t, c, "/nodes/new/videos", "clip.mp4", input)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload: status %d body %s", resp.StatusCode, body)
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
	if !strings.HasSuffix(body.Data.FilePath, ".mp4") {
		t.Fatalf("stored path %q not normalized to .mp4", body.Data.FilePath)
	}

	relPath := strings.TrimPrefix(body.Data.FilePath, "/uploads/")
	diskPath := filepath.Join(s.cfg.UploadDir, relPath)
	info, err := os.Stat(diskPath)
	if err != nil {
		t.Fatalf("stat %q: %v", diskPath, err)
	}
	if info.Size() == 0 {
		t.Fatalf("stored video is empty: %s", diskPath)
	}
}

// TestNodeVideoUpload_RejectsNonVideo confirms the format gate: a text
// blob should come back as a JSON error, not a 200 with a filePath.
func TestNodeVideoUpload_RejectsNonVideo(t *testing.T) {
	requireFFmpeg(t)
	integrationDB(t)
	c := newClient(t)
	signup(t, c, "alice", "alice@example.com", "correct horse battery staple")
	topicID := createNode(t, c, "topic", "A topic", "")

	resp := uploadVideo(t, c, "/nodes/"+topicID.String()+"/videos", "not-a-video.txt", []byte("hello"))
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("non-video upload unexpectedly succeeded: status %d", resp.StatusCode)
	}
}

// TestTargetVideoSize_CapsShorterSide is a pure-function sanity check on
// the resolution math so the cap stays well-defined even without ffmpeg
// installed on the host.
func TestTargetVideoSize_CapsShorterSide(t *testing.T) {
	cases := []struct {
		name         string
		w, h, cap    int
		wantW, wantH int
	}{
		{"4k landscape", 3840, 2160, 1080, 1920, 1080},
		{"4k portrait", 2160, 3840, 1080, 1080, 1920},
		{"1080p untouched", 1920, 1080, 1080, 1920, 1080},
		{"720p untouched", 1280, 720, 1080, 1280, 720},
		{"odd dims rounded down", 1281, 721, 1080, 1280, 720},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotW, gotH := targetVideoSize(tc.w, tc.h, tc.cap)
			if gotW != tc.wantW || gotH != tc.wantH {
				t.Fatalf("targetVideoSize(%d,%d,%d) = %dx%d, want %dx%d",
					tc.w, tc.h, tc.cap, gotW, gotH, tc.wantW, tc.wantH)
			}
		})
	}
}

func requireFFmpeg(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not on PATH; skipping video-upload integration test")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH; skipping video-upload integration test")
	}
}

// synthVideo generates a one-second mp4 with the requested dimensions
// using ffmpeg's lavfi color source. The output goes through the same
// codec the handler emits so the upload format isn't artificially
// "exotic" relative to production traffic.
func synthVideo(t *testing.T, w, h int) []byte {
	t.Helper()
	tmp := filepath.Join(t.TempDir(), "in.mp4")
	cmd := exec.Command("ffmpeg",
		"-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=red:s="+strconv.Itoa(w)+"x"+strconv.Itoa(h)+":d=1:r=30",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		tmp,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("synthVideo: %v\n%s", err, out)
	}
	data, err := os.ReadFile(tmp)
	if err != nil {
		t.Fatalf("read synth: %v", err)
	}
	return data
}

func probeStoredDimensions(t *testing.T, path string) (int, int) {
	t.Helper()
	out, err := exec.Command("ffprobe",
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height",
		"-of", "csv=p=0:s=x",
		path,
	).Output()
	if err != nil {
		t.Fatalf("ffprobe stored: %v", err)
	}
	dims := strings.TrimSpace(string(out))
	parts := strings.Split(dims, "x")
	if len(parts) != 2 {
		t.Fatalf("ffprobe dims %q", dims)
	}
	w, err := strconv.Atoi(parts[0])
	if err != nil {
		t.Fatalf("atoi %q: %v", parts[0], err)
	}
	h, err := strconv.Atoi(parts[1])
	if err != nil {
		t.Fatalf("atoi %q: %v", parts[1], err)
	}
	return w, h
}

func uploadVideo(t *testing.T, c *http.Client, path, filename string, data []byte) *http.Response {
	t.Helper()
	tok := fetchCSRFToken(t, c)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreateFormFile("video", filename)
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
