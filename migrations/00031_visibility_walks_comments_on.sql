-- +goose Up
-- +goose StatementBegin

-- node_visible_to() previously walked only the parent_node_id chain.
-- Comments are not linked via parent_node_id — they hang off their target
-- via a 'comments_on' edge (migration 00028). The cascade trigger added
-- in 00030 keeps a comment's visibility column aligned with its parent's,
-- but node_local_visible_to() still evaluates `connections` and "viewer
-- owns the node" against the comment row's own created_by (the commenter,
-- not the parent-node author). A viewer mutual-followers with the
-- commenter but not with the parent author would therefore see comments
-- in the argument graph (and anywhere else node_visible_to is consulted)
-- even when the parent node was hidden to them.
--
-- This migration extends the recursive chain so a comment node also
-- expands to its 'comments_on' target. The intersection (bool_and) then
-- includes the real parent's local check, which uses the parent's
-- created_by and parent's visibility kind. Non-comment nodes are
-- unaffected — the new branch is gated on cur.type = 'comment'.
--
-- The cascade trigger from 00030 is intentionally left in place: it keeps
-- nodes.visibility on comment rows consistent for display purposes (the
-- visibility badge on a comment) and is harmless under the new function
-- (the AND only ever further restricts the result).

DROP FUNCTION IF EXISTS node_visible_to(nodes, UUID);

CREATE FUNCTION node_visible_to(target nodes, viewer_id UUID) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    WITH RECURSIVE chain(id, path) AS (
      SELECT target.id, ARRAY[target.id]
      UNION
      SELECT step.next_id, chain.path || step.next_id
      FROM chain
      JOIN nodes cur ON cur.id = chain.id
      CROSS JOIN LATERAL (
        SELECT cur.parent_node_id AS next_id
        WHERE cur.parent_node_id IS NOT NULL
        UNION ALL
        SELECT e.to_node AS next_id
        FROM edges e
        WHERE cur.type = 'comment'
          AND e.from_node = cur.id
          AND e.kind = 'comments_on'
      ) step
      WHERE NOT step.next_id = ANY(chain.path)
    )
    SELECT COALESCE(bool_and(node_local_visible_to(n.*, viewer_id)), FALSE)
    FROM chain
    JOIN nodes n ON n.id = chain.id;
  $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP FUNCTION IF EXISTS node_visible_to(nodes, UUID);

CREATE FUNCTION node_visible_to(target nodes, viewer_id UUID) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    WITH RECURSIVE chain(id, path) AS (
      SELECT target.id, ARRAY[target.id]
      UNION ALL
      SELECT parent.id, chain.path || parent.id
      FROM chain
      JOIN nodes child ON child.id = chain.id
      JOIN nodes parent ON parent.id = child.parent_node_id
      WHERE child.parent_node_id IS NOT NULL
        AND NOT parent.id = ANY(chain.path)
    )
    SELECT COALESCE(bool_and(node_local_visible_to(n.*, viewer_id)), FALSE)
    FROM chain
    JOIN nodes n ON n.id = chain.id;
  $$;

-- +goose StatementEnd
