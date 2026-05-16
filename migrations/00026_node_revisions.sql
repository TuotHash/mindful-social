-- +goose Up
-- +goose StatementBegin

-- node_revisions captures a snapshot of a node's content (title, body, tags)
-- on every create / edit / revert. The point is to make wiki-open editing
-- self-healing — any user with edit rights can look at the history and roll
-- back. Visibility / group / policy changes are NOT versioned here; they're
-- governance changes that live in the request log, not the content history.
--
-- revision is monotonic per node, starting at 1 (the initial creation). The
-- UNIQUE constraint guards against a race where two concurrent updates pick
-- the same next number — the second insert would fail and the handler would
-- log it (without rolling back the actual edit).
CREATE TABLE node_revisions (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    node_id      UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    revision     INT NOT NULL CHECK (revision >= 1),
    title        TEXT NOT NULL,
    body         TEXT NOT NULL,
    tag_names    TEXT[] NOT NULL DEFAULT '{}',
    edited_by    UUID REFERENCES users(id) ON DELETE SET NULL,
    edited_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    edit_summary TEXT NOT NULL DEFAULT '',
    UNIQUE (node_id, revision)
);

-- Newest-first reads dominate (history listing, "latest revision" lookup),
-- so the secondary index orders by revision DESC. The UNIQUE constraint
-- already gives us (node_id, revision) ascending for primary-key-like reads.
CREATE INDEX idx_node_revisions_node_revision_desc
    ON node_revisions(node_id, revision DESC);

-- Backfill: one revision per existing node, captured from current state so
-- history is never empty after this migration. Tag names come from the
-- existing node_tags join, sorted for stable ordering. edited_at uses the
-- node's updated_at so the timestamp reflects the most recent known edit
-- rather than the migration moment.
INSERT INTO node_revisions (node_id, revision, title, body, tag_names, edited_by, edited_at, edit_summary)
SELECT
    n.id,
    1,
    n.title,
    n.body,
    COALESCE((
        SELECT array_agg(t.name ORDER BY t.name)
        FROM node_tags nt
        JOIN tags t ON t.id = nt.tag_id
        WHERE nt.node_id = n.id
    ), ARRAY[]::text[]),
    n.created_by,
    n.updated_at,
    'Initial revision (backfilled).'
FROM nodes n;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE node_revisions;

-- +goose StatementEnd
