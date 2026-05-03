-- name: CreateAuthIdentity :one
INSERT INTO auth_identities (user_id, provider, subject, secret)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetIdentityByProvider :one
SELECT * FROM auth_identities
WHERE provider = $1 AND subject = $2;

-- name: GetPasswordIdentityForLogin :one
-- Lookup used by the password-login flow: find the user by email AND the
-- bcrypt hash of their password identity in one round-trip.
SELECT u.*, ai.secret AS password_hash, ai.id AS identity_id
FROM users u
JOIN auth_identities ai ON ai.user_id = u.id
WHERE u.email = $1
  AND ai.provider = 'password';

-- name: ListIdentitiesForUser :many
SELECT * FROM auth_identities
WHERE user_id = $1
ORDER BY created_at;
