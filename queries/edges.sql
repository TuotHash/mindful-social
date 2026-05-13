-- name: CreateEdge :one
INSERT INTO edges (from_node, to_node, kind, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListEdgesFromNodeForViewer :many
-- Outgoing edges with the destination node's title and type joined in,
-- ready to render the "this node points at..." section of the legend.
-- position is NULL for legend-only edges and an integer rank when the
-- FROM-node has highlighted it. node_visible_to() hides edges whose
-- endpoint the viewer isn't entitled to.
SELECT
    e.id,
    e.kind,
    e.position,
    e.created_at,
    n.id    AS to_id,
    n.slug  AS to_slug,
    n.type  AS to_type,
    n.title AS to_title
FROM edges e
JOIN nodes n ON n.id = e.to_node
WHERE e.from_node = sqlc.arg(from_node)
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY e.created_at DESC;

-- name: ListEdgesToNodeForViewer :many
-- Incoming edges, similar shape — for "what points at this node".
-- to_position is the rank when this node (the TO endpoint) has highlighted
-- the edge from its side; NULL keeps it in the legend only.
SELECT
    e.id,
    e.kind,
    e.to_position,
    e.created_at,
    n.id    AS from_id,
    n.slug  AS from_slug,
    n.type  AS from_type,
    n.title AS from_title
FROM edges e
JOIN nodes n ON n.id = e.from_node
WHERE e.to_node = sqlc.arg(to_node)
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY e.created_at DESC;

-- name: ListHighlightedEdgesForNode :many
-- All edges where `node_id` is an endpoint AND that endpoint has marked
-- the edge as highlighted (position when from_node, to_position when
-- to_node). Returns the "other" endpoint regardless of direction so the
-- highlight card can render uniformly. direction tells the UI which
-- active/passive label to apply. Restricted to reasoning targets so the
-- "Key reasoning" section semantically matches its name.
SELECT
    e.id,
    e.kind,
    CASE WHEN e.from_node = sqlc.arg(node_id) THEN 'outgoing' ELSE 'incoming' END::text AS direction,
    CASE WHEN e.from_node = sqlc.arg(node_id) THEN e.position ELSE e.to_position END AS pos,
    other.id    AS other_id,
    other.slug  AS other_slug,
    other.type  AS other_type,
    other.title AS other_title,
    other.body  AS other_body
FROM edges e
JOIN nodes other ON other.id = CASE WHEN e.from_node = sqlc.arg(node_id) THEN e.to_node ELSE e.from_node END
WHERE (e.from_node = sqlc.arg(node_id) OR e.to_node = sqlc.arg(node_id))
  AND ((e.from_node = sqlc.arg(node_id) AND e.position IS NOT NULL)
       OR (e.to_node = sqlc.arg(node_id) AND e.to_position IS NOT NULL))
  AND other.type = 'reasoning'
  AND node_visible_to(other.*, sqlc.narg(viewer_id)::uuid)
ORDER BY pos ASC;

-- name: HighlightEdge :execrows
-- Promote an edge into the highlights section from the perspective of
-- `pov_node`, which must be one of the edge's endpoints. The rank is the
-- next-highest across the existing highlights on that side, so the new
-- card lands at the bottom by default. The "other" endpoint must be a
-- reasoning — the highlights section is for reasonings only. If either
-- check fails the WHERE clause filters the row out and nothing changes.
UPDATE edges AS e
SET
    position = CASE
        WHEN e.from_node = sqlc.arg(pov_node) THEN COALESCE(
            (SELECT MAX(sib.position) + 1 FROM edges sib WHERE sib.from_node = sqlc.arg(pov_node)),
            1
        )
        ELSE e.position
    END,
    to_position = CASE
        WHEN e.to_node = sqlc.arg(pov_node) THEN COALESCE(
            (SELECT MAX(sib.to_position) + 1 FROM edges sib WHERE sib.to_node = sqlc.arg(pov_node)),
            1
        )
        ELSE e.to_position
    END
WHERE e.id = sqlc.arg(edge_id)
  AND sqlc.arg(pov_node) IN (e.from_node, e.to_node)
  AND EXISTS (
      SELECT 1 FROM nodes other
      WHERE other.id = CASE WHEN e.from_node = sqlc.arg(pov_node) THEN e.to_node ELSE e.from_node END
        AND other.type = 'reasoning'
  );

-- name: UnhighlightEdge :execrows
-- Reverse of HighlightEdge: clears the rank on whichever side matches
-- pov_node. No-op if pov_node isn't one of the edge's endpoints.
UPDATE edges AS e
SET
    position    = CASE WHEN e.from_node = sqlc.arg(pov_node) THEN NULL ELSE e.position END,
    to_position = CASE WHEN e.to_node   = sqlc.arg(pov_node) THEN NULL ELSE e.to_position END
WHERE e.id = sqlc.arg(edge_id)
  AND sqlc.arg(pov_node) IN (e.from_node, e.to_node);

-- name: DeleteEdge :execrows
-- Page-node editors can delete only edges touching the page node they are
-- editing. The handler enforces edit permission on that node; this WHERE
-- clause keeps the edge membership check in the database mutation too.
DELETE FROM edges
WHERE id = sqlc.arg(edge_id)
  AND sqlc.arg(page_node_id) IN (from_node, to_node);
