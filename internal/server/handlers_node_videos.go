package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

const (
	// 256 MiB raw upload ceiling — comfortably covers a few minutes of
	// 4K phone video. The transcoder caps the stored mp4 at 1080p so the
	// post-transcode artefact is usually a fraction of this size.
	maxNodeVideoBytes = 256 << 20

	nodeVideoFormField = "video"

	// Longer side ceiling after scaling. 1080p means "1080 lines on the
	// shorter side" — we cap the *shorter* dimension to 1080 so 4K phone
	// portrait (2160x3840) becomes 1080x1920 instead of 608x1080.
	nodeVideoShortSideCap = 1080

	nodeVideoTranscodeTimeout = 5 * time.Minute
	nodeVideoProbeTimeout     = 30 * time.Second
)

// handleNodeVideoUpload accepts a multipart video upload from the markdown
// editor, transcodes it to a normalized H.264/AAC mp4 capped at 1080p, and
// stores the artefact against the root topic of the subtree the editor is
// editing — same scoping rule as handleNodeImageUpload.
//
// The response mirrors the EasyMDE imageUpload contract
// ({data:{filePath}} on success, {error:"<code>"} on failure) so the
// editor-side JS can reuse the same plumbing.
func (s *Server) handleNodeVideoUpload(w http.ResponseWriter, r *http.Request) {
	user := currentUser(r)
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}

	allowed, err := s.canLinkToNode(r.Context(), node, user)
	if err != nil {
		s.logger.Error("node video upload: link policy", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}
	if !allowed {
		writeNodeImageError(w, http.StatusForbidden, "noPermission")
		return
	}

	rootID, err := s.queries.FindRootTopicForNode(r.Context(), node.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeNodeImageError(w, http.StatusBadRequest, "noPermission")
			return
		}
		s.logger.Error("node video upload: find root topic", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	publicPath, info, targetW, targetH, durationMs, ok := s.storeNodeVideoUpload(
		w,
		r,
		filepath.Join(s.cfg.UploadDir, "topics", rootID.String()),
		"/uploads/topics/"+rootID.String(),
	)
	if !ok {
		return
	}

	if _, err := s.queries.CreateNodeVideo(r.Context(), db.CreateNodeVideoParams{
		RootTopicID: rootID,
		UploadedBy:  user.ID,
		StoredPath:  publicPath,
		ContentType: "video/mp4",
		ByteSize:    info.Size(),
		Width:       int32(targetW),
		Height:      int32(targetH),
		DurationMs:  int32(durationMs),
	}); err != nil {
		relPath := strings.TrimPrefix(publicPath, "/uploads/")
		_ = os.Remove(filepath.Join(s.cfg.UploadDir, relPath))
		s.logger.Error("node video upload: insert", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return
	}

	writeNodeImageSuccess(w, publicPath)
}

func (s *Server) handleNewNodeVideoUpload(w http.ResponseWriter, r *http.Request) {
	if currentUser(r) == nil {
		writeNodeImageError(w, http.StatusForbidden, "noPermission")
		return
	}

	publicPath, _, _, _, _, ok := s.storeNodeVideoUpload(
		w,
		r,
		filepath.Join(s.cfg.UploadDir, "drafts"),
		"/uploads/drafts",
	)
	if !ok {
		return
	}

	writeNodeImageSuccess(w, publicPath)
}

func (s *Server) storeNodeVideoUpload(w http.ResponseWriter, r *http.Request, dir, publicPrefix string) (string, os.FileInfo, int, int, int, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxNodeVideoBytes+1024)
	file, _, err := r.FormFile(nodeVideoFormField)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeNodeImageError(w, http.StatusRequestEntityTooLarge, "fileTooLarge")
			return "", nil, 0, 0, 0, false
		}
		writeNodeImageError(w, http.StatusBadRequest, "noFileGiven")
		return "", nil, 0, 0, 0, false
	}
	defer file.Close()

	tmpDir := filepath.Join(s.cfg.UploadDir, "tmp")
	if err := os.MkdirAll(tmpDir, nodeImageDirPerm); err != nil {
		s.logger.Error("node video upload: mkdir tmp", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}

	inFile, err := os.CreateTemp(tmpDir, "in-*.bin")
	if err != nil {
		s.logger.Error("node video upload: create temp", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}
	inPath := inFile.Name()
	defer os.Remove(inPath)
	if _, err := io.Copy(inFile, file); err != nil {
		inFile.Close()
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeNodeImageError(w, http.StatusRequestEntityTooLarge, "fileTooLarge")
			return "", nil, 0, 0, 0, false
		}
		s.logger.Error("node video upload: stash input", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}
	if err := inFile.Close(); err != nil {
		s.logger.Error("node video upload: close input", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}

	probe, err := probeNodeVideo(r.Context(), inPath)
	if err != nil {
		writeNodeImageError(w, http.StatusBadRequest, "typeNotAllowed")
		return "", nil, 0, 0, 0, false
	}

	targetW, targetH := targetVideoSize(probe.Width, probe.Height, nodeVideoShortSideCap)

	if err := os.MkdirAll(dir, nodeImageDirPerm); err != nil {
		s.logger.Error("node video upload: mkdir", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}

	name, err := randomImageName(".mp4")
	if err != nil {
		s.logger.Error("node video upload: name", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}
	storedPath := filepath.Join(dir, name)

	if err := transcodeNodeVideo(r.Context(), inPath, storedPath, targetW, targetH); err != nil {
		_ = os.Remove(storedPath)
		s.logger.Error("node video upload: transcode", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}

	info, err := os.Stat(storedPath)
	if err != nil {
		_ = os.Remove(storedPath)
		s.logger.Error("node video upload: stat output", "err", err)
		writeNodeImageError(w, http.StatusInternalServerError, "importError")
		return "", nil, 0, 0, 0, false
	}

	return publicPrefix + "/" + name, info, targetW, targetH, probe.DurationMs, true
}

// videoProbe is the slice of ffprobe output we actually use.
type videoProbe struct {
	Width      int
	Height     int
	DurationMs int
}

// probeNodeVideo runs ffprobe against path and reports the first video
// stream's dimensions plus the container's reported duration. An error
// here is treated as "the upload isn't a video we can transcode" so the
// caller maps it to a typeNotAllowed response.
func probeNodeVideo(parent context.Context, path string) (videoProbe, error) {
	ctx, cancel := context.WithTimeout(parent, nodeVideoProbeTimeout)
	defer cancel()
	bin, err := exec.LookPath("ffprobe")
	if err != nil {
		return videoProbe{}, fmt.Errorf("ffprobe not found: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-select_streams", "v:0",
		"-show_entries", "stream=width,height,codec_type",
		"-show_entries", "format=duration",
		"-of", "json",
		path,
	)
	out, err := cmd.Output()
	if err != nil {
		return videoProbe{}, fmt.Errorf("ffprobe run: %w", err)
	}

	var parsed struct {
		Streams []struct {
			Width     int    `json:"width"`
			Height    int    `json:"height"`
			CodecType string `json:"codec_type"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return videoProbe{}, fmt.Errorf("ffprobe parse: %w", err)
	}
	if len(parsed.Streams) == 0 || parsed.Streams[0].CodecType != "video" {
		return videoProbe{}, errors.New("no video stream")
	}
	w := parsed.Streams[0].Width
	h := parsed.Streams[0].Height
	if w <= 0 || h <= 0 {
		return videoProbe{}, errors.New("invalid video dimensions")
	}
	durSeconds, _ := strconv.ParseFloat(parsed.Format.Duration, 64)
	if durSeconds < 0 {
		durSeconds = 0
	}
	return videoProbe{
		Width:      w,
		Height:     h,
		DurationMs: int(durSeconds * 1000),
	}, nil
}

// targetVideoSize caps the shorter dimension to cap while preserving the
// input aspect ratio, returning even numbers so libx264 + yuv420p are
// happy (chroma subsampling requires width/height divisible by 2).
func targetVideoSize(w, h, cap int) (int, int) {
	if w <= 0 || h <= 0 {
		return w, h
	}
	shorter := w
	if h < shorter {
		shorter = h
	}
	if shorter <= cap {
		return roundDownEven(w), roundDownEven(h)
	}
	factor := float64(cap) / float64(shorter)
	return roundDownEven(int(float64(w)*factor + 0.5)), roundDownEven(int(float64(h)*factor + 0.5))
}

// roundDownEven nudges odd values down by one. libx264 with yuv420p (the
// most widely playable pixel format) requires even dimensions. Rounding
// down rather than up keeps the scaled output strictly inside the cap.
func roundDownEven(n int) int {
	if n < 2 {
		return 2
	}
	if n%2 == 1 {
		return n - 1
	}
	return n
}

// transcodeNodeVideo invokes ffmpeg to produce a normalized H.264/AAC mp4
// at the requested dimensions. faststart relocates the moov atom so the
// browser can begin playback before the whole file finishes downloading.
// crf 28 trades some quality for a meaningful size reduction; phone clips
// shrink to ~10–30% of the input.
func transcodeNodeVideo(parent context.Context, in, out string, w, h int) error {
	ctx, cancel := context.WithTimeout(parent, nodeVideoTranscodeTimeout)
	defer cancel()
	bin, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	args := []string{
		"-y",
		"-hide_banner",
		"-nostats",
		"-loglevel", "error",
		"-i", in,
		"-vf", fmt.Sprintf("scale=%d:%d", w, h),
		"-map", "0:v:0",
		"-map", "0:a:0?",
		"-c:v", "libx264",
		"-preset", "medium",
		"-crf", "28",
		"-pix_fmt", "yuv420p",
		"-c:a", "aac",
		"-b:a", "128k",
		"-ac", "2",
		"-movflags", "+faststart",
		"-threads", "0",
		out,
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %w: %s", err, stderr.String())
	}
	return nil
}
