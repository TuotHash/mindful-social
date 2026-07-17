package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

// perJobTimeout bounds one generation end to end (web fetches + LLM). The
// worker runs off the request path so it isn't subject to the 30s HTTP cap,
// but a single hung job shouldn't stall the queue forever.
const perJobTimeout = 3 * time.Minute

// drafter and gatherer are the two collaborators the worker needs, defined as
// interfaces so tests can substitute fakes. *Client and *Gatherer satisfy them.
type drafter interface {
	GenerateNodeGrounded(ctx context.Context, prompt string, sources []Source) (*NodeDraft, error)
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
	logger   *slog.Logger

	stop   context.CancelFunc
	done   chan struct{}
	closed sync.Once

	idleSleep time.Duration
}

// NewWorker returns a started worker. When drafter is nil (AI disabled) it is
// an inert no-op whose Close returns immediately.
func NewWorker(queries *db.Queries, drafter drafter, gatherer gatherer, logger *slog.Logger) *Worker {
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{
		queries:   queries,
		drafter:   drafter,
		gatherer:  gatherer,
		logger:    logger,
		stop:      cancel,
		done:      make(chan struct{}),
		idleSleep: 2 * time.Second,
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
	ctx, cancel := context.WithTimeout(ctx, perJobTimeout)
	defer cancel()

	var urls []string
	if len(job.InputUrls) > 0 {
		if err := json.Unmarshal(job.InputUrls, &urls); err != nil {
			w.fail(ctx, logger, job, fmt.Errorf("bad input_urls: %w", err))
			return
		}
	}

	sources, err := w.gatherer.Gather(ctx, job.Prompt, urls, job.UseSearch)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	draft, err := w.drafter.GenerateNodeGrounded(ctx, job.Prompt, sources)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	body := appendSources(draft.Body, sources)
	sourcesJSON, err := marshalSources(sources)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}
	if err := w.queries.CompleteGenerationJob(ctx, db.CompleteGenerationJobParams{
		ID:            job.ID,
		ResultType:    &draft.Type,
		ResultTitle:   &draft.Title,
		ResultBody:    &body,
		ResultSources: sourcesJSON,
	}); err != nil {
		logger.Error("ai worker: complete", "err", err)
		return
	}
	logger.Info("ai draft generated", "type", draft.Type, "sources", len(sources))
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
}

// sourceRef is the persisted shape of a used source (result_sources JSONB).
type sourceRef struct {
	URL   string `json:"url"`
	Title string `json:"title"`
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
