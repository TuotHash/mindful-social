-- name: SetPin :one
-- Upsert: changing your stance from supports → opposes (or any other
-- transition) replaces the existing row in place. created_at is reset on
-- update so the profile shows the most recent stance change first. Returns
-- the pin's id so callers can attach reasonings to it.
INSERT INTO user_node_pins (user_id, node_id, kind)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, node_id) DO UPDATE SET
    kind = EXCLUDED.kind,
    created_at = now()
RETURNING id;

-- name: DeletePin :exec
DELETE FROM user_node_pins WHERE user_id = $1 AND node_id = $2;

-- name: GetPinForUserAndNode :one
-- Whether and how the viewer has pinned a given node. Attached reasonings
-- are loaded separately via ListReasoningsForPin so the same shape covers
-- 0, 1, or many reasonings without nested aggregation here.
SELECT
    p.id,
    p.kind,
    p.created_at
FROM user_node_pins p
WHERE p.user_id = $1 AND p.node_id = $2;

-- name: ListPinsByUser :many
-- A user's pins with the joined node — for the "On my profile" section on
-- a profile page. Reasonings are loaded separately via ListReasoningsForPins
-- to avoid row-multiplying joins.
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
WHERE p.user_id = $1
ORDER BY p.created_at DESC;

-- name: ListReasoningsForPin :many
-- Reasonings attached to a single pin, oldest-attached first so the order
-- the user added them is preserved.
SELECT n.id, n.slug, n.title
FROM pin_reasonings pr
JOIN nodes n ON n.id = pr.reasoning_id
WHERE pr.pin_id = $1
ORDER BY pr.created_at ASC;

-- name: ListReasoningsForPins :many
-- Batch-load reasonings for multiple pins at once — used to avoid N+1 when
-- rendering a profile's pin list. Result includes the pin_id so callers
-- can group by pin.
SELECT pr.pin_id, n.id, n.slug, n.title
FROM pin_reasonings pr
JOIN nodes n ON n.id = pr.reasoning_id
WHERE pr.pin_id = ANY(sqlc.arg(pin_ids)::uuid[])
ORDER BY pr.created_at ASC;

-- name: DeleteReasoningsForPin :exec
-- Clear all attached reasonings for a pin. Combined with AddPinReasoning
-- this implements "replace the set" semantics for the pin form.
DELETE FROM pin_reasonings WHERE pin_id = $1;

-- name: AddPinReasoning :exec
-- Attach one reasoning to a pin. ON CONFLICT DO NOTHING so the same
-- (pin, reasoning) pair is a harmless retry rather than an error.
INSERT INTO pin_reasonings (pin_id, reasoning_id)
VALUES ($1, $2)
ON CONFLICT (pin_id, reasoning_id) DO NOTHING;
