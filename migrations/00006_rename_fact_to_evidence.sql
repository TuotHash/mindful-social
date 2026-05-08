-- +goose Up
-- +goose StatementBegin

-- Rename the 'fact' enum value to 'evidence'. The term "evidence" reads more
-- naturally for what these nodes are (an external source cited by reasoning)
-- and avoids the overconfident framing of "fact". Existing rows keep their
-- identity; only the label changes.
ALTER TYPE node_type RENAME VALUE 'fact' TO 'evidence';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TYPE node_type RENAME VALUE 'evidence' TO 'fact';

-- +goose StatementEnd
