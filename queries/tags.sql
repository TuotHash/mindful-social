-- name: UpsertTag :one
-- Idempotent: returns the tag id whether it already existed or just got
-- inserted. The DO UPDATE SET name=EXCLUDED.name is a no-op that exists
-- only so RETURNING fires on the conflict path.
INSERT INTO tags (name) VALUES ($1)
ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name
RETURNING id;

-- name: AttachTag :exec
INSERT INTO node_tags (node_id, tag_id) VALUES ($1, $2)
ON CONFLICT DO NOTHING;

-- name: DeleteTagsForNode :exec
-- Used by the "replace all tags" path on node update — the handler deletes
-- the existing rows and re-inserts the new set.
DELETE FROM node_tags WHERE node_id = $1;

-- name: ListTagsForNode :many
SELECT t.id, t.name FROM tags t
JOIN node_tags nt ON nt.tag_id = t.id
WHERE nt.node_id = $1
ORDER BY t.name ASC;

-- name: ListAllTagsForViewer :many
-- Tag list with how many nodes carry each tag — for the /tags index page.
-- Counts only nodes the viewer is allowed to see (public, plus connections-
-- /list-/private-scoped nodes the viewer has access to). Tags whose visible
-- node count is zero are dropped so we don't leak the existence of tags that
-- only live on private nodes.
SELECT t.id, t.name, count(n.id) AS node_count
FROM tags t
JOIN node_tags nt ON nt.tag_id = t.id
JOIN nodes n ON n.id = nt.node_id
WHERE node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
GROUP BY t.id, t.name
ORDER BY node_count DESC, t.name ASC;

-- name: GetTagByName :one
SELECT * FROM tags WHERE name = $1;

-- name: ListNodesWithTagForViewer :many
-- Nodes that carry a given tag, most recent first — for /tags/{name}.
-- Filtered through node_visible_to() so a viewer only sees nodes they can.
SELECT n.* FROM nodes n
JOIN node_tags nt ON nt.node_id = n.id
WHERE nt.tag_id = sqlc.arg(tag_id)
  AND node_visible_to(n.*, sqlc.narg(viewer_id)::uuid)
ORDER BY n.created_at DESC
LIMIT 100;
