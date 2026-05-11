-- name: CreateUser :one
INSERT INTO users (username, email)
VALUES ($1, $2)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = $1;

-- name: GetUserByEmail :one
SELECT * FROM users WHERE email = $1;

-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1;

-- name: ListUsersForAdmin :many
-- Roster for the admin /users page. Recent signups first; staff bubble to
-- the top so they're easy to find.
SELECT * FROM users
ORDER BY
    CASE role
        WHEN 'admin' THEN 0
        WHEN 'moderator' THEN 1
        ELSE 2
    END,
    created_at DESC;

-- name: UpdateUserRole :exec
UPDATE users SET role = $2 WHERE id = $1;

-- name: UpdateUserUsername :exec
UPDATE users SET username = $2 WHERE id = $1;

-- name: UpdateUserEmail :exec
UPDATE users SET email = $2 WHERE id = $1;

-- name: UpdateUserPreferences :exec
-- Updates all three composer/display defaults at once. The audience-list FK
-- is nullable; pass NULL whenever default_node_visibility is anything other
-- than 'list'. Timezone is an IANA name (empty string = fall back to UTC).
UPDATE users
SET default_node_visibility = $2,
    default_audience_list_id = $3,
    timezone = $4
WHERE id = $1;
