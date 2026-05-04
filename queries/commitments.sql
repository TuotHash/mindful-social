-- name: CreateCommitment :one
-- Creates a user's stance on a view, optionally with a personal reasoning
-- node attached. UNIQUE(user_id, view_id) ensures at most one commitment per
-- user per view, so callers should treat 23505 as "already committed".
INSERT INTO commitments (user_id, view_id, reasoning_id)
VALUES ($1, $2, $3)
RETURNING *;

-- name: DeleteCommitment :exec
DELETE FROM commitments WHERE user_id = $1 AND view_id = $2;

-- name: GetCommitmentForUserAndView :one
-- Whether and how a specific user has committed to a specific view. Returns
-- the optional reasoning's title alongside, so the view page can render
-- "you've committed via <reasoning title>" in one round-trip.
SELECT
    c.user_id,
    c.view_id,
    c.reasoning_id,
    c.created_at,
    rn.title AS reasoning_title
FROM commitments c
LEFT JOIN nodes rn ON rn.id = c.reasoning_id
WHERE c.user_id = $1 AND c.view_id = $2;

-- name: ListReasoningsAuthoredBy :many
-- Reasoning-type nodes the user has authored, alphabetical — used as the
-- picker on the commit-to-view form.
SELECT id, title FROM nodes
WHERE type = 'reasoning' AND created_by = $1
ORDER BY title ASC
LIMIT 200;

-- name: ListCommitmentsByUser :many
-- A user's commitments with the joined view title and optional reasoning
-- title — for the "committed views" section on a profile page.
SELECT
    c.view_id,
    c.reasoning_id,
    c.created_at,
    vn.title AS view_title,
    rn.title AS reasoning_title
FROM commitments c
JOIN nodes vn ON vn.id = c.view_id
LEFT JOIN nodes rn ON rn.id = c.reasoning_id
WHERE c.user_id = $1
ORDER BY c.created_at DESC;