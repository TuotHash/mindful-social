-- name: CreateNode :one
INSERT INTO nodes (type, title, body, source_url, created_by)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1;

-- name: ListRecentNodes :many
SELECT * FROM nodes
ORDER BY created_at DESC
LIMIT $1;

-- name: ListNodesByType :many
SELECT * FROM nodes
WHERE type = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: UpdateNode :one
UPDATE nodes
SET title = $2,
    body = $3,
    source_url = $4,
    updated_at = now()
WHERE id = $1
RETURNING *;

-- name: CountNodes :one
SELECT count(*) FROM nodes;

-- name: ListNodesExcept :many
-- All nodes except the given one, alphabetically — used to populate the
-- target-node picker on the edge creation form.
SELECT * FROM nodes
WHERE id != $1
ORDER BY title ASC
LIMIT 500;

-- name: ListNodesAuthoredBy :many
-- Nodes a user has authored, most recent first — for the "Authored" section
-- on a profile page.
SELECT * FROM nodes
WHERE created_by = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: SearchNodes :many
-- Full-text search over title + body using a precomputed tsvector. The query
-- uses websearch_to_tsquery so arbitrary user input is safe (it tolerates
-- bad punctuation and supports "phrases" + OR). ts_rank orders by relevance,
-- with creation date as a tiebreaker. ts_headline produces a short marked-up
-- excerpt the template can render directly.
SELECT
    n.id, n.type, n.title, n.body, n.source_url, n.created_by,
    n.created_at, n.updated_at,
    ts_rank(n.search_tsv, q) AS rank,
    ts_headline('english', n.body, q,
                'StartSel=«HL», StopSel=«/HL», MaxFragments=1, MaxWords=24, MinWords=8') AS excerpt
FROM nodes n, websearch_to_tsquery('english', $1) q
WHERE n.search_tsv @@ q
ORDER BY rank DESC, n.created_at DESC
LIMIT $2;
