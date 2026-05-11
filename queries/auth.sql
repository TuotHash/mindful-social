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

-- name: GetPasswordIdentityForUser :one
SELECT * FROM auth_identities
WHERE user_id = $1 AND provider = 'password'
LIMIT 1;

-- name: UpdatePasswordIdentitySecret :exec
UPDATE auth_identities
SET secret = $2
WHERE user_id = $1 AND provider = 'password';

-- name: UpdatePasswordIdentitySubject :exec
-- Keeps the password identity's subject (an email mirror) aligned with the
-- user's current email when admins change it. Login itself looks up by
-- users.email, so the subject is essentially metadata — but we keep it in
-- sync to avoid future surprises.
UPDATE auth_identities
SET subject = $2
WHERE user_id = $1 AND provider = 'password';

-- name: GetIdentityForUser :one
SELECT * FROM auth_identities WHERE id = $1 AND user_id = $2;

-- name: DeleteAuthIdentityForUser :exec
DELETE FROM auth_identities WHERE id = $1 AND user_id = $2;

-- name: CountIdentitiesForUser :one
SELECT COUNT(*) FROM auth_identities WHERE user_id = $1;
