package audio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/db"
)

// Worker drains the audio_jobs queue in the background. Single goroutine
// for now — Kokoro on CPU benefits little from parallelism on a 6GB-free
// VPS, and ClaimNextAudioJob already uses FOR UPDATE SKIP LOCKED so
// stepping up to N workers is just changing the launch loop.
type Worker struct {
	queries  *db.Queries
	client   *SidecarClient
	audioDir string
	logger   *slog.Logger

	stop   context.CancelFunc
	done   chan struct{}
	closed sync.Once

	// idleSleep is how long the loop pauses when the queue is empty.
	// Short enough that newly-enqueued jobs feel responsive, long enough
	// that an idle server doesn't hammer the database.
	idleSleep time.Duration
}

// NewWorker returns a started worker. Call Close to shut it down.
// audioDir is created if it doesn't exist. If client is nil, the worker
// short-circuits — used when TTS is disabled in config.
func NewWorker(queries *db.Queries, client *SidecarClient, audioDir string, logger *slog.Logger) (*Worker, error) {
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		return nil, fmt.Errorf("create audio dir: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := &Worker{
		queries:   queries,
		client:    client,
		audioDir:  audioDir,
		logger:    logger,
		stop:      cancel,
		done:      make(chan struct{}),
		idleSleep: 2 * time.Second,
	}
	if client == nil {
		// Nothing to do. Close the done channel so Close() returns immediately.
		close(w.done)
		return w, nil
	}
	go w.run(ctx)
	return w, nil
}

// Close stops the worker and waits for the current job (if any) to finish.
func (w *Worker) Close() error {
	w.closed.Do(func() {
		w.stop()
	})
	<-w.done
	return nil
}

func (w *Worker) run(ctx context.Context) {
	defer close(w.done)
	w.logger.Info("audio worker started", "dir", w.audioDir)
	for {
		select {
		case <-ctx.Done():
			w.logger.Info("audio worker stopping")
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

// tick claims one job and processes it. Returns true if a job was found
// (so the caller skips the idle sleep and immediately looks for the next).
func (w *Worker) tick(ctx context.Context) bool {
	job, err := w.queries.ClaimNextAudioJob(ctx)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false
		}
		w.logger.Error("audio worker: claim", "err", err)
		return false
	}
	w.process(ctx, job)
	return true
}

func (w *Worker) process(ctx context.Context, job db.AudioJob) {
	logger := w.logger.With("job_id", job.ID, "node_id", job.NodeID, "chunk", job.ChunkIndex)

	text, err := w.loadChunkText(ctx, job)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	res, err := w.client.Synthesize(ctx, text, job.Language, job.Voice)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	relPath, err := w.writeAudio(job.NodeID, int(job.ChunkIndex), res.OggOpus)
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	_, err = w.queries.CreateAudioChunk(ctx, db.CreateAudioChunkParams{
		NodeID:      job.NodeID,
		ChunkIndex:  job.ChunkIndex,
		CharStart:   job.CharStart,
		CharEnd:     job.CharEnd,
		DurationMs:  int32(res.DurationMs),
		Bytes:       int64(len(res.OggOpus)),
		FilePath:    relPath,
		Language:    job.Language,
		Voice:       res.Voice,
	})
	if err != nil {
		w.fail(ctx, logger, job, err)
		return
	}

	if err := w.queries.CompleteAudioJob(ctx, job.ID); err != nil {
		// The chunk file exists, the chunks row exists, but the job is
		// still "running". The next claim cycle won't pick it up
		// because ClaimNextAudioJob only sees 'pending'. We log and
		// move on — the artifact is correct and serveable.
		logger.Warn("audio worker: complete", "err", err)
		return
	}
	logger.Info("audio chunk synthesized", "dur_ms", res.DurationMs, "bytes", len(res.OggOpus))
}

// loadChunkText reads the source node and slices out the chunk's char range.
func (w *Worker) loadChunkText(ctx context.Context, job db.AudioJob) (string, error) {
	node, err := w.queries.GetNode(ctx, job.NodeID)
	if err != nil {
		return "", fmt.Errorf("get node: %w", err)
	}
	full := ReadText{Title: node.Title, Body: node.Body}.Joined()
	start := int(job.CharStart)
	end := int(job.CharEnd)
	if start < 0 || end > len(full) || start >= end {
		return "", fmt.Errorf("invalid char range [%d,%d) for text of length %d",
			start, end, len(full))
	}
	return full[start:end], nil
}

// writeAudio places the Opus bytes at <audioDir>/<node-id>/<chunk>.opus
// and returns the path relative to audioDir (for the audio_chunks row).
func (w *Worker) writeAudio(nodeID uuid.UUID, chunkIndex int, body []byte) (string, error) {
	rel := filepath.Join(nodeID.String(), fmt.Sprintf("%d.opus", chunkIndex))
	abs := filepath.Join(w.audioDir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	// Atomic write via temp + rename so a crashed worker never leaves
	// truncated audio for the next reader.
	tmp := abs + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, abs); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return rel, nil
}

func (w *Worker) fail(ctx context.Context, logger *slog.Logger, job db.AudioJob, cause error) {
	logger.Error("audio worker: job failed", "err", cause)
	msg := cause.Error()
	if len(msg) > 1024 {
		msg = msg[:1024]
	}
	if err := w.queries.FailAudioJob(ctx, db.FailAudioJobParams{
		ID:        job.ID,
		LastError: &msg,
	}); err != nil {
		logger.Error("audio worker: mark failed", "err", err)
	}
}
