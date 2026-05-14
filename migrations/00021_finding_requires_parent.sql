-- +goose Up
-- +goose StatementBegin

-- Findings are evidence/citations that always hang off something else
-- (a view or another finding). The application's only finding-creation
-- path lives inside the Connect form, which always supplies a parent,
-- so we codify the invariant at the schema level.
--
-- NOT VALID skips validation of pre-existing rows; any orphan findings
-- in early alpha data are left alone until a follow-up backfill or
-- cleanup migration runs. Forward enforcement is still in effect:
-- INSERTs and UPDATEs are checked normally.
ALTER TABLE nodes
  ADD CONSTRAINT finding_requires_parent
  CHECK (type <> 'finding' OR parent_node_id IS NOT NULL)
  NOT VALID;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE nodes DROP CONSTRAINT IF EXISTS finding_requires_parent;

-- +goose StatementEnd
