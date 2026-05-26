-- name: CreateNotification :exec
INSERT INTO notifications (recipient_id, actor_id, kind, node_id)
VALUES ($1, $2, $3, $4);

-- name: ListNotificationsForUser :many
SELECT
  n.id,
  n.kind,
  n.node_id,
  n.read_at,
  n.created_at,
  a.id                   AS actor_id,
  a.username             AS actor_username,
  a.profile_image_path   AS actor_profile_image_path,
  COALESCE(nd.title, '') AS node_title,
  COALESCE(nd.slug, '')  AS node_slug
FROM notifications n
JOIN users a ON a.id = n.actor_id
LEFT JOIN nodes nd ON nd.id = n.node_id
WHERE n.recipient_id = $1
ORDER BY n.created_at DESC
LIMIT 50;

-- name: CountUnreadNotifications :one
SELECT count(*)::bigint
FROM notifications
WHERE recipient_id = $1 AND read_at IS NULL;

-- name: MarkAllNotificationsRead :exec
UPDATE notifications
SET read_at = now()
WHERE recipient_id = $1 AND read_at IS NULL;
