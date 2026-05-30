package server

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/audio"
	"github.com/TuotHash/mindful-social/internal/db"
)

// enqueueAudioForNode is the shim called from the create/update handlers.
// It detects language (if not yet set), plans chunks, and inserts the
// initial audio_jobs rows. Failures are logged, not propagated — audio
// is a non-critical sidecar feature; the user's post is the primary
// artifact and must succeed even when TTS is misbehaving.
func (s *Server) enqueueAudioForNode(ctx context.Context, node db.Node) {
	if s.audioWorker == nil {
		// TTS disabled (no sidecar URL configured). Skip silently.
		return
	}
	if err := audio.PlanAndEnqueueForUpload(ctx, s.queries, node); err != nil {
		s.logger.Warn("audio: enqueue for upload",
			"node_id", node.ID, "err", err)
	}
}

// ensureAudioChunkEnqueued enqueues a specific chunk index on-demand,
// used by the chunk endpoint when the listener walks past the
// already-generated head. Returns nil when the chunk is already done.
func (s *Server) ensureAudioChunkEnqueued(ctx context.Context, nodeID uuid.UUID, chunkIndex int) error {
	if s.audioWorker == nil {
		return errors.New("audio disabled")
	}
	_, err := s.queries.GetAudioChunk(ctx, db.GetAudioChunkParams{
		NodeID:     nodeID,
		ChunkIndex: int32(chunkIndex),
	})
	if err == nil {
		return nil // already generated
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	return audio.EnqueueOnDemand(ctx, s.queries, nodeID, chunkIndex)
}
