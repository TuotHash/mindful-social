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
