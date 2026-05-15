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

-- name: ListNodeVideosByUploader :many
SELECT
    v.id,
    v.stored_path,
    v.content_type,
    v.byte_size,
    v.width,
    v.height,
    v.duration_ms,
    v.created_at,
    n.slug AS root_topic_slug,
    n.title AS root_topic_title
FROM node_videos v
JOIN nodes n ON n.id = v.root_topic_id
WHERE v.uploaded_by = $1
ORDER BY v.created_at DESC;
