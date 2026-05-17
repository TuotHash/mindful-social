-- name: SetPin :one
-- Upsert: changing your stance from supports → opposes (or any other
-- transition) replaces the existing row in place. created_at is reset on
-- update so the profile shows the most recent stance change first. Returns
-- the pin's id so callers can attach findings to it.
INSERT INTO user_node_pins (user_id, node_id, kind)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, node_id) DO UPDATE SET
    kind = EXCLUDED.kind,
    created_at = now()
RETURNING id;

-- name: DeletePin :execrows
-- Returns the number of removed rows so callers can 404 a request to
-- unpin a node the user never pinned. Mirrors the pattern on the edge
-- delete / highlight / unhighlight queries.
DELETE FROM user_node_pins WHERE user_id = $1 AND node_id = $2;

-- name: GetPinForUserAndNode :one
-- Whether and how the viewer has pinned a given node. Pins now carry
-- only the stance — findings live as first-class graph nodes attached
-- through edges, not bolted onto the pin record.
SELECT
    p.id,
    p.kind,
    p.created_at
FROM user_node_pins p
WHERE p.user_id = $1 AND p.node_id = $2;

-- name: ListPinsByUser :many
-- A user's pins with the joined node — for the "On my profile" section on
-- a profile page. node_visible_to() hides pins whose underlying node the
-- viewer isn't entitled to see.
SELECT
    p.id,
    p.node_id,
    p.kind,
    p.created_at,
    n.slug  AS node_slug,
    n.type  AS node_type,
    n.title AS node_title
FROM user_node_pins p
JOIN nodes n ON n.id = p.node_id
WHERE p.user_id = sqlc.arg(user_id)
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY p.created_at DESC;
