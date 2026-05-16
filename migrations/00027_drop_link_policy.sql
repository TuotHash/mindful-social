-- +goose Up
-- +goose StatementBegin

-- Drop link_policy entirely. The product rule is now "if you can see a node
-- you can connect to it" — visibility (node_visible_to) is the only gate.
-- node_action_allowed() stays in place because edit_policy still uses it.
ALTER TABLE nodes DROP COLUMN IF EXISTS link_policy;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Restore the column with its original default. Existing rows get 'public',
-- which is what the original migration set them to anyway.
ALTER TABLE nodes
  ADD COLUMN link_policy node_action_policy NOT NULL DEFAULT 'public';

-- +goose StatementEnd
