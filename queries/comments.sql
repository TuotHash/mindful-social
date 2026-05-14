-- name: CreateComment :one
WITH target AS (
  SELECT n.id
  FROM nodes n
  WHERE n.id = sqlc.arg(node_id)::uuid
    AND n.type = 'view'
    AND node_visible_to(n.*, sqlc.arg(author_id)::uuid)
),
parent AS (
  SELECT c.id
  FROM comments c
  WHERE c.id = sqlc.narg(parent_id)::uuid
    AND c.node_id = sqlc.arg(node_id)::uuid
    AND c.parent_id IS NULL
)
INSERT INTO comments (node_id, parent_id, author_id, body)
SELECT
  target.id,
  sqlc.narg(parent_id)::uuid,
  sqlc.arg(author_id)::uuid,
  sqlc.arg(body)::text
FROM target
WHERE sqlc.narg(parent_id)::uuid IS NULL
   OR EXISTS (SELECT 1 FROM parent)
RETURNING *;

-- name: GetCommentForNode :one
SELECT
  c.*,
  u.username AS author_username,
  u.profile_image_path AS author_profile_image_path
FROM comments c
JOIN users u ON u.id = c.author_id
WHERE c.id = sqlc.arg(id)::uuid
  AND c.node_id = sqlc.arg(node_id)::uuid;

-- name: ListCommentsForNode :many
SELECT
  c.*,
  u.username AS author_username,
  u.profile_image_path AS author_profile_image_path
FROM comments c
JOIN users u ON u.id = c.author_id
LEFT JOIN comments parent ON parent.id = c.parent_id
WHERE c.node_id = sqlc.arg(node_id)::uuid
ORDER BY
  COALESCE(parent.created_at, c.created_at) ASC,
  CASE WHEN c.parent_id IS NULL THEN 0 ELSE 1 END ASC,
  c.created_at ASC;

-- name: UpdateComment :one
UPDATE comments
SET body = sqlc.arg(body)::text,
    edited_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND node_id = sqlc.arg(node_id)::uuid
  AND author_id = sqlc.arg(author_id)::uuid
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteComment :execrows
UPDATE comments
SET deleted_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND node_id = sqlc.arg(node_id)::uuid
  AND author_id = sqlc.arg(author_id)::uuid
  AND deleted_at IS NULL;

-- name: HardDeleteComment :execrows
DELETE FROM comments
WHERE id = sqlc.arg(id)::uuid
  AND node_id = sqlc.arg(node_id)::uuid;
