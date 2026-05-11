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
