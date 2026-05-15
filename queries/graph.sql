-- name: ListArgumentGraphNodesForViewer :many
-- Recent visible nodes for the graph canvas. The graph viewer is intentionally
-- bounded so a public instance cannot ship an unbounded graph into the page.
SELECT
    n.id::text AS id,
    n.slug,
    n.type::text AS node_type,
    n.title,
    u.username AS author_username
FROM nodes n
JOIN users u ON u.id = n.created_by
WHERE node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY n.created_at DESC
LIMIT sqlc.arg(result_limit);

-- name: ListArgumentGraphEdgesForViewer :many
-- Edges whose endpoints are both present in the same recent visible-node
-- slice. Endpoint visibility gates edge visibility because edges do not carry
-- separate visibility policy.
WITH visible_nodes AS (
    SELECT n.id
    FROM nodes n
    WHERE node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
    ORDER BY n.created_at DESC
    LIMIT sqlc.arg(node_limit)
)
SELECT
    e.id::text AS id,
    e.from_node::text AS from_id,
    e.to_node::text AS to_id,
    e.kind::text AS kind
FROM edges e
JOIN visible_nodes src ON src.id = e.from_node
JOIN visible_nodes dst ON dst.id = e.to_node
ORDER BY e.created_at DESC
LIMIT sqlc.arg(edge_limit);

-- name: ListArgumentGraphEdgesForNodeIDs :many
-- Edges among a known set of visible graph nodes, used by server-side graph
-- search results.
SELECT
    e.id::text AS id,
    e.from_node::text AS from_id,
    e.to_node::text AS to_id,
    e.kind::text AS kind
FROM edges e
JOIN nodes src ON src.id = e.from_node
JOIN nodes dst ON dst.id = e.to_node
WHERE e.from_node = ANY(sqlc.arg(node_ids)::uuid[])
  AND e.to_node = ANY(sqlc.arg(node_ids)::uuid[])
  AND node_visible_to(src.*, sqlc.narg(viewer_id)::uuid)
  AND node_visible_to(dst.*, sqlc.narg(viewer_id)::uuid)
ORDER BY e.created_at DESC
LIMIT sqlc.arg(edge_limit);
