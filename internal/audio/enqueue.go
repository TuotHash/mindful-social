package audio

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/TuotHash/mindful-social/internal/db"
)

// Priority bands for the audio_jobs queue. Lower = sooner.
const (
	priorityOnDemand = 10  // user is waiting for this chunk right now
	priorityUpload   = 100 // routine post-create / post-update work
)

// PlanAndEnqueueForUpload is the upload-time entrypoint. It:
//
//  1. Reads the node's current title+body, detects the language if the
//     node doesn't have one yet, and persists the detected code.
//  2. Plans chunks against the node's read text.
//  3. Enqueues either every chunk (short posts) or just the first
//     chunk (long posts — the rest get enqueued on-demand by the
//     chunk endpoint when the listener reaches them).
//
// Returns silently when the node language isn't one we support — the
// caller's create/update path keeps working without audio.
func PlanAndEnqueueForUpload(ctx context.Context, queries *db.Queries, node db.Node) error {
	lang, err := resolveLanguage(ctx, queries, node)
	if err != nil {
		return err
	}
	if !IsSupported(lang) {
		return nil // not an error — just no audio for this post
	}
	voice := DefaultVoice(lang)

	text := ReadText{Title: node.Title, Body: node.Body}.Joined()
	chunks := PlanChunks(text)
	if len(chunks) == 0 {
		return nil
	}

	enqueueCount := len(chunks)
	if !IsShortPost(text) {
		enqueueCount = 1 // long post: only the head, rest on-demand
	}

	for i := 0; i < enqueueCount; i++ {
		c := chunks[i]
		if _, err := queries.EnqueueAudioJob(ctx, db.EnqueueAudioJobParams{
			NodeID:     node.ID,
			ChunkIndex: int32(c.Index),
			CharStart:  int32(c.CharStart),
			CharEnd:    int32(c.CharEnd),
			Language:   lang,
			Voice:      voice,
			Priority:   priorityUpload,
		}); err != nil {
			return fmt.Errorf("enqueue chunk %d: %w", c.Index, err)
		}
	}
	return nil
}

// EnqueueOnDemand schedules generation of a specific chunk with high
// priority — used by the audio chunk endpoint when the listener
// approaches the end of what's already generated. Idempotent: if the
// chunk is already pending or completed, this is a no-op.
func EnqueueOnDemand(ctx context.Context, queries *db.Queries, nodeID uuid.UUID, chunkIndex int) error {
	node, err := queries.GetNode(ctx, nodeID)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}
	if node.Language == nil || !IsSupported(*node.Language) {
		return errors.New("node has no supported language for audio")
	}
	lang := *node.Language
	text := ReadText{Title: node.Title, Body: node.Body}.Joined()
	chunks := PlanChunks(text)
	if chunkIndex < 0 || chunkIndex >= len(chunks) {
		return fmt.Errorf("chunk %d out of range (have %d)", chunkIndex, len(chunks))
	}
	c := chunks[chunkIndex]
	_, err = queries.EnqueueAudioJob(ctx, db.EnqueueAudioJobParams{
		NodeID:     node.ID,
		ChunkIndex: int32(c.Index),
		CharStart:  int32(c.CharStart),
		CharEnd:    int32(c.CharEnd),
		Language:   lang,
		Voice:      DefaultVoice(lang),
		Priority:   priorityOnDemand,
	})
	return err
}

// BackfillExistingNodes enqueues TTS jobs for every node that has no
// audio chunks and no in-flight job — i.e. posts created before TTS
// existed, or while the sidecar was unreachable. Safe to call on every
// startup: PlanAndEnqueueForUpload is idempotent (EnqueueAudioJob does
// ON CONFLICT DO UPDATE), so a node already mid-backfill is just a
// no-op. Per-node failures are logged and skipped; the loop keeps
// going so one bad row can't stall the rest of the catch-up.
func BackfillExistingNodes(ctx context.Context, queries *db.Queries, logger *slog.Logger) {
	nodes, err := queries.ListNodesNeedingAudioBackfill(ctx)
	if err != nil {
		logger.Warn("audio backfill: list nodes", "err", err)
		return
	}
	if len(nodes) == 0 {
		return
	}
	logger.Info("audio backfill: starting", "count", len(nodes))
	enqueued := 0
	for _, node := range nodes {
		if ctx.Err() != nil {
			return
		}
		if err := PlanAndEnqueueForUpload(ctx, queries, node); err != nil {
			logger.Warn("audio backfill: enqueue", "node_id", node.ID, "err", err)
			continue
		}
		enqueued++
	}
	logger.Info("audio backfill: done", "enqueued", enqueued, "skipped", len(nodes)-enqueued)
}

// resolveLanguage returns the node's stored language, detecting and
// persisting one if the column is null.
func resolveLanguage(ctx context.Context, queries *db.Queries, node db.Node) (string, error) {
	if node.Language != nil && *node.Language != "" {
		return *node.Language, nil
	}
	detected := DetectLanguage(ReadText{Title: node.Title, Body: node.Body}.Joined())
	if detected == "" {
		return "", nil
	}
	if err := queries.SetNodeLanguage(ctx, db.SetNodeLanguageParams{
		ID:       node.ID,
		Language: &detected,
	}); err != nil {
		return "", fmt.Errorf("save detected language: %w", err)
	}
	return detected, nil
}
