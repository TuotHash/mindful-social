-- +goose Up
-- +goose StatementBegin

-- Images uploaded from a markdown editor live against the root topic of the
-- subtree the editor was opened in, so every descendant node can embed the
-- same picture without a per-node re-upload. The handler resolves the root
-- topic before inserting; this table stays oblivious to the recursion.
CREATE TABLE node_images (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  root_topic_id  UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  uploaded_by    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  stored_path    TEXT NOT NULL UNIQUE,
  content_type   TEXT NOT NULL,
  byte_size      BIGINT NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_node_images_root_topic ON node_images(root_topic_id);
CREATE INDEX idx_node_images_uploaded_by ON node_images(uploaded_by);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS node_images;

-- +goose StatementEnd
