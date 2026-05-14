-- name: CreateNodeVideo :one
INSERT INTO node_videos (
    root_topic_id,
    uploaded_by,
    stored_path,
    content_type,
    byte_size,
    width,
    height,
    duration_ms
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING *;

-- name: ListNodeVideosForRoot :many
SELECT id, stored_path, content_type, byte_size, width, height, duration_ms, created_at
FROM node_videos
WHERE root_topic_id = $1
ORDER BY created_at DESC;
