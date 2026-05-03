-- name: CreateEdge :one
INSERT INTO edges (from_node, to_node, kind, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListEdgesFromNode :many
-- Outgoing edges with the destination node's title and type joined in,
-- ready to render the "this node points at..." section of the legend.
SELECT
    e.id,
    e.kind,
    e.created_at,
    n.id    AS to_id,
    n.type  AS to_type,
    n.title AS to_title
FROM edges e
JOIN nodes n ON n.id = e.to_node
WHERE e.from_node = $1
ORDER BY e.created_at DESC;

-- name: ListEdgesToNode :many
-- Incoming edges, similar shape — for "what points at this node".
SELECT
    e.id,
    e.kind,
    e.created_at,
    n.id    AS from_id,
    n.type  AS from_type,
    n.title AS from_title
FROM edges e
JOIN nodes n ON n.id = e.from_node
WHERE e.to_node = $1
ORDER BY e.created_at DESC;

-- name: DeleteEdge :exec
DELETE FROM edges WHERE id = $1;
