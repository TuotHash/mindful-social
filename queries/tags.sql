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

-- name: ListAllTags :many
-- Tag list with how many nodes carry each tag — for the /tags index page.
-- Tags with zero nodes (orphans from previous edits) come last.
SELECT t.id, t.name, count(nt.node_id) AS node_count
FROM tags t
LEFT JOIN node_tags nt ON nt.tag_id = t.id
GROUP BY t.id, t.name
ORDER BY node_count DESC, t.name ASC;

-- name: GetTagByName :one
SELECT * FROM tags WHERE name = $1;

-- name: ListNodesWithTag :many
-- Nodes that carry a given tag, most recent first — for /tags/{name}.
SELECT n.* FROM nodes n
JOIN node_tags nt ON nt.node_id = n.id
WHERE nt.tag_id = $1
ORDER BY n.created_at DESC
LIMIT 100;
