-- +goose Up
-- +goose StatementBegin

-- Per-user defaults applied by composer and timestamp rendering.
--   default_node_visibility    — preselected when creating a new node.
--   default_audience_list_id   — preselected when default visibility is
--                                'list'. Stays nullable so the column can
--                                clear if the list is deleted.
--   timezone                   — IANA name (e.g. 'America/New_York') used
--                                to localise timestamps. Empty string =
--                                fall back to UTC.
ALTER TABLE users
  ADD COLUMN default_node_visibility visibility_kind NOT NULL DEFAULT 'public',
  ADD COLUMN default_audience_list_id UUID REFERENCES audience_lists(id) ON DELETE SET NULL,
  ADD COLUMN timezone TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE users
  DROP COLUMN IF EXISTS timezone,
  DROP COLUMN IF EXISTS default_audience_list_id,
  DROP COLUMN IF EXISTS default_node_visibility;

-- +goose StatementEnd
