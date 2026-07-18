-- +goose Up
-- +goose StatementBegin

-- Live progress for a drafting job, written by the worker as it streams the
-- model's output. `stage` is a short human status ("Searching the web…",
-- "Writing the draft…"); `progress` is the draft text accumulated so far. The
-- polling "Generating…" modal reads both to show the user what the model is
-- doing and the draft appearing live. Neither affects the final draft — that
-- still comes from the strictly-parsed result_* columns on completion.
ALTER TABLE node_generation_jobs
  ADD COLUMN stage    TEXT NOT NULL DEFAULT '',
  ADD COLUMN progress TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE node_generation_jobs
  DROP COLUMN stage,
  DROP COLUMN progress;

-- +goose StatementEnd
