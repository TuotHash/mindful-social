package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/TuotHash/mindful-social/internal/audio"
	"github.com/TuotHash/mindful-social/internal/db"
)

type audioManifestChunk struct {
	Index      int    `json:"index"`
	DurationMs int    `json:"duration_ms"`
	URL        string `json:"url"`
	Ready      bool   `json:"ready"`
}

type audioManifest struct {
	NodeID           string                `json:"node_id"`
	Language         string                `json:"language"`
	Voice            string                `json:"voice"`
	TotalChunks      int                   `json:"total_chunks"`
	EstimatedTotalMs int                   `json:"estimated_total_ms"`
	Chunks           []audioManifestChunk  `json:"chunks"`
}

// handleAudioManifest returns the chunk plan for a node along with which
// chunks are already generated. The player calls this on load and again
// when it needs to know whether the next chunk is ready to play.
func (s *Server) handleAudioManifest(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	if node.Language == nil || !audio.IsSupported(*node.Language) {
		// Node hasn't been classified for TTS (no detected language, or a
		// language we don't ship a voice for). The player treats this as
		// "no audio available".
		s.writeJSON(w, http.StatusOK, audioManifest{
			NodeID:      node.ID.String(),
			TotalChunks: 0,
			Chunks:      []audioManifestChunk{},
		})
		return
	}
	lang := *node.Language
	text := audio.ReadText{Title: node.Title, Body: node.Body}.Joined()
	plan := audio.PlanChunks(text)

	existing, err := s.queries.ListAudioChunksByNode(r.Context(), node.ID)
	if err != nil {
		s.logger.Error("audio manifest: list chunks", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}
	byIndex := make(map[int32]db.AudioChunk, len(existing))
	for _, c := range existing {
		byIndex[c.ChunkIndex] = c
	}

	manifest := audioManifest{
		NodeID:      node.ID.String(),
		Language:    lang,
		Voice:       audio.DefaultVoice(lang),
		TotalChunks: len(plan),
		Chunks:      make([]audioManifestChunk, len(plan)),
	}
	totalMs := 0
	for i, c := range plan {
		entry := audioManifestChunk{
			Index:      i,
			DurationMs: c.EstMs,
			URL:        "/nodes/" + node.ID.String() + "/audio/chunks/" + strconv.Itoa(i),
		}
		if existing, ok := byIndex[int32(i)]; ok {
			entry.Ready = true
			entry.DurationMs = int(existing.DurationMs)
		}
		manifest.Chunks[i] = entry
		totalMs += entry.DurationMs
	}
	manifest.EstimatedTotalMs = totalMs

	s.writeJSON(w, http.StatusOK, manifest)
}

// handleAudioChunk serves a single Opus chunk by index. If the chunk
// hasn't been generated yet, on-demand enqueues it and returns 202 so
// the player can poll the manifest and try again.
func (s *Server) handleAudioChunk(w http.ResponseWriter, r *http.Request) {
	node, ok := s.resolveNode(w, r)
	if !ok {
		return
	}
	idxStr := chiURLParam(r, "n")
	idx, err := strconv.Atoi(idxStr)
	if err != nil || idx < 0 {
		s.notFound(w, r)
		return
	}

	chunk, err := s.queries.GetAudioChunk(r.Context(), db.GetAudioChunkParams{
		NodeID:     node.ID,
		ChunkIndex: int32(idx),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Not yet generated. Kick off on-demand work and tell the
			// client to come back.
			if enqErr := s.ensureAudioChunkEnqueued(r.Context(), node.ID, idx); enqErr != nil {
				s.logger.Warn("audio chunk: enqueue on demand",
					"node_id", node.ID, "chunk", idx, "err", enqErr)
				// Fall through to 404 if even enqueue fails (e.g. unsupported lang).
				s.notFound(w, r)
				return
			}
			w.Header().Set("Retry-After", "3")
			w.WriteHeader(http.StatusAccepted)
			return
		}
		s.logger.Error("audio chunk: lookup", "err", err)
		s.renderError(w, r, http.StatusInternalServerError)
		return
	}

	abs := filepath.Join(s.cfg.AudioDir, filepath.FromSlash(chunk.FilePath))
	w.Header().Set("Content-Type", "audio/ogg")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Audio-Duration-Ms", strconv.Itoa(int(chunk.DurationMs)))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeFile(w, r, abs)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		s.logger.Warn("writeJSON", "err", err)
	}
}
