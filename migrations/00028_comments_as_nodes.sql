-- +goose Up
-- +goose StatementBegin

-- Comments are promoted to first-class nodes so they appear in the
-- argument graph and reuse the existing node infrastructure (search,
-- visibility, edges). A comment is a node with type='comment' connected
-- to its target by a 'comments_on' edge. The target may be any node
-- (top-level) or another comment (a reply). This is an early-alpha
-- refactor; there is no production comment data to migrate.
--
-- title and slug stay NOT NULL — too many handlers assume non-null
-- strings to make a flip worthwhile. Comments use the empty string for
-- title and a synthetic 'c-<uuid>' slug at the application layer; the
-- slug is never URL-exposed for comments (the redirect path uses the
-- parent node's slug instead).

ALTER TYPE node_type ADD VALUE IF NOT EXISTS 'comment';
ALTER TYPE edge_kind ADD VALUE IF NOT EXISTS 'comments_on';

DROP INDEX IF EXISTS idx_comments_author_created;
DROP INDEX IF EXISTS idx_comments_node_parent_created;
DROP TABLE IF EXISTS comments;

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
