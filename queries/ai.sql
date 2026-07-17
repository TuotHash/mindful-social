-- name: EnqueueGenerationJob :one
-- Creates a pending AI node-drafting job. input_urls and use_search capture how
-- the draft should be grounded; the worker fills in the result_* columns.
INSERT INTO node_generation_jobs (user_id, prompt, input_urls, use_search)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ClaimNextGenerationJob :one
-- Atomically grabs the oldest pending job. SKIP LOCKED keeps multiple workers
-- safe. Returns no rows when the queue is empty — treat pgx.ErrNoRows as
-- "nothing to do".
UPDATE node_generation_jobs
SET status     = 'running',
    attempts   = attempts + 1,
    started_at = now()
WHERE id = (
  SELECT id FROM node_generation_jobs
  WHERE status = 'pending'
  ORDER BY created_at
  LIMIT 1
  FOR UPDATE SKIP LOCKED
)
RETURNING *;

-- name: CompleteGenerationJob :exec
-- Stores the drafted node and the sources it was grounded in.
UPDATE node_generation_jobs
SET status         = 'completed',
    completed_at   = now(),
    result_type    = $2,
    result_title   = $3,
    result_body    = $4,
    result_sources = $5,
    last_error     = NULL
WHERE id = $1;

-- name: FailGenerationJob :exec
-- Marks failed and keeps last_error for the UI to surface.
UPDATE node_generation_jobs
SET status     = 'failed',
    last_error = $2
WHERE id = $1;

-- name: GetGenerationJob :one
-- Scoped to the owner so a user can only poll their own jobs.
SELECT * FROM node_generation_jobs
WHERE id = $1 AND user_id = $2;
