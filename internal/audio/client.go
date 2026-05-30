package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// SidecarClient talks to the Python Kokoro service. Configure with the
// base URL (e.g. http://127.0.0.1:8090). Zero value is not usable.
type SidecarClient struct {
	baseURL string
	http    *http.Client
}

func NewSidecarClient(baseURL string) *SidecarClient {
	return &SidecarClient{
		baseURL: baseURL,
		http: &http.Client{
			// Synthesis time scales with text length. 2 min covers a
			// ~5-min-of-audio chunk on a typical CPU with comfortable headroom.
			Timeout: 2 * time.Minute,
		},
	}
}

type synthesizeRequest struct {
	Text     string  `json:"text"`
	Language string  `json:"language"`
	Voice    string  `json:"voice"`
	Speed    float64 `json:"speed,omitempty"`
}

// SynthesisResult is what the sidecar returns: raw Opus-in-OGG bytes plus
// metadata pulled from response headers. Voice is whatever the server
// actually picked, which can differ from the requested name for the
// German Martin fine-tune (which always uses its one voice).
type SynthesisResult struct {
	OggOpus    []byte
	DurationMs int
	SampleRate int
	Voice      string
}

// Synthesize POSTs to /synthesize and returns the generated audio.
// Errors include sidecar 5xx responses verbatim so the worker can write
// last_error back to audio_jobs for diagnostics.
func (c *SidecarClient) Synthesize(ctx context.Context, text, language, voice string) (*SynthesisResult, error) {
	body, err := json.Marshal(synthesizeRequest{
		Text:     text,
		Language: language,
		Voice:    voice,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("sidecar %s: %s", resp.Status, bytes.TrimSpace(msg))
	}

	audio, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if len(audio) == 0 {
		return nil, errors.New("sidecar returned empty audio body")
	}
	return &SynthesisResult{
		OggOpus:    audio,
		DurationMs: parseHeaderInt(resp.Header.Get("X-Audio-Duration-Ms")),
		SampleRate: parseHeaderInt(resp.Header.Get("X-Audio-Sample-Rate")),
		Voice:      resp.Header.Get("X-Audio-Voice"),
	}, nil
}

// Healthz returns nil when the sidecar is reachable and responsive.
// Used at startup by the worker so we can warn (and disable) early if
// the sidecar isn't running, instead of failing every job in turn.
func (c *SidecarClient) Healthz(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sidecar /healthz returned %s", resp.Status)
	}
	return nil
}

func parseHeaderInt(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
