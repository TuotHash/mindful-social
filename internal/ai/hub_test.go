package ai

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func recv(t *testing.T, ch <-chan ProgressEvent) ProgressEvent {
	t.Helper()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
		return ProgressEvent{}
	}
}

func TestProgressHubDeliversToSubscriber(t *testing.T) {
	h := NewProgressHub()
	id := uuid.New()

	ch, cancel := h.Subscribe(id)
	defer cancel()

	h.Publish(id, ProgressEvent{Stage: "Writing…", Progress: "hello"})
	got := recv(t, ch)
	if got.Stage != "Writing…" || got.Progress != "hello" {
		t.Errorf("unexpected event %+v", got)
	}

	h.Publish(id, ProgressEvent{Done: true, Status: "completed"})
	done := recv(t, ch)
	if !done.Done || done.Status != "completed" {
		t.Errorf("expected terminal completed, got %+v", done)
	}
}

func TestProgressHubReplaysSnapshotOnLateSubscribe(t *testing.T) {
	h := NewProgressHub()
	id := uuid.New()

	// No subscribers yet — the event is retained as the snapshot.
	h.Publish(id, ProgressEvent{Stage: "Writing…", Progress: "partial draft"})

	ch, cancel := h.Subscribe(id)
	defer cancel()
	got := recv(t, ch)
	if got.Progress != "partial draft" {
		t.Errorf("late subscriber should replay snapshot, got %+v", got)
	}
}

func TestProgressHubCoalescesToLatest(t *testing.T) {
	h := NewProgressHub()
	id := uuid.New()
	ch, cancel := h.Subscribe(id)
	defer cancel()

	// A slow reader: publish several before reading. The buffer holds one, and
	// latest-wins means the reader sees the most recent, not a stale one.
	h.Publish(id, ProgressEvent{Progress: "one"})
	h.Publish(id, ProgressEvent{Progress: "one two"})
	h.Publish(id, ProgressEvent{Progress: "one two three"})

	got := recv(t, ch)
	if got.Progress != "one two three" {
		t.Errorf("expected latest coalesced event, got %q", got.Progress)
	}
}

func TestProgressHubTerminalClearsSnapshot(t *testing.T) {
	h := NewProgressHub()
	id := uuid.New()

	h.Publish(id, ProgressEvent{Progress: "mid"})
	h.Publish(id, ProgressEvent{Done: true, Status: "failed"})

	// A subscriber joining after the terminal event must not replay the stale
	// mid-generation snapshot.
	ch, cancel := h.Subscribe(id)
	defer cancel()
	select {
	case ev := <-ch:
		t.Errorf("expected no replay after terminal, got %+v", ev)
	case <-time.After(50 * time.Millisecond):
		// good — nothing retained
	}
}

func TestProgressHubPublishNilSafe(t *testing.T) {
	var h *ProgressHub
	h.Publish(uuid.New(), ProgressEvent{Progress: "x"}) // must not panic
}
