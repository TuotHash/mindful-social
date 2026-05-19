-- Comments are stored as nodes (type='comment') attached to their target
-- via a 'comments_on' edge. Top-level comments point at the discussed
-- node (view, topic, finding, ...); replies point at the parent comment.
-- The single reply level is enforced at write time so the thread UI's
-- two-level layout stays valid.

-- name: CreateCommentNode :one
-- Creates a comment node and the comments_on edge linking it to its target
-- in one statement. Branches on parent_id:
--   parent_id IS NULL → top-level comment on node_id (any non-comment node)
--   parent_id IS NOT  → reply, where parent_id must already be a top-level
--                       comment of node_id (its comments_on points at node_id)
-- Visibility, group_id, and visibility_group_id are inherited from node_id
-- so private/group conversations shield their replies too. The caller
-- supplies comment_id and comment_slug — slug is the synthetic 'c-<uuid>'
-- pattern; it satisfies the NOT NULL UNIQUE constraint without being
-- URL-exposed (redirects use the parent node's slug).
WITH target_node AS (
    SELECT n.id, n.visibility, n.visibility_group_id, n.group_id
    FROM nodes n
    WHERE n.id = sqlc.arg(node_id)::uuid
      AND n.deleted_at IS NULL
      AND n.type <> 'comment'
      AND node_visible_to(n.*, sqlc.arg(author_id)::uuid)
),
parent_comment AS (
    SELECT p.id
    FROM nodes p
    JOIN edges pe ON pe.from_node = p.id
                 AND pe.kind = 'comments_on'
                 AND pe.to_node = sqlc.arg(node_id)::uuid
    WHERE p.id = sqlc.narg(parent_id)::uuid
      AND p.type = 'comment'
      AND p.deleted_at IS NULL
),
attach_to AS (
    SELECT id FROM parent_comment
    WHERE sqlc.narg(parent_id)::uuid IS NOT NULL
    UNION ALL
    SELECT id FROM target_node
    WHERE sqlc.narg(parent_id)::uuid IS NULL
),
new_comment AS (
    INSERT INTO nodes (
        id, type, title, body, slug,
        created_by, visibility, visibility_group_id, group_id
    )
    SELECT
        sqlc.arg(comment_id)::uuid,
        'comment',
        '',
        sqlc.arg(body)::text,
        sqlc.arg(comment_slug)::text,
        sqlc.arg(author_id)::uuid,
        target_node.visibility,
        target_node.visibility_group_id,
        target_node.group_id
    FROM target_node
    WHERE EXISTS (SELECT 1 FROM attach_to)
    RETURNING *
),
new_edge AS (
    INSERT INTO edges (from_node, to_node, kind, created_by)
    SELECT new_comment.id, attach_to.id, 'comments_on', sqlc.arg(author_id)::uuid
    FROM new_comment, attach_to
    RETURNING from_node
)
SELECT * FROM new_comment;

-- name: GetCommentForNode :one
-- Loads a single comment attached (one or two hops) to the named node.
-- Used by the edit/update/delete handlers so a comment from one view
-- can't be touched via another view's URL.
SELECT
    c.id,
    c.body,
    c.created_at,
    c.updated_at,
    c.deleted_at,
    c.created_by AS author_id,
    u.username AS author_username,
    u.profile_image_path AS author_profile_image_path
FROM nodes c
JOIN edges e ON e.from_node = c.id AND e.kind = 'comments_on'
JOIN users u ON u.id = c.created_by
WHERE c.id = sqlc.arg(id)::uuid
  AND c.type = 'comment'
  AND (
      e.to_node = sqlc.arg(node_id)::uuid
      OR e.to_node IN (
          SELECT root.id
          FROM nodes root
          JOIN edges re ON re.from_node = root.id
                       AND re.kind = 'comments_on'
                       AND re.to_node = sqlc.arg(node_id)::uuid
          WHERE root.type = 'comment'
      )
  );

-- name: ListCommentsForNode :many
-- Returns top-level comments attached to node_id, plus their direct
-- replies. parent_id is NULL on top-level rows and set to the parent
-- comment's id on reply rows. Soft-deleted comments stay in the result
-- so the "comment removed" placeholder keeps the thread structure.
WITH top_level AS (
    SELECT c.id, c.created_at
    FROM nodes c
    JOIN edges e ON e.from_node = c.id
                AND e.kind = 'comments_on'
                AND e.to_node = sqlc.arg(node_id)::uuid
    WHERE c.type = 'comment'
)
SELECT
    c.id,
    tl_parent.id AS parent_id,
    c.body,
    c.created_at,
    c.updated_at,
    c.deleted_at,
    c.created_by AS author_id,
    u.username AS author_username,
    u.profile_image_path AS author_profile_image_path
FROM nodes c
JOIN edges e ON e.from_node = c.id AND e.kind = 'comments_on'
JOIN users u ON u.id = c.created_by
LEFT JOIN top_level tl_self   ON tl_self.id = c.id
LEFT JOIN top_level tl_parent ON tl_parent.id = e.to_node
WHERE c.type = 'comment'
  AND (tl_self.id IS NOT NULL OR tl_parent.id IS NOT NULL)
ORDER BY
    COALESCE(tl_parent.created_at, c.created_at) ASC,
    CASE WHEN tl_parent.id IS NULL THEN 0 ELSE 1 END ASC,
    c.created_at ASC;

-- name: UpdateComment :one
-- Edits a comment's body. Only the author can edit, and only while not
-- soft-deleted. updated_at bumps; the UI treats updated_at > created_at
-- as "edited".
UPDATE nodes
SET body = sqlc.arg(body)::text,
    updated_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND type = 'comment'
  AND created_by = sqlc.arg(author_id)::uuid
  AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteComment :execrows
-- Marks a comment removed without deleting it. The thread keeps its shape;
-- the renderer shows "comment removed" in place of the body.
UPDATE nodes
SET deleted_at = now()
WHERE id = sqlc.arg(id)::uuid
  AND type = 'comment'
  AND created_by = sqlc.arg(author_id)::uuid
  AND deleted_at IS NULL;

-- name: HardDeleteComment :execrows
-- Permanent delete used by staff. Removes the comment plus its direct
-- replies (comment nodes whose comments_on edge points at this comment).
-- The matching edges are removed by their own ON DELETE CASCADE.
DELETE FROM nodes
WHERE type = 'comment'
  AND (
      id = sqlc.arg(id)::uuid
      OR id IN (
          SELECT c.id
          FROM nodes c
          JOIN edges e ON e.from_node = c.id
                      AND e.kind = 'comments_on'
                      AND e.to_node = sqlc.arg(id)::uuid
          WHERE c.type = 'comment'
      )
  );
