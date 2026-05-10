-- +goose Up
-- +goose StatementBegin

-- to_position is the highlight rank from the TO-node's perspective, the
-- mirror of `position` (which is the FROM-node's). With both columns,
-- either endpoint of an edge can independently promote it into their
-- "Key reasoning" highlights without affecting the other side. NULL on
-- a column means the edge isn't highlighted from that side.
ALTER TABLE edges ADD COLUMN to_position SMALLINT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE edges DROP COLUMN to_position;

-- +goose StatementEnd
