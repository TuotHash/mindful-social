-- name: CreateAudienceList :one
-- Creates a new list for an owner. The unique index on (owner_id) WHERE
-- is_trusted means at most one trusted list per user; passing is_trusted=true
-- twice for the same owner raises a unique-violation that the caller maps to
-- a friendly error.
INSERT INTO audience_lists (owner_id, name, is_trusted)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetAudienceList :one
SELECT * FROM audience_lists WHERE id = $1;

-- name: GetTrustedList :one
-- Lookup the user's built-in Trusted list. Should always succeed for
-- existing users (created on signup or backfilled by the migration).
SELECT * FROM audience_lists WHERE owner_id = $1 AND is_trusted;

-- name: ListAudienceLists :many
-- Trusted list first, then custom lists alphabetically — order the visibility
-- selector and the lists-management page both rely on.
SELECT * FROM audience_lists
WHERE owner_id = $1
ORDER BY is_trusted DESC, name ASC;

-- name: DeleteAudienceList :exec
-- Trusted lists can't be deleted; the WHERE clause enforces this without
-- the caller having to remember.
DELETE FROM audience_lists
WHERE id = $1 AND owner_id = $2 AND NOT is_trusted;

-- name: AddListMember :exec
-- Idempotent. The handler treats "already a member" the same as "now a
-- member" so the form doesn't need to know which it was.
INSERT INTO audience_list_members (list_id, member_user_id)
VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: RemoveListMember :exec
DELETE FROM audience_list_members
WHERE list_id = $1 AND member_user_id = $2;

-- name: ListListMembers :many
SELECT u.id, u.username, m.added_at
FROM audience_list_members m
JOIN users u ON u.id = m.member_user_id
WHERE m.list_id = $1
ORDER BY u.username ASC;

-- name: CountListMembers :one
SELECT count(*)::bigint FROM audience_list_members WHERE list_id = $1;

-- name: IsListMember :one
SELECT EXISTS(
  SELECT 1 FROM audience_list_members
  WHERE list_id = $1 AND member_user_id = $2
)::bool AS is_member;
