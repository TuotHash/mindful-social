-- +goose Up
-- +goose StatementBegin

-- Merge 'reasoning' and 'evidence' node types into a single 'finding'.
-- The distinction never carried its weight in the UI — both are units of
-- content that support or contest a view, whether sourced or inferred —
-- and the split forced contributors to pick a label before they had
-- written the thing. After the merge, source_url stays optional on every
-- finding (set it when the content points at an external source; leave it
-- blank when the finding stands on its own argument).

-- Postgres can't drop enum values in place. Swap the type and fold old
-- rows in with a CASE in the USING expression.
ALTER TYPE node_type RENAME TO node_type_old;
CREATE TYPE node_type AS ENUM ('topic', 'view', 'finding');

ALTER TABLE nodes
  ALTER COLUMN type TYPE node_type USING (
    CASE
      WHEN type::text IN ('reasoning', 'evidence') THEN 'finding'::node_type
      ELSE type::text::node_type
    END
  );

DROP TYPE node_type_old;

-- Rename the pin/finding junction table so the schema vocabulary stays
-- consistent with the merged node type.
ALTER TABLE pin_reasonings RENAME TO pin_findings;
ALTER TABLE pin_findings RENAME COLUMN reasoning_id TO finding_id;
ALTER INDEX pin_reasonings_pin_idx RENAME TO pin_findings_pin_idx;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Down is lossy: we can't recover whether a finding was originally a
-- reasoning or an evidence. Map everything back to 'reasoning' as the
-- safer default; operators wanting evidence rows back should re-tag by
-- presence of source_url.
ALTER INDEX pin_findings_pin_idx RENAME TO pin_reasonings_pin_idx;
ALTER TABLE pin_findings RENAME COLUMN finding_id TO reasoning_id;
ALTER TABLE pin_findings RENAME TO pin_reasonings;

ALTER TYPE node_type RENAME TO node_type_old;
CREATE TYPE node_type AS ENUM ('topic', 'view', 'reasoning', 'evidence');

ALTER TABLE nodes
  ALTER COLUMN type TYPE node_type USING (
    CASE
      WHEN type::text = 'finding' THEN 'reasoning'::node_type
      ELSE type::text::node_type
    END
  );

DROP TYPE node_type_old;

-- +goose StatementEnd
