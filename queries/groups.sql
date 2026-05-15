-- name: CreateGroup :one
INSERT INTO groups (slug, name, description, owner_id, visibility)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: GetGroup :one
SELECT * FROM groups WHERE id = $1;

-- name: GetGroupBySlug :one
SELECT * FROM groups WHERE slug = $1;

-- name: AddGroupMember :exec
INSERT INTO group_memberships (group_id, user_id, role)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, user_id) DO UPDATE
SET role = CASE
  WHEN group_memberships.role = 'owner' THEN group_memberships.role
  ELSE EXCLUDED.role
END;

-- name: RemoveGroupMember :exec
DELETE FROM group_memberships
WHERE group_id = $1 AND user_id = $2 AND role <> 'owner';

-- name: SetGroupMemberRole :execrows
-- Updates a member's role. Owners are protected — their role cannot be
-- changed here; transferring ownership is a separate flow. Reports the
-- number of affected rows so callers can distinguish "no such member"
-- from "owner protected".
UPDATE group_memberships
SET role = $3
WHERE group_id = $1 AND user_id = $2 AND role <> 'owner';

-- name: UpdateGroupMemberVisibility :exec
UPDATE groups SET member_visibility = $2 WHERE id = $1;

-- name: UpdateGroupVisibility :exec
-- Owner-only — the handler enforces that. Stored as the same enum the
-- create form populates: 'public' | 'connections' | 'private'.
UPDATE groups SET visibility = $2 WHERE id = $1;

-- name: CanViewGroup :one
-- Single-group visibility probe used by resolveGroup() to gate the
-- detail page for non-public groups. Mirrors the visibility branches
-- in ListVisibleGroups: members always pass, connections groups pass
-- when viewer mutually follows the owner, private groups require
-- membership.
SELECT (
  g.visibility = 'public'
  OR EXISTS (
    SELECT 1 FROM group_memberships m
    WHERE m.group_id = g.id
      AND m.user_id = sqlc.narg(viewer_id)::uuid
  )
  OR (
    g.visibility = 'connections'
    AND sqlc.narg(viewer_id)::uuid IS NOT NULL
    AND EXISTS (
      SELECT 1 FROM follows f1
      JOIN follows f2
        ON f2.follower_id = f1.followed_id
       AND f2.followed_id = f1.follower_id
      WHERE f1.follower_id = sqlc.narg(viewer_id)::uuid
        AND f1.followed_id = g.owner_id
    )
  )
)::bool AS can_view
FROM groups g
WHERE g.id = sqlc.arg(group_id)::uuid;

-- name: GetGroupMembership :one
SELECT * FROM group_memberships
WHERE group_id = $1 AND user_id = $2;

-- name: IsGroupMember :one
SELECT EXISTS(
  SELECT 1 FROM group_memberships
  WHERE group_id = $1 AND user_id = $2
)::bool AS is_member;

-- name: CountGroupMembers :one
SELECT count(*)::bigint FROM group_memberships WHERE group_id = $1;

-- name: ListGroupMembers :many
SELECT u.id, u.username, m.role, m.joined_at
FROM group_memberships m
JOIN users u ON u.id = m.user_id
WHERE m.group_id = $1
ORDER BY
  CASE m.role WHEN 'owner' THEN 0 WHEN 'admin' THEN 1 ELSE 2 END,
  u.username ASC;

-- name: ListVisibleGroups :many
-- Three visibility branches:
--   public       — visible to everyone (the public/anon visitor included)
--   connections  — visible to the owner's mutual followers + members
--   private      — visible only to members
-- The viewer_role / is_member columns power the badge/CTA logic in the
-- groups index; member_count is a per-row aggregate so we don't need a
-- separate hit per group.
SELECT
  g.id,
  g.slug,
  g.name,
  g.description,
  g.owner_id,
  g.visibility,
  g.created_at,
  gm.role AS viewer_role,
  (gm.user_id IS NOT NULL)::bool AS is_member,
  (SELECT count(*)::bigint FROM group_memberships m WHERE m.group_id = g.id) AS member_count
FROM groups g
LEFT JOIN group_memberships gm
  ON gm.group_id = g.id
 AND gm.user_id = sqlc.narg(viewer_id)::uuid
WHERE g.visibility = 'public'
   OR gm.user_id IS NOT NULL
   OR (
     g.visibility = 'connections'
     AND sqlc.narg(viewer_id)::uuid IS NOT NULL
     AND EXISTS (
       SELECT 1 FROM follows f1
       JOIN follows f2
         ON f2.follower_id = f1.followed_id
        AND f2.followed_id = f1.follower_id
       WHERE f1.follower_id = sqlc.narg(viewer_id)::uuid
         AND f1.followed_id = g.owner_id
     )
   )
ORDER BY g.created_at DESC;

-- name: SearchGroups :many
-- Trigram fuzzy match against the group name. Mirrors SearchUsers but
-- additionally gates each row by the viewer's right to see the group
-- (see ListVisibleGroups for the visibility branches). The %> threshold
-- is the same one the picker uses elsewhere (set in pgxpool.AfterConnect).
SELECT
  g.id,
  g.slug,
  g.name,
  g.description,
  g.visibility,
  (SELECT count(*)::bigint FROM group_memberships m WHERE m.group_id = g.id) AS member_count
FROM groups g
WHERE (
    g.name ILIKE '%' || sqlc.arg(query)::text || '%'
    OR g.name %> sqlc.arg(query)::text
  )
  AND (
    g.visibility = 'public'
    OR (
      sqlc.narg(viewer_id)::uuid IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM group_memberships m
        WHERE m.group_id = g.id
          AND m.user_id = sqlc.narg(viewer_id)::uuid
      )
    )
    OR (
      g.visibility = 'connections'
      AND sqlc.narg(viewer_id)::uuid IS NOT NULL
      AND EXISTS (
        SELECT 1 FROM follows f1
        JOIN follows f2
          ON f2.follower_id = f1.followed_id
         AND f2.followed_id = f1.follower_id
        WHERE f1.follower_id = sqlc.narg(viewer_id)::uuid
          AND f1.followed_id = g.owner_id
      )
    )
  )
ORDER BY
  CASE WHEN g.name ILIKE sqlc.arg(query)::text || '%' THEN 0 ELSE 1 END,
  word_similarity(sqlc.arg(query)::text, g.name) DESC,
  g.name ASC
LIMIT sqlc.arg(result_limit);

-- name: ListGroupsForUser :many
SELECT
  g.id,
  g.slug,
  g.name,
  g.description,
  g.owner_id,
  g.visibility,
  g.created_at,
  m.role
FROM group_memberships m
JOIN groups g ON g.id = m.group_id
WHERE m.user_id = $1
ORDER BY g.name ASC;

-- name: ListNodesForGroupForViewer :many
SELECT * FROM nodes n
WHERE n.group_id = sqlc.arg(group_id)::uuid
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY n.created_at DESC
LIMIT sqlc.arg(result_limit);

-- name: CreateGroupInvite :one
INSERT INTO group_invites (group_id, invited_user_id, invited_by)
VALUES ($1, $2, $3)
ON CONFLICT (group_id, invited_user_id) DO UPDATE
SET invited_by = EXCLUDED.invited_by,
    created_at = now(),
    accepted_at = NULL
RETURNING *;

-- name: AcceptGroupInvite :execrows
UPDATE group_invites
SET accepted_at = now()
WHERE id = $1 AND invited_user_id = $2 AND accepted_at IS NULL;

-- name: ListPendingGroupInvitesForUser :many
SELECT
  i.id,
  i.group_id,
  g.slug AS group_slug,
  g.name AS group_name,
  i.invited_by,
  u.username AS invited_by_username,
  i.created_at
FROM group_invites i
JOIN groups g ON g.id = i.group_id
JOIN users u ON u.id = i.invited_by
WHERE i.invited_user_id = $1
  AND i.accepted_at IS NULL
ORDER BY i.created_at DESC;
