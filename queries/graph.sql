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

-- name: ListArgumentGraphNeighborhood :many
-- Returns every visible node within max_hops edges of any seed_id, including
-- the seeds themselves. Used by the graph search path so a single direct
-- match still arrives at the browser with enough surrounding context for the
-- client-side depth slider to walk. Cycles are de-duplicated by the UNION;
-- the outer LIMIT is the safety net against pathologically dense subgraphs.
WITH RECURSIVE reached AS (
    SELECT n.id, 0 AS hop
    FROM nodes n
    WHERE n.id = ANY(sqlc.arg(seed_ids)::uuid[])
      AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
    UNION
    SELECT n2.id, r.hop + 1
    FROM reached r
    JOIN edges e ON r.id IN (e.from_node, e.to_node)
    JOIN nodes n2 ON n2.id IN (e.from_node, e.to_node) AND n2.id <> r.id
    WHERE r.hop < sqlc.arg(max_hops)::int
      AND node_visible_to(n2.*, sqlc.narg(viewer_id)::uuid)
)
SELECT
    n.id::text AS id,
    n.slug,
    n.type::text AS node_type,
    n.title,
    u.username AS author_username
FROM (SELECT DISTINCT id FROM reached) r
JOIN nodes n ON n.id = r.id
JOIN users u ON u.id = n.created_by
ORDER BY n.created_at DESC
LIMIT sqlc.arg(result_limit);

-- name: ListArgumentGraphSeedsByAuthor :many
-- Visible node IDs authored by a given username. Used by the graph viewer's
-- author filter so the neighborhood walk can expand around that author's
-- contributions while still respecting per-node visibility for the viewer.
-- The cap mirrors the search seed budget: the canvas can't render more than
-- a few hundred nodes regardless of how prolific the author is.
SELECT n.id
FROM nodes n
JOIN users u ON u.id = n.created_by
WHERE u.username = sqlc.arg(author_username)::text
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY n.created_at DESC
LIMIT sqlc.arg(result_limit);

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
