package ai

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testWorker(idle time.Duration) *Worker {
	return &Worker{idleTimeout: idle, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestWatchIdleCancelsOnStall(t *testing.T) {
	w := testWorker(100 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var last atomic.Int64
	last.Store(time.Now().UnixNano()) // set once, never poked again → goes idle
	var stalled atomic.Bool
	stop := w.watchIdle(ctx, &last, &stalled, cancel)
	defer stop()

	select {
	case <-ctx.Done():
		if !stalled.Load() {
			t.Fatal("ctx cancelled but stalled flag not set")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not cancel a stalled stream")
	}
}

func TestWatchIdleLetsActiveStreamRun(t *testing.T) {
	w := testWorker(200 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	var stalled atomic.Bool
	stop := w.watchIdle(ctx, &last, &stalled, cancel)

	// Poke steadily for well over one idle window; the watchdog must not fire.
	deadline := time.Now().Add(700 * time.Millisecond)
	for time.Now().Before(deadline) {
		last.Store(time.Now().UnixNano())
		time.Sleep(40 * time.Millisecond)
		if stalled.Load() {
			t.Fatal("watchdog cancelled an actively-streaming generation")
		}
	}
	stop()
	if ctx.Err() != nil {
		t.Fatalf("ctx should be live after an active stream, got %v", ctx.Err())
	}
}

func TestAppendSources(t *testing.T) {
	if got := appendSources("body text", nil); got != "body text" {
		t.Errorf("no sources should leave body unchanged, got %q", got)
	}
	out := appendSources("The claim.", []Source{
		{URL: "https://a.example", Title: "A title"},
		{URL: "https://b.example", Title: ""}, // untitled falls back to URL
	})
	if !strings.Contains(out, "The claim.") {
		t.Errorf("original body dropped: %q", out)
	}
	if !strings.Contains(out, "**Sources**") {
		t.Errorf("missing Sources heading: %q", out)
	}
	if !strings.Contains(out, "[A title](https://a.example)") {
		t.Errorf("missing titled source link: %q", out)
	}
	if !strings.Contains(out, "[https://b.example](https://b.example)") {
		t.Errorf("untitled source should fall back to URL: %q", out)
	}
}

func TestMarshalSources(t *testing.T) {
	b, err := marshalSources([]Source{{URL: "https://a", Title: "A", Text: "ignored body"}})
	if err != nil {
		t.Fatalf("marshalSources: %v", err)
	}
	var refs []sourceRef
	if err := json.Unmarshal(b, &refs); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(refs) != 1 || refs[0].URL != "https://a" || refs[0].Title != "A" {
		t.Errorf("unexpected refs %+v", refs)
	}
	// Text must not be persisted — only url/title.
	if strings.Contains(string(b), "ignored body") {
		t.Errorf("source text should not be persisted: %s", b)
	}
}
