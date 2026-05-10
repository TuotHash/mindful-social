-- name: CreateFollow :exec
-- Idempotent: re-following is a no-op rather than an error so the button
-- handler doesn't need to disambiguate states.
INSERT INTO follows (follower_id, followed_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: DeleteFollow :exec
DELETE FROM follows WHERE follower_id = $1 AND followed_id = $2;

-- name: CountFollowers :one
SELECT count(*)::bigint FROM follows WHERE followed_id = $1;

-- name: CountFollowing :one
SELECT count(*)::bigint FROM follows WHERE follower_id = $1;

-- name: GetFollowState :one
-- One round-trip lookup the profile page uses to render the button: does
-- the viewer follow this profile, and does the profile follow the viewer
-- back (mutual = connection)?
SELECT
  EXISTS(
    SELECT 1 FROM follows f
    WHERE f.follower_id = sqlc.arg(viewer_id)::uuid
      AND f.followed_id = sqlc.arg(profile_id)::uuid
  )::bool AS viewer_follows,
  EXISTS(
    SELECT 1 FROM follows f
    WHERE f.follower_id = sqlc.arg(profile_id)::uuid
      AND f.followed_id = sqlc.arg(viewer_id)::uuid
  )::bool AS follows_viewer;

-- name: ListConnections :many
-- People the user has a mutual follow with — drives the "friends bubble"
-- graph view. Alphabetical for stable rendering.
SELECT u.id, u.username
FROM follows f1
JOIN follows f2
  ON f2.follower_id = f1.followed_id
 AND f2.followed_id = f1.follower_id
JOIN users u ON u.id = f1.followed_id
WHERE f1.follower_id = $1
ORDER BY u.username ASC;

-- name: ListFollowing :many
-- Users that $1 follows.
SELECT u.id, u.username
FROM follows f
JOIN users u ON u.id = f.followed_id
WHERE f.follower_id = $1
ORDER BY u.username ASC;

-- name: ListFollowers :many
-- Users that follow $1.
SELECT u.id, u.username
FROM follows f
JOIN users u ON u.id = f.follower_id
WHERE f.followed_id = $1
ORDER BY u.username ASC;
