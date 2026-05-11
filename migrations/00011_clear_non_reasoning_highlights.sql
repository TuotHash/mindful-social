-- +goose Up
-- +goose StatementBegin

-- The highlight UI now restricts the feature to reasoning targets only —
-- the "Key reasoning" section should only ever contain reasonings. Any
-- previously-highlighted topic/view/evidence edges would otherwise become
-- orphaned (still set in the DB but unreachable through the UI), so this
-- migration clears them so the data matches the new constraint.
UPDATE edges e
SET position = NULL
FROM nodes n
WHERE e.to_node = n.id
  AND e.position IS NOT NULL
  AND n.type <> 'reasoning';

UPDATE edges e
SET to_position = NULL
FROM nodes n
WHERE e.from_node = n.id
  AND e.to_position IS NOT NULL
  AND n.type <> 'reasoning';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- No-op: we can't restore the rank values without keeping a history table.
-- Rolling back the highlight-only-reasonings rule means re-highlighting by
-- hand in the UI.
SELECT 1;

-- +goose StatementEnd
