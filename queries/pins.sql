-- name: SetPin :exec
-- Upsert: changing your stance from supports → opposes (or any other
-- transition) replaces the existing row in place. created_at is reset on
-- update so the profile shows the most recent stance change first.
INSERT INTO user_node_pins (user_id, node_id, kind, reasoning_id)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, node_id) DO UPDATE SET
    kind = EXCLUDED.kind,
    reasoning_id = EXCLUDED.reasoning_id,
    created_at = now();

-- name: DeletePin :exec
DELETE FROM user_node_pins WHERE user_id = $1 AND node_id = $2;

-- name: GetPinForUserAndNode :one
-- Whether and how the viewer has pinned a given node. Returns the optional
-- reasoning's title alongside, so the node detail page can render
-- "you support this via <reasoning title>" in one round-trip.
SELECT
    p.kind,
    p.reasoning_id,
    p.created_at,
    rn.slug  AS reasoning_slug,
    rn.title AS reasoning_title
FROM user_node_pins p
LEFT JOIN nodes rn ON rn.id = p.reasoning_id
WHERE p.user_id = $1 AND p.node_id = $2;

-- name: ListPinsByUser :many
-- A user's pins with the joined node and optional reasoning titles — for
-- the "On my profile" section on a profile page.
SELECT
    p.node_id,
    p.kind,
    p.reasoning_id,
    p.created_at,
    n.slug    AS node_slug,
    n.type    AS node_type,
    n.title   AS node_title,
    rn.slug   AS reasoning_slug,
    rn.title  AS reasoning_title
FROM user_node_pins p
JOIN nodes n ON n.id = p.node_id
LEFT JOIN nodes rn ON rn.id = p.reasoning_id
WHERE p.user_id = $1
ORDER BY p.created_at DESC;
