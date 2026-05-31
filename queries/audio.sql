-- name: SetNodeLanguage :exec
-- Sets the detected (or user-chosen) language on a node. Two-letter code.
UPDATE nodes SET language = $2 WHERE id = $1;

-- name: EnqueueAudioJob :one
-- Upsert: if a job already exists for (node, chunk_index) leave it alone so
-- we don't reset attempts/status on retry. Returns the row either way so the
-- caller can decide whether it just created work.
INSERT INTO audio_jobs (
    node_id, chunk_index, char_start, char_end, language, voice, priority
)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (node_id, chunk_index) DO UPDATE
  SET priority = LEAST(audio_jobs.priority, EXCLUDED.priority)
RETURNING *;

-- name: ClaimNextAudioJob :one
-- Atomically grabs the highest-priority pending job. SKIP LOCKED lets
-- multiple worker goroutines/processes run safely. Returns no rows when the
-- queue is empty — callers should treat pgx.ErrNoRows as "nothing to do".
UPDATE audio_jobs
SET status     = 'running',
    attempts   = attempts + 1,
    started_at = now()
WHERE id = (
  SELECT id FROM audio_jobs
  WHERE status = 'pending'
  ORDER BY priority, created_at
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteAudioJob :exec
UPDATE audio_jobs
SET status       = 'completed',
    completed_at = now(),
    last_error   = NULL
WHERE id = $1;

-- name: FailAudioJob :exec
-- Marks failed but leaves attempts/last_error intact for diagnostics. If we
-- ever want retries with backoff we can flip status back to 'pending' from
-- a separate scheduler instead of handling it here.
UPDATE audio_jobs
SET status     = 'failed',
    last_error = $2
WHERE id = $1;

-- name: CreateAudioChunk :one
INSERT INTO audio_chunks (
    node_id, chunk_index, char_start, char_end,
    duration_ms, bytes, file_path, language, voice
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (node_id, chunk_index) DO UPDATE
  SET char_start  = EXCLUDED.char_start,
      char_end    = EXCLUDED.char_end,
      duration_ms = EXCLUDED.duration_ms,
      bytes       = EXCLUDED.bytes,
      file_path   = EXCLUDED.file_path,
      language    = EXCLUDED.language,
      voice       = EXCLUDED.voice
RETURNING *;

-- name: GetAudioChunk :one
SELECT * FROM audio_chunks
WHERE node_id = $1 AND chunk_index = $2;

-- name: ListAudioChunksByNode :many
SELECT * FROM audio_chunks
WHERE node_id = $1
ORDER BY chunk_index;

-- name: CountAudioChunksByNode :one
SELECT count(*)::bigint AS chunk_count,
       COALESCE(sum(duration_ms), 0)::bigint AS total_ms
FROM audio_chunks
WHERE node_id = $1;

-- name: GetMaxAudioChunkIndex :one
-- Returns -1 if no chunks exist yet (the next chunk_index is then 0).
SELECT COALESCE(max(chunk_index), -1)::int AS max_index
FROM audio_chunks
WHERE node_id = $1;

-- name: HasPendingAudioJobsForNode :one
SELECT EXISTS (
  SELECT 1 FROM audio_jobs
  WHERE node_id = $1 AND status IN ('pending', 'running')
)::bool AS has_pending;

-- name: ListNodesNeedingAudioBackfill :many
-- Nodes that don't yet have any synthesized audio chunks AND don't already
-- have a queued/in-flight job. Used by the startup backfill to catch posts
-- that were created before TTS existed or while the sidecar was down.
-- Comments are excluded to match the live create/update path.
SELECT n.id, n.type, n.title, n.body, n.source_url, n.created_by,
       n.created_at, n.updated_at, n.search_tsv, n.slug,
       n.visibility, n.edit_policy, n.parent_node_id, n.group_id,
       n.visibility_group_id, n.deleted_at, n.language
FROM nodes n
WHERE n.deleted_at IS NULL
  AND n.type <> 'comment'
  AND NOT EXISTS (
    SELECT 1 FROM audio_chunks c WHERE c.node_id = n.id
  )
  AND NOT EXISTS (
    SELECT 1 FROM audio_jobs j
    WHERE j.node_id = n.id AND j.status IN ('pending', 'running')
  )
ORDER BY n.created_at DESC;
