-- +goose Up
-- +goose StatementBegin

-- Language used for TTS narration of a node's title+body. NULL until detected
-- or set by the author. Two-letter codes: 'en', 'es', 'fr', 'it', 'de'.
ALTER TABLE nodes ADD COLUMN language TEXT;

CREATE TYPE audio_job_status AS ENUM ('pending', 'running', 'completed', 'failed');

-- Synthesis work queue. One row per (node, chunk_index). The worker picks
-- up 'pending' rows in FIFO order. 'priority' lets the on-demand path jump
-- ahead of the bulk-upload backlog (lower number = sooner).
CREATE TABLE audio_jobs (
  id           UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id      UUID             NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  chunk_index  INT              NOT NULL,
  char_start   INT              NOT NULL,
  char_end     INT              NOT NULL,
  language     TEXT             NOT NULL,
  voice        TEXT             NOT NULL,
  priority     INT              NOT NULL DEFAULT 100,
  status       audio_job_status NOT NULL DEFAULT 'pending',
  attempts     INT              NOT NULL DEFAULT 0,
  last_error   TEXT,
  created_at   TIMESTAMPTZ      NOT NULL DEFAULT now(),
  started_at   TIMESTAMPTZ,
  completed_at TIMESTAMPTZ,
  UNIQUE (node_id, chunk_index)
);

CREATE INDEX audio_jobs_pending_queue
  ON audio_jobs (priority, created_at)
  WHERE status = 'pending';

-- One row per generated audio chunk. chunk_index 0 is always the first
-- segment played. char_start/char_end describe which slice of the read
-- text (title + "\n\n" + body) this chunk covers, used for resumes and
-- caption-track alignment later.
CREATE TABLE audio_chunks (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id      UUID        NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  chunk_index  INT         NOT NULL,
  char_start   INT         NOT NULL,
  char_end     INT         NOT NULL,
  duration_ms  INT         NOT NULL,
  bytes        BIGINT      NOT NULL,
  file_path    TEXT        NOT NULL,
  language     TEXT        NOT NULL,
  voice        TEXT        NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (node_id, chunk_index)
);

CREATE INDEX audio_chunks_node ON audio_chunks (node_id, chunk_index);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE audio_chunks;
DROP TABLE audio_jobs;
DROP TYPE audio_job_status;
ALTER TABLE nodes DROP COLUMN language;

-- +goose StatementEnd
