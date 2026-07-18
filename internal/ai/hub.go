package ai

import (
	"sync"

	"github.com/google/uuid"
)

// ProgressEvent is one live update about a running generation job. Progress is
// the raw accumulated draft text so far (the SSE handler humanizes it before
// sending it to the browser). A Done event is terminal and carries the final
// Status ("completed" | "failed").
type ProgressEvent struct {
	Stage    string
	Progress string
	Done     bool
	Status   string
}

// ProgressHub is an in-process pub/sub that carries live generation updates from
// the worker to the SSE handler that streams them to the browser. It is
// single-instance by design — it matches the single in-process worker and holds
// no cross-process state, so a multi-instance deployment would simply not see
// another instance's events (the SSE client then falls back to the status poll).
type ProgressHub struct {
	mu   sync.Mutex
	subs map[uuid.UUID]map[chan ProgressEvent]struct{}
	// last retains the most recent non-terminal event per job so a subscriber
	// that connects mid-generation immediately sees the current state.
	last map[uuid.UUID]ProgressEvent
}

// NewProgressHub returns an empty hub.
func NewProgressHub() *ProgressHub {
	return &ProgressHub{
		subs: make(map[uuid.UUID]map[chan ProgressEvent]struct{}),
		last: make(map[uuid.UUID]ProgressEvent),
	}
}

// Subscribe registers a listener for a job's updates. The returned channel
// immediately receives the latest known snapshot (if any) and then every
// subsequent event; it is closed when the returned cancel func is called.
// cancel is idempotent.
func (h *ProgressHub) Subscribe(jobID uuid.UUID) (<-chan ProgressEvent, func()) {
	ch := make(chan ProgressEvent, 1)
	h.mu.Lock()
	if h.subs[jobID] == nil {
		h.subs[jobID] = make(map[chan ProgressEvent]struct{})
	}
	h.subs[jobID][ch] = struct{}{}
	if snap, ok := h.last[jobID]; ok {
		ch <- snap // buffer cap 1, freshly created — never blocks
	}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			if set := h.subs[jobID]; set != nil {
				if _, ok := set[ch]; ok {
					delete(set, ch)
					close(ch)
				}
				if len(set) == 0 {
					delete(h.subs, jobID)
				}
			}
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// Publish delivers ev to every current subscriber of jobID. Events carry full
// state (not deltas), so when a subscriber is slow the hub coalesces to the
// latest event rather than blocking or queueing — no information is lost. A
// terminal event also clears the retained snapshot. Safe to call on a nil hub
// (no-op), which keeps the worker simple when AI is disabled or in tests.
func (h *ProgressHub) Publish(jobID uuid.UUID, ev ProgressEvent) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if ev.Done {
		delete(h.last, jobID)
	} else {
		h.last[jobID] = ev
	}
	for ch := range h.subs[jobID] {
		// Latest-wins: if the buffer is full, drop the stale event and replace
		// it with this newer, more complete one. Non-blocking throughout, so a
		// slow reader can never stall the worker.
		select {
		case ch <- ev:
		default:
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- ev:
			default:
			}
		}
	}
}
