-- +goose Up
-- +goose StatementBegin

-- An edge with NULL position appears only in the legend on the source node's
-- page. A non-NULL position promotes the edge to the "featured" section,
-- where the destination node's body is rendered inline. Lower numbers sort
-- first. Cap of 4 is enforced in the UI, not the schema.
ALTER TABLE edges ADD COLUMN position SMALLINT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE edges DROP COLUMN position;

-- +goose StatementEnd
