-- +goose Up
-- +goose StatementBegin

-- Fold 'refines', 'cites' and 'relates_to' into a single 'related' kind.
-- The three non-stance kinds all expressed loose connection in practice;
-- the distinction forced contributors to label something before they had
-- thought it through. Stance kinds ('supports' / 'opposes') still carry
-- their own weight and stay separate.
--
-- Postgres can't drop enum values in place. Swap the type and fold old
-- rows in with a CASE in the USING expression, same pattern as the
-- earlier node_type collapse in migration 00016.
ALTER TYPE edge_kind RENAME TO edge_kind_old;
CREATE TYPE edge_kind AS ENUM ('supports', 'opposes', 'related');

-- Before swapping the column type, collapse any rows whose new kind
-- would now duplicate an existing edge between the same pair. Without
-- this the ALTER TABLE explodes on the (from_node, to_node, kind)
-- unique constraint when both 'refines' and 'relates_to' connect the
-- same two nodes (and so on). Deduplicate by preferring the oldest
-- edge for each (from_node, to_node, new-kind) trio.
DELETE FROM edges e
USING edges other
WHERE e.from_node = other.from_node
  AND e.to_node = other.to_node
  AND (
    CASE WHEN e.kind::text IN ('refines', 'cites', 'relates_to') THEN 'related'
         ELSE e.kind::text END
  ) = (
    CASE WHEN other.kind::text IN ('refines', 'cites', 'relates_to') THEN 'related'
         ELSE other.kind::text END
  )
  AND (e.created_at, e.id) > (other.created_at, other.id);

ALTER TABLE edges
  ALTER COLUMN kind TYPE edge_kind USING (
    CASE
      WHEN kind::text IN ('refines', 'cites', 'relates_to') THEN 'related'::edge_kind
      ELSE kind::text::edge_kind
    END
  );

DROP TYPE edge_kind_old;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Down is lossy: 'related' edges can't be reclassified back into the
-- three original kinds. Map every 'related' row to 'relates_to' on
-- rollback (the loosest of the three), which leaves the previous
-- 'refines' / 'cites' rows merged under one label rather than restored.
ALTER TYPE edge_kind RENAME TO edge_kind_old;
CREATE TYPE edge_kind AS ENUM ('supports', 'opposes', 'refines', 'cites', 'relates_to');

ALTER TABLE edges
  ALTER COLUMN kind TYPE edge_kind USING (
    CASE
      WHEN kind::text = 'related' THEN 'relates_to'::edge_kind
      ELSE kind::text::edge_kind
    END
  );

DROP TYPE edge_kind_old;

-- +goose StatementEnd
