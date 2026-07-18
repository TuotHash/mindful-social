-- +goose Up
-- +goose StatementBegin

-- Evidence the AI proposes alongside the main drafted node: each item becomes a
-- reusable `finding` node (with its own source_url) linked to the main node, if
-- the user ticks it in the confirm modal. Stored as
-- [{"title":..,"body":..,"source_url":..,"relation":"supports|opposes|related"}].
ALTER TABLE node_generation_jobs
  ADD COLUMN result_evidence JSONB NOT NULL DEFAULT '[]'::jsonb;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE node_generation_jobs
  DROP COLUMN result_evidence;

-- +goose StatementEnd
