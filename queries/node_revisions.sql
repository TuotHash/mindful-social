-- name: CreateNodeRevision :one
-- Writes a new revision for a node, picking the next revision number atomically
-- by reading max(revision)+1 in the same statement. The UNIQUE(node_id,
-- revision) constraint backs this up if two updates race.
INSERT INTO node_revisions (
    node_id, revision, title, body, tag_names, edited_by, edit_summary
)
VALUES (
    sqlc.arg(node_id),
    COALESCE((SELECT max(revision) FROM node_revisions WHERE node_id = sqlc.arg(node_id)), 0) + 1,
    sqlc.arg(title),
    sqlc.arg(body),
    sqlc.arg(tag_names)::text[],
    sqlc.narg(edited_by)::uuid,
    sqlc.arg(edit_summary)
)
RETURNING *;

-- name: ListNodeRevisions :many
-- All revisions for a node, newest first. editor_username is null when the
-- user has been deleted (FK is ON DELETE SET NULL) — the UI shows "deleted
-- user" in that case.
SELECT
    r.id,
    r.node_id,
    r.revision,
    r.title,
    r.tag_names,
    r.edited_by,
    r.edited_at,
    r.edit_summary,
    u.username AS editor_username
FROM node_revisions r
LEFT JOIN users u ON u.id = r.edited_by
WHERE r.node_id = $1
ORDER BY r.revision DESC;

-- name: GetNodeRevision :one
-- A single revision identified by (node_id, revision). Returns the full body
-- so the snapshot view can render it.
SELECT
    r.id,
    r.node_id,
    r.revision,
    r.title,
    r.body,
    r.tag_names,
    r.edited_by,
    r.edited_at,
    r.edit_summary,
    u.username AS editor_username
FROM node_revisions r
LEFT JOIN users u ON u.id = r.edited_by
WHERE r.node_id = $1 AND r.revision = $2;

-- name: GetLatestNodeRevision :one
-- The newest revision for a node. The update handler reads this to decide
-- whether the incoming save actually changes anything — if title/body/tags
-- all match, we skip writing a spurious duplicate.
SELECT * FROM node_revisions
WHERE node_id = $1
ORDER BY revision DESC
LIMIT 1;
