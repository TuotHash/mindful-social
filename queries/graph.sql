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

-- name: ListArgumentGraphSeeds :many
-- Visible node IDs matching the active graph-viewer filters. All filter
-- parameters are nullable / sentinel-blank: pass NULL (or an empty array
-- for tag_names) to skip a predicate. The seeds returned here feed the
-- neighborhood walk, so the depth slider can still expand context around
-- whatever set the filters carve out. Combining filters behaves as AND
-- from the user's perspective; tag_names itself requires every named
-- tag to be attached to a node (intersection, not union). Free-text
-- search is intentionally not part of this query — it stays in
-- SearchNodes (which uses tsvector + trigram) and is intersected at the
-- Go layer when both are active. The cap matches the canvas budget.
SELECT n.id
FROM nodes n
JOIN users u ON u.id = n.created_by
LEFT JOIN groups g ON g.id = n.group_id
WHERE node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
  AND (sqlc.narg(author_username)::text IS NULL
       OR u.username = sqlc.narg(author_username)::text)
  AND (sqlc.narg(group_slug)::text IS NULL
       OR g.slug = sqlc.narg(group_slug)::text)
  AND (sqlc.narg(since)::timestamptz IS NULL
       OR n.created_at >= sqlc.narg(since)::timestamptz)
  AND (sqlc.narg(until)::timestamptz IS NULL
       OR n.created_at <= sqlc.narg(until)::timestamptz)
  AND (sqlc.narg(sourced)::bool IS NULL
       OR (sqlc.narg(sourced)::bool
           AND n.source_url IS NOT NULL AND n.source_url <> '')
       OR (NOT sqlc.narg(sourced)::bool
           AND (n.source_url IS NULL OR n.source_url = '')))
  AND (sqlc.narg(visibility)::visibility_kind IS NULL
       OR n.visibility = sqlc.narg(visibility)::visibility_kind)
  AND (cardinality(sqlc.arg(tag_names)::text[]) = 0
       OR (
         SELECT count(DISTINCT t.name)
         FROM node_tags nt
         JOIN tags t ON t.id = nt.tag_id
         WHERE nt.node_id = n.id
           AND t.name = ANY(sqlc.arg(tag_names)::text[])
       ) = cardinality(sqlc.arg(tag_names)::text[]))
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
