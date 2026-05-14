-- +goose Up
-- +goose StatementBegin

-- Videos uploaded from the markdown editor follow the same scoping rule as
-- node_images: they belong to the root topic of the subtree the editor was
-- opened in, so every descendant node can embed the same clip without a
-- per-node re-upload. The handler transcodes incoming files to H.264/AAC
-- mp4 with a 1080p ceiling before insertion, so stored_path always points
-- at a normalized mp4.
CREATE TABLE node_videos (
  id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  root_topic_id  UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  uploaded_by    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  stored_path    TEXT NOT NULL UNIQUE,
  content_type   TEXT NOT NULL,
  byte_size      BIGINT NOT NULL,
  width          INTEGER NOT NULL,
  height         INTEGER NOT NULL,
  duration_ms    INTEGER NOT NULL,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_node_videos_root_topic ON node_videos(root_topic_id);
CREATE INDEX idx_node_videos_uploaded_by ON node_videos(uploaded_by);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS node_videos;

-- +goose StatementEnd
