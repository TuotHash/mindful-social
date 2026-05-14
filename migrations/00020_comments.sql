-- +goose Up
-- +goose StatementBegin

-- Plain-text discussion threads attached to view nodes. The roadmap sketch
-- used bigint ids, but this schema follows the rest of the app's UUID-based
-- nodes/users model.
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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_comments_author_created;
DROP INDEX IF EXISTS idx_comments_node_parent_created;
DROP TABLE IF EXISTS comments;

-- +goose StatementEnd
