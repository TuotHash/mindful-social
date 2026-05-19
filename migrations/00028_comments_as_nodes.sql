-- +goose Up
-- +goose StatementBegin

-- Comments are promoted to first-class nodes so they appear in the
-- argument graph and reuse the existing node infrastructure (search,
-- visibility, edges). A comment is a node with type='comment' connected
-- to its target by a 'comments_on' edge. The target may be any node
-- (top-level) or another comment (a reply). This is an early-alpha
-- refactor; there is no production comment data to migrate.

ALTER TYPE node_type ADD VALUE IF NOT EXISTS 'comment';
ALTER TYPE edge_kind ADD VALUE IF NOT EXISTS 'comments_on';

DROP INDEX IF EXISTS idx_comments_author_created;
DROP INDEX IF EXISTS idx_comments_node_parent_created;
DROP TABLE IF EXISTS comments;

-- Comments are body-only; title and slug stay NULL. Non-comment node
-- creation still supplies both at the application layer.
ALTER TABLE nodes ALTER COLUMN title DROP NOT NULL;
ALTER TABLE nodes ALTER COLUMN slug  DROP NOT NULL;

-- search_tsv was generated as `title || ' ' || body`, which yields NULL
-- whenever title is NULL. Rebuild with COALESCE so comment bodies are
-- still indexed.
DROP INDEX IF EXISTS nodes_search_tsv_idx;
ALTER TABLE nodes DROP COLUMN search_tsv;
ALTER TABLE nodes ADD COLUMN search_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('english', COALESCE(title, '') || ' ' || COALESCE(body, ''))) STORED;
CREATE INDEX nodes_search_tsv_idx ON nodes USING GIN (search_tsv);

-- Soft-delete support. A removed comment leaves a "comment removed"
-- placeholder in the thread (matching prior behavior). Non-comment nodes
-- are still hard-deleted; the column is simply unset for them.
ALTER TABLE nodes ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE INDEX idx_nodes_deleted_at ON nodes(deleted_at) WHERE deleted_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_nodes_deleted_at;
ALTER TABLE nodes DROP COLUMN IF EXISTS deleted_at;

DROP INDEX IF EXISTS nodes_search_tsv_idx;
ALTER TABLE nodes DROP COLUMN IF EXISTS search_tsv;
ALTER TABLE nodes ADD COLUMN search_tsv tsvector
    GENERATED ALWAYS AS (to_tsvector('english', title || ' ' || body)) STORED;
CREATE INDEX nodes_search_tsv_idx ON nodes USING GIN (search_tsv);

ALTER TABLE nodes ALTER COLUMN slug  SET NOT NULL;
ALTER TABLE nodes ALTER COLUMN title SET NOT NULL;

CREATE TABLE comments (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  node_id    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  parent_id  UUID REFERENCES comments(id) ON DELETE CASCADE,
  author_id  UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  edited_at  TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  CONSTRAINT no_self_comment_parent CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX idx_comments_node_parent_created
  ON comments(node_id, parent_id, created_at);
CREATE INDEX idx_comments_author_created
  ON comments(author_id, created_at DESC);

-- Enum values added in Up cannot be removed without recreating the type;
-- left in place since nothing should reference them after this Down runs.

-- +goose StatementEnd
