-- name: FindRootTopicForNode :one
-- Walk the parent chain to the topmost ancestor. Returns the id only when
-- that ancestor is a topic; otherwise no row (callers reject the upload).
-- The `parent_node_id IS NOT NULL` cycle guard mirrors node_visible_to() —
-- a malformed cycle would otherwise loop forever.
WITH RECURSIVE chain(id, parent_node_id, type, path) AS (
    SELECT n.id, n.parent_node_id, n.type, ARRAY[n.id]
    FROM nodes n
    WHERE n.id = $1
    UNION ALL
    SELECT n.id, n.parent_node_id, n.type, chain.path || n.id
    FROM chain
    JOIN nodes n ON n.id = chain.parent_node_id
    WHERE chain.parent_node_id IS NOT NULL
      AND NOT n.id = ANY(chain.path)
)
SELECT chain.id
FROM chain
WHERE chain.parent_node_id IS NULL
  AND chain.type = 'topic'
LIMIT 1;

-- name: CreateNodeImage :one
INSERT INTO node_images (
    root_topic_id,
    uploaded_by,
    stored_path,
    content_type,
    byte_size
) VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListNodeImagesForRoot :many
SELECT id, stored_path, content_type, byte_size, created_at
FROM node_images
WHERE root_topic_id = $1
ORDER BY created_at DESC;
