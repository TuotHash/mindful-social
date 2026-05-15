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

-- name: SearchUsers :many
-- People search for /search. Prefix/substring matching makes exact handle
-- discovery predictable, while trigram word similarity catches small typos.
SELECT id, username, created_at
FROM users
WHERE username ILIKE '%' || sqlc.arg(query)::text || '%'
   OR username %> sqlc.arg(query)::text
ORDER BY
    CASE WHEN username ILIKE sqlc.arg(query)::text || '%' THEN 0 ELSE 1 END,
    word_similarity(sqlc.arg(query)::text, username) DESC,
    username ASC
LIMIT sqlc.arg(result_limit);

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
-- Updates the composer and display defaults. Timezone is an IANA name
-- (empty string = fall back to UTC).
UPDATE users
SET default_node_visibility = $2,
    timezone = $3
WHERE id = $1;

-- name: UpdateUserProfileImage :exec
UPDATE users SET profile_image_path = $2 WHERE id = $1;
