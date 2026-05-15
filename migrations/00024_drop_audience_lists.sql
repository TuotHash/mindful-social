-- +goose Up
-- +goose StatementBegin

-- Audience Lists were the per-user "trusted recipients" mechanism we
-- shipped before groups. Groups now cover the same use case (a group
-- with member_visibility=owner and everyone as member is functionally
-- a private recipient list owned by one person), so we remove the list
-- machinery entirely: tables, columns, the 'list' visibility kind, and
-- the list branch of node_local_visible_to.

-- Drop the visibility functions first — they reference the `nodes`
-- composite type via `nodes.*`, and Postgres won't let us alter the
-- columns those functions read while they exist.
DROP FUNCTION IF EXISTS node_visible_to(nodes, UUID);
DROP FUNCTION IF EXISTS node_local_visible_to(nodes, UUID);

-- Drop the multi-branch CHECK that ties visibility to list_id vs group_id.
-- We re-add a slimmer version after the column is gone.
ALTER TABLE nodes DROP CONSTRAINT IF EXISTS visibility_target_consistency;

-- Drop the FK columns that reference audience_lists, so the tables can
-- be dropped after this. Any node still set to visibility='list' will
-- be coerced to 'private' below.
ALTER TABLE users DROP COLUMN IF EXISTS default_audience_list_id;
ALTER TABLE nodes DROP COLUMN IF EXISTS visibility_list_id;

-- Rewrite any stranded list-visibility values to 'private'. We pick
-- private (not public) because it's the safer default — losing access
-- is recoverable, accidentally exposing posts is not.
UPDATE nodes SET visibility = 'private' WHERE visibility = 'list';
UPDATE users SET default_node_visibility = 'private' WHERE default_node_visibility = 'list';

-- Type-swap visibility_kind to drop the 'list' value. Same pattern as
-- earlier migrations: Postgres can't remove enum values directly.
ALTER TYPE visibility_kind RENAME TO visibility_kind_old;
CREATE TYPE visibility_kind AS ENUM ('public', 'connections', 'group', 'private');

ALTER TABLE nodes ALTER COLUMN visibility DROP DEFAULT;
ALTER TABLE nodes
  ALTER COLUMN visibility TYPE visibility_kind
  USING visibility::text::visibility_kind;
ALTER TABLE nodes ALTER COLUMN visibility SET DEFAULT 'public';

ALTER TABLE users ALTER COLUMN default_node_visibility DROP DEFAULT;
ALTER TABLE users
  ALTER COLUMN default_node_visibility TYPE visibility_kind
  USING default_node_visibility::text::visibility_kind;
ALTER TABLE users ALTER COLUMN default_node_visibility SET DEFAULT 'public';

DROP TYPE visibility_kind_old;

-- Re-add a slimmed visibility/target consistency check: only 'group'
-- nodes carry a visibility_group_id; every other visibility leaves the
-- target columns NULL.
ALTER TABLE nodes ADD CONSTRAINT visibility_target_consistency CHECK (
  (visibility = 'group' AND visibility_group_id IS NOT NULL)
  OR (visibility <> 'group' AND visibility_group_id IS NULL)
);

-- Now the lists tables have no live references and can go.
DROP TABLE IF EXISTS audience_list_members;
DROP TABLE IF EXISTS audience_lists;

-- Recreate the visibility functions without the list branch. Same
-- shape as in 00017 minus the audience_list_members check.
CREATE FUNCTION node_local_visible_to(target nodes, viewer_id UUID) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    SELECT
      target.visibility = 'public'
      OR (viewer_id IS NOT NULL AND target.created_by = viewer_id)
      OR (
        viewer_id IS NOT NULL
        AND target.visibility = 'connections'
        AND EXISTS (
          SELECT 1 FROM follows f1
          JOIN follows f2
            ON f2.follower_id = f1.followed_id
           AND f2.followed_id = f1.follower_id
          WHERE f1.follower_id = viewer_id
            AND f1.followed_id = target.created_by
        )
      )
      OR (
        viewer_id IS NOT NULL
        AND target.visibility = 'group'
        AND EXISTS (
          SELECT 1 FROM group_memberships m
          WHERE m.group_id = target.visibility_group_id
            AND m.user_id = viewer_id
        )
      );
  $$;

CREATE FUNCTION node_visible_to(target nodes, viewer_id UUID) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    WITH RECURSIVE chain(id, path) AS (
      SELECT target.id, ARRAY[target.id]
      UNION ALL
      SELECT parent.id, chain.path || parent.id
      FROM chain
      JOIN nodes child ON child.id = chain.id
      JOIN nodes parent ON parent.id = child.parent_node_id
      WHERE child.parent_node_id IS NOT NULL
        AND NOT parent.id = ANY(chain.path)
    )
    SELECT COALESCE(bool_and(node_local_visible_to(n.*, viewer_id)), FALSE)
    FROM chain
    JOIN nodes n ON n.id = chain.id;
  $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Best-effort rollback. The list tables and their data are gone, so we
-- recreate empty shells matching the original schema. Nodes that were
-- rewritten to 'private' above stay private — there's no way to know
-- which list they used to belong to.

DROP FUNCTION IF EXISTS node_visible_to(nodes, UUID);
DROP FUNCTION IF EXISTS node_local_visible_to(nodes, UUID);

ALTER TABLE nodes DROP CONSTRAINT IF EXISTS visibility_target_consistency;

CREATE TABLE audience_lists (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name        TEXT NOT NULL,
  is_trusted  BOOLEAN NOT NULL DEFAULT FALSE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (owner_id, name)
);
CREATE UNIQUE INDEX audience_lists_one_trusted_per_owner
  ON audience_lists(owner_id) WHERE is_trusted;

CREATE TABLE audience_list_members (
  list_id        UUID NOT NULL REFERENCES audience_lists(id) ON DELETE CASCADE,
  member_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  added_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (list_id, member_user_id)
);

ALTER TYPE visibility_kind RENAME TO visibility_kind_old;
CREATE TYPE visibility_kind AS ENUM ('public', 'connections', 'list', 'group', 'private');

ALTER TABLE nodes ALTER COLUMN visibility DROP DEFAULT;
ALTER TABLE nodes
  ALTER COLUMN visibility TYPE visibility_kind
  USING visibility::text::visibility_kind;
ALTER TABLE nodes ALTER COLUMN visibility SET DEFAULT 'public';

ALTER TABLE users ALTER COLUMN default_node_visibility DROP DEFAULT;
ALTER TABLE users
  ALTER COLUMN default_node_visibility TYPE visibility_kind
  USING default_node_visibility::text::visibility_kind;
ALTER TABLE users ALTER COLUMN default_node_visibility SET DEFAULT 'public';

DROP TYPE visibility_kind_old;

ALTER TABLE nodes
  ADD COLUMN visibility_list_id UUID REFERENCES audience_lists(id) ON DELETE RESTRICT;
ALTER TABLE users
  ADD COLUMN default_audience_list_id UUID REFERENCES audience_lists(id) ON DELETE SET NULL;

ALTER TABLE nodes ADD CONSTRAINT visibility_target_consistency CHECK (
  (
    visibility = 'list'
    AND visibility_list_id IS NOT NULL
    AND visibility_group_id IS NULL
  )
  OR (
    visibility = 'group'
    AND visibility_group_id IS NOT NULL
    AND visibility_list_id IS NULL
  )
  OR (
    visibility NOT IN ('list', 'group')
    AND visibility_list_id IS NULL
    AND visibility_group_id IS NULL
  )
);

CREATE FUNCTION node_local_visible_to(target nodes, viewer_id UUID) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    SELECT
      target.visibility = 'public'
      OR (viewer_id IS NOT NULL AND target.created_by = viewer_id)
      OR (
        viewer_id IS NOT NULL
        AND target.visibility = 'connections'
        AND EXISTS (
          SELECT 1 FROM follows f1
          JOIN follows f2
            ON f2.follower_id = f1.followed_id
           AND f2.followed_id = f1.follower_id
          WHERE f1.follower_id = viewer_id
            AND f1.followed_id = target.created_by
        )
      )
      OR (
        viewer_id IS NOT NULL
        AND target.visibility = 'list'
        AND EXISTS (
          SELECT 1 FROM audience_list_members m
          WHERE m.list_id = target.visibility_list_id
            AND m.member_user_id = viewer_id
        )
      )
      OR (
        viewer_id IS NOT NULL
        AND target.visibility = 'group'
        AND EXISTS (
          SELECT 1 FROM group_memberships m
          WHERE m.group_id = target.visibility_group_id
            AND m.user_id = viewer_id
        )
      );
  $$;

CREATE FUNCTION node_visible_to(target nodes, viewer_id UUID) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    WITH RECURSIVE chain(id, path) AS (
      SELECT target.id, ARRAY[target.id]
      UNION ALL
      SELECT parent.id, chain.path || parent.id
      FROM chain
      JOIN nodes child ON child.id = chain.id
      JOIN nodes parent ON parent.id = child.parent_node_id
      WHERE child.parent_node_id IS NOT NULL
        AND NOT parent.id = ANY(chain.path)
    )
    SELECT COALESCE(bool_and(node_local_visible_to(n.*, viewer_id)), FALSE)
    FROM chain
    JOIN nodes n ON n.id = chain.id;
  $$;

-- +goose StatementEnd
