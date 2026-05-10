-- name: CreateEdge :one
INSERT INTO edges (from_node, to_node, kind, created_by)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListEdgesFromNodeForViewer :many
-- Outgoing edges with the destination node's title and type joined in,
-- ready to render the "this node points at..." section of the legend.
-- position is NULL for legend-only edges and an integer rank for featured ones.
-- node_visible_to() hides edges whose endpoint the viewer isn't entitled to.
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
SELECT
    e.id,
    e.kind,
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

-- name: ListFeaturedEdgesFromNodeForViewer :many
-- Outgoing edges marked as featured (position IS NOT NULL), with the
-- destination node's full body included so it can be rendered inline on the
-- source node's page. Ordered by position ascending. Filtered through
-- node_visible_to() so a hidden destination simply doesn't appear.
SELECT
    e.id,
    e.kind,
    e.position,
    n.id    AS to_id,
    n.slug  AS to_slug,
    n.type  AS to_type,
    n.title AS to_title,
    n.body  AS to_body
FROM edges e
JOIN nodes n ON n.id = e.to_node
WHERE e.from_node = sqlc.arg(from_node)
  AND e.position IS NOT NULL
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY e.position ASC;

-- name: FeatureEdge :exec
-- Promote an edge to the featured section by assigning it the next position
-- after the current max for its source node. from_node is required so callers
-- can't accidentally feature an edge against the wrong source.
UPDATE edges AS e
SET position = COALESCE(
    (SELECT MAX(sib.position) + 1 FROM edges AS sib WHERE sib.from_node = $2),
    1
)
WHERE e.id = $1 AND e.from_node = $2;

-- name: UnfeatureEdge :exec
UPDATE edges AS e SET position = NULL WHERE e.id = $1 AND e.from_node = $2;

-- name: DeleteEdge :exec
-- Any logged-in user can delete any edge (wiki-open curation, same as
-- feature/unfeature). Both endpoints of an edge can trigger this — the page
-- the user is on is just where they get redirected after the delete.
DELETE FROM edges WHERE id = $1;
