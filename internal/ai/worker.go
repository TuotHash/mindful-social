package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

// drafter and gatherer are the two collaborators the worker needs, defined as
// interfaces so tests can substitute fakes. *Client and *Gatherer satisfy them.
type drafter interface {
	GenerateNodeGroundedStream(ctx context.Context, prompt string, sources []Source, onToken func(delta string)) (*NodeDraft, error)
}

type gatherer interface {
	Gather(ctx context.Context, prompt string, urls []string, useSearch bool) ([]Source, error)
}

// Worker drains node_generation_jobs in the background, mirroring
// audio.Worker. Single goroutine: local LLM generation is serial anyway, and
// ClaimNextGenerationJob uses FOR UPDATE SKIP LOCKED so scaling up is just a
// matter of launching more.
type Worker struct {
	queries  *db.Queries
	drafter  drafter
	gatherer gatherer
	hub      *ProgressHub // live updates to the SSE handler; may be nil
	logger   *slog.Logger

	// jobTimeout is the ABSOLUTE ceiling on one generation end to end (web
	// fetches + LLM) — the outer backstop against a runaway job. The primary
	// generation guard is idleTimeout below.
	jobTimeout time.Duration

	// idleTimeout is how long the streaming model may produce no output before
	// the generation is treated as stalled and cancelled. The clock resets on
	// every token, so a slow-but-progressing model is never killed here.
	idleTimeout time.Duration

	stop   context.CancelFunc
	done   chan struct{}
	closed sync.Once

	idleSleep time.Duration
}

// NewWorker returns a started worker. When drafter is nil (AI disabled) it is
// an inert no-op whose Close returns immediately. jobTimeout is the absolute
// ceiling on a single job; idleTimeout is the no-output-for-this-long stall
// guard on streaming generation. Non-positive values fall back to 30m and 90s.
// hub receives live progress for the SSE handler and may be nil.
func NewWorker(queries *db.Queries, drafter drafter, gatherer gatherer, hub *ProgressHub, jobTimeout, idleTimeout time.Duration, logger *slog.Logger) *Worker {
	if jobTimeout <= 0 {
		jobTimeout = 30 * time.Minute
	}
	if idleTimeout <= 0 {
		idleTimeout = 90 * time.Second
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{
		queries:     queries,
		drafter:     drafter,
		gatherer:    gatherer,
		hub:         hub,
		logger:      logger,
		jobTimeout:  jobTimeout,
		idleTimeout: idleTimeout,
		stop:        cancel,
		done:        make(chan struct{}),
		idleSleep:   2 * time.Second,
	}
	if drafter == nil {
		close(w.done)
		return w
	}
	go w.run(ctx)
	return w
}

// Close stops the worker and waits for the in-flight job (if any) to finish.
func (w *Worker) Close() error {
	w.closed.Do(func() { w.stop() })
	<-w.done
	return nil
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.done)
	w.logger.Info("ai generation worker started")
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("ai generation worker stopping")
			return
		default:
		}
		if didWork := w.tick(ctx); !didWork {
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.idleSleep):
			}
		}
	}
}

// tick claims and processes one job. Returns true when a job was found.
func (w *Worker) tick(ctx context.Context) bool {
	job, err := w.queries.ClaimNextGenerationJob(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		w.logger.Error("ai worker: claim", "err", err)
		return false
	}
	w.process(ctx, job)
	return true
}

func (w *Worker) process(ctx context.Context, job db.NodeGenerationJob) {
	logger := w.logger.With("job_id", job.ID, "user_id", job.UserID)
	// Absolute ceiling on the whole job; the idle watchdog below is the real
	// stall guard on the streaming phase.
	ctx, cancel := context.WithTimeout(ctx, w.jobTimeout)
	defer cancel()

	var urls []string
	if len(job.InputUrls) > 0 {
		if err := json.Unmarshal(job.InputUrls, &urls); err != nil {
			w.fail(ctx, logger, job, fmt.Errorf("bad input_urls: %w", err))
			return
		}
	}

	gatherStage := "Reading your sources…"
	if job.UseSearch {
		gatherStage = "Searching the web…"
	}
	w.setProgress(ctx, job.ID, gatherStage, "")
	w.hub.Publish(job.ID, ProgressEvent{Stage: gatherStage})

	sources, err := w.gatherer.Gather(ctx, job.Prompt, urls, job.UseSearch)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	writeStage := "Writing the draft…"
	if len(sources) > 0 {
		writeStage = fmt.Sprintf("Read %s — writing the draft…", plural(len(sources), "source"))
	}
	w.setProgress(ctx, job.ID, writeStage, "")
	w.hub.Publish(job.ID, ProgressEvent{Stage: writeStage})

	draft, err := w.generate(ctx, job, sources, writeStage)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	// Citations no longer get stapled into the body — they become reusable
	// evidence findings the user picks in the confirm modal.
	body := draft.Body
	sourcesJSON, err := marshalSources(sources)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}
	evidence := groundedEvidence(draft.Evidence, sources)
	evidenceJSON, err := json.Marshal(evidence)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}
	if err := w.queries.CompleteGenerationJob(ctx, db.CompleteGenerationJobParams{
		ID:             job.ID,
		ResultType:     &draft.Type,
		ResultTitle:    &draft.Title,
		ResultBody:     &body,
		ResultSources:  sourcesJSON,
		ResultEvidence: evidenceJSON,
	}); err != nil {
		logger.Error("ai worker: complete", "err", err)
		return
	}
	logger.Info("ai draft generated", "type", draft.Type, "sources", len(sources), "evidence", len(evidence))
	// Terminal event: the SSE client swaps the modal to the finished form.
	w.hub.Publish(job.ID, ProgressEvent{Done: true, Status: string(db.GenerationJobStatusCompleted)})
}

// generate runs the streaming LLM call under an idle watchdog. It streams the
// draft into the job row (throttled) so the polling UI shows it live, resets the
// idle clock on every token, and cancels the call if the model goes quiet for
// longer than idleTimeout — turning a stall into a fast, clear failure while
// letting a slow-but-progressing model run to completion. The idle guard only
// engages after the first token: time-to-first-token (prompt evaluation) on a
// local model with grounded context is legitimately minutes, so before any
// output arrives only the outer hard-cap timeout applies.
func (w *Worker) generate(ctx context.Context, job db.NodeGenerationJob, sources []Source, stage string) (*NodeDraft, error) {
	llmCtx, cancelLLM := context.WithCancel(ctx)
	defer cancelLLM()

	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	var started, stalled atomic.Bool
	stop := w.watchIdle(llmCtx, &last, &started, &stalled, cancelLLM)
	defer stop()

	// accum, lastDB, and lastHub are touched only from onToken, which the stream
	// reader invokes synchronously on this goroutine — no locking needed.
	var accum strings.Builder
	var lastDB, lastHub time.Time
	onToken := func(delta string) {
		started.Store(true)
		last.Store(time.Now().UnixNano())
		accum.WriteString(delta)
		now := time.Now()
		// Push to the SSE hub often (in-memory, cheap) so the browser sees the
		// draft flow ~10x/s; persist to Postgres at most once/second as a
		// durable fallback for a late or reconnecting subscriber.
		if now.Sub(lastHub) >= 100*time.Millisecond {
			lastHub = now
			w.hub.Publish(job.ID, ProgressEvent{Stage: stage, Progress: accum.String()})
		}
		if now.Sub(lastDB) >= time.Second {
			lastDB = now
			w.setProgress(ctx, job.ID, stage, accum.String())
		}
	}

	draft, err := w.drafter.GenerateNodeGroundedStream(llmCtx, job.Prompt, sources, onToken)
	stop()
	if err != nil {
		if stalled.Load() {
			return nil, fmt.Errorf("the model stopped producing output for over %s", w.idleTimeout)
		}
		return nil, err
	}
	return draft, nil
}

// watchIdle starts a goroutine that cancels via cancel() when the timestamp in
// last hasn't advanced within idleTimeout, recording the stall in stalled. It
// returns a stop func (idempotent) that ends the goroutine. Extracted so the
// stall logic is unit-testable without a model or a database.
func (w *Worker) watchIdle(ctx context.Context, last *atomic.Int64, started, stalled *atomic.Bool, cancel context.CancelFunc) func() {
	// Check several times per idle window so a stall is caught promptly, but
	// never busier than every 250ms.
	interval := w.idleTimeout / 4
	if interval < 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				// Don't enforce idle until the model has emitted its first
				// token; the initial warmup is bounded only by the hard cap.
				if !started.Load() {
					continue
				}
				idleFor := time.Since(time.Unix(0, last.Load()))
				if idleFor >= w.idleTimeout {
					stalled.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	return stop
}

// setProgress writes the job's live stage/progress. Best-effort: a failed write
// is logged and swallowed so it can never fail the generation itself.
func (w *Worker) setProgress(ctx context.Context, id uuid.UUID, stage, progress string) {
	if err := w.queries.UpdateGenerationJobProgress(ctx, db.UpdateGenerationJobProgressParams{
		ID:       id,
		Stage:    stage,
		Progress: progress,
	}); err != nil {
		w.logger.Warn("ai worker: progress update", "err", err, "job_id", id)
	}
}

// plural formats a count with its noun, adding an "s" for anything but one.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func (w *Worker) fail(ctx context.Context, logger *slog.Logger, job db.NodeGenerationJob, cause error) {
	logger.Error("ai worker: job failed", "err", cause)
	msg := cause.Error()
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	if err := w.queries.FailGenerationJob(ctx, db.FailGenerationJobParams{ID: job.ID, LastError: &msg}); err != nil {
		logger.Error("ai worker: mark failed", "err", err)
	}
	w.hub.Publish(job.ID, ProgressEvent{Done: true, Status: string(db.GenerationJobStatusFailed)})
}

// sourceRef is the persisted shape of a used source (result_sources JSONB).
type sourceRef struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

// evidenceRef is the persisted shape of a proposed evidence finding
// (result_evidence JSONB), matching what the confirm handler unmarshals.
type evidenceRef struct {
	Title     string `json:"title"`
	Body      string `json:"body"`
	SourceURL string `json:"source_url"`
	Relation  string `json:"relation"`
}

// groundedEvidence keeps only evidence whose source_url was one of the pages
// actually fetched — a hallucinated link can never become a citation node — and
// dedupes by URL so one source yields at most one evidence finding.
func groundedEvidence(items []EvidenceDraft, sources []Source) []evidenceRef {
	out := make([]evidenceRef, 0, len(items))
	if len(items) == 0 || len(sources) == 0 {
		return out
	}
	allowed := make(map[string]bool, len(sources))
	for _, s := range sources {
		allowed[s.URL] = true
	}
	seen := map[string]bool{}
	for _, e := range items {
		if !allowed[e.SourceURL] || seen[e.SourceURL] {
			continue
		}
		seen[e.SourceURL] = true
		out = append(out, evidenceRef{Title: e.Title, Body: e.Body, SourceURL: e.SourceURL, Relation: e.Relation})
	}
	return out
}

func marshalSources(sources []Source) ([]byte, error) {
	refs := make([]sourceRef, 0, len(sources))
	for _, s := range sources {
		refs = append(refs, sourceRef{URL: s.URL, Title: s.Title})
	}
	return json.Marshal(refs)
}

// appendSources adds a markdown "Sources" list to the body so the citation
// travels with the node (which has only one source_url field). No-op when the
// draft was ungrounded.
func appendSources(body string, sources []Source) string {
	if len(sources) == 0 {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	if strings.TrimSpace(body) != "" {
		b.WriteString("\n\n")
	}
	b.WriteString("**Sources**\n")
	for _, s := range sources {
		title := s.Title
		if strings.TrimSpace(title) == "" {
			title = s.URL
		}
		fmt.Fprintf(&b, "\n- [%s](%s)", title, s.URL)
	}
	return b.String()
}
