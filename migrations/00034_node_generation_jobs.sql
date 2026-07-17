-- +goose Up
-- +goose StatementBegin

-- Queue of AI node-drafting requests. One row per "Generate with AI" submit.
-- A background worker picks up 'pending' rows, gathers web sources (user URLs
-- and/or a SearXNG search), calls the LLM, and writes the drafted node back
-- into the result_* columns. The web UI polls the row by id until it reaches
-- 'completed' (or 'failed'), then pre-fills the normal post form. Nothing here
-- writes to nodes — the human confirms the draft through the usual create path.
CREATE TYPE generation_job_status AS ENUM ('pending', 'running', 'completed', 'failed');

CREATE TABLE node_generation_jobs (
  id             UUID                  PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id        UUID                  NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  prompt         TEXT                  NOT NULL,
  input_urls     JSONB                 NOT NULL DEFAULT '[]'::jsonb, -- user-supplied source URLs
  use_search     BOOLEAN               NOT NULL DEFAULT false,       -- also search the web
  status         generation_job_status NOT NULL DEFAULT 'pending',
  attempts       INT                   NOT NULL DEFAULT 0,
  result_type    TEXT,                 -- 'view' | 'topic' | 'finding'
  result_title   TEXT,
  result_body    TEXT,
  result_sources JSONB                 NOT NULL DEFAULT '[]'::jsonb, -- [{"url":...,"title":...}]
  last_error     TEXT,
  created_at     TIMESTAMPTZ           NOT NULL DEFAULT now(),
  started_at     TIMESTAMPTZ,
  completed_at   TIMESTAMPTZ
);

-- Partial index over the FIFO the worker drains, matching the audio_jobs shape.
CREATE INDEX node_generation_jobs_pending
  ON node_generation_jobs (created_at)
  WHERE status = 'pending';

-- Poll/lookups are always scoped to the owning user.
CREATE INDEX node_generation_jobs_user ON node_generation_jobs (user_id, created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE node_generation_jobs;
DROP TYPE generation_job_status;

-- +goose StatementEnd
