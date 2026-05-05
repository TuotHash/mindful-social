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

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = $1;

-- name: CountEdgesForNode :one
-- Total edges (incoming + outgoing) that would cascade-delete if the node
-- were removed. Used on the deletion confirmation page.
SELECT count(*) FROM edges WHERE from_node = $1 OR to_node = $1;

-- name: CountOtherUserPinsForNode :one
-- Pins on this node by users other than the node's author. The author's own
-- pin is excluded — they obviously consent to losing it. Other users' pins
-- are surfaced on the confirmation page so the author knows what cascades.
SELECT count(*) FROM user_node_pins WHERE node_id = $1 AND user_id <> $2;

-- name: ListNodesAuthoredBy :many
-- Nodes a user has authored, most recent first — for the "Authored" section
-- on a profile page.
SELECT * FROM nodes
WHERE created_by = $1
ORDER BY created_at DESC
LIMIT $2;

-- name: PickerSearchNodes :many
-- Title/body full-text search for the edge-creation picker. Excludes the
-- source node (sqlc parameter $2) and returns just enough columns to render
-- a radio list. Empty queries return nothing — the form's empty state tells
-- the user to type to search.
SELECT n.id, n.type, n.title
FROM nodes n, websearch_to_tsquery('english', $1) q
WHERE n.search_tsv @@ q
  AND n.id != $2
ORDER BY ts_rank(n.search_tsv, q) DESC, n.title ASC
LIMIT 50;

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
    -- Cast to text so sqlc maps it to Go string instead of []byte.
    -- Without the cast, sqlc can't infer the return type of ts_headline.
    ts_headline('english', n.body, q,
                'StartSel=«HL», StopSel=«/HL», MaxFragments=1, MaxWords=24, MinWords=8')::text AS excerpt
FROM nodes n, websearch_to_tsquery('english', $1) q
WHERE n.search_tsv @@ q
ORDER BY rank DESC, n.created_at DESC
LIMIT $2;
