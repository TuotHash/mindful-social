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

-- name: DeletePin :exec
DELETE FROM user_node_pins WHERE user_id = $1 AND node_id = $2;

-- name: GetPinForUserAndNode :one
-- Whether and how the viewer has pinned a given node. Attached findings
-- are loaded separately via ListFindingsForPin so the same shape covers
-- 0, 1, or many findings without nested aggregation here.
SELECT
    p.id,
    p.kind,
    p.created_at
FROM user_node_pins p
WHERE p.user_id = $1 AND p.node_id = $2;

-- name: ListPinsByUser :many
-- A user's pins with the joined node — for the "On my profile" section on
-- a profile page. Findings are loaded separately via ListFindingsForPins
-- to avoid row-multiplying joins. node_visible_to() hides pins whose
-- underlying node the viewer isn't entitled to see.
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

-- name: ListFindingsForPin :many
-- Findings attached to a single pin, oldest-attached first so the order
-- the user added them is preserved. Hidden findings are filtered out.
SELECT n.id, n.slug, n.title
FROM pin_findings pf
JOIN nodes n ON n.id = pf.finding_id
WHERE pf.pin_id = sqlc.arg(pin_id)
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY pf.created_at ASC;

-- name: ListFindingsForPins :many
-- Batch-load findings for multiple pins at once — used to avoid N+1 when
-- rendering a profile's pin list. Result includes the pin_id so callers
-- can group by pin. Hidden findings are filtered out.
SELECT pf.pin_id, n.id, n.slug, n.title
FROM pin_findings pf
JOIN nodes n ON n.id = pf.finding_id
WHERE pf.pin_id = ANY(sqlc.arg(pin_ids)::uuid[])
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY pf.created_at ASC;

-- name: DeleteFindingsForPin :exec
-- Clear all attached findings for a pin. Combined with AddPinFinding
-- this implements "replace the set" semantics for the pin form.
DELETE FROM pin_findings WHERE pin_id = $1;

-- name: AddPinFinding :exec
-- Attach one finding to a pin. ON CONFLICT DO NOTHING so the same
-- (pin, finding) pair is a harmless retry rather than an error.
INSERT INTO pin_findings (pin_id, finding_id)
VALUES ($1, $2)
ON CONFLICT (pin_id, finding_id) DO NOTHING;
