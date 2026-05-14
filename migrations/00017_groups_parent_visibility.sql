-- +goose Up
-- +goose StatementBegin

-- Groups are collaborative spaces. They are distinct from audience lists:
-- lists are private recipient sets owned by one user; groups have shared
-- membership and can host their own slice of the graph.
CREATE TYPE group_visibility_kind AS ENUM ('public', 'invite', 'closed');
CREATE TYPE group_member_role AS ENUM ('owner', 'admin', 'member');

CREATE TABLE groups (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  slug        TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL UNIQUE,
  description TEXT NOT NULL DEFAULT '',
  owner_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  visibility  group_visibility_kind NOT NULL DEFAULT 'invite',
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_groups_owner ON groups(owner_id);
CREATE INDEX idx_groups_visibility ON groups(visibility);

CREATE TABLE group_memberships (
  group_id  UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role      group_member_role NOT NULL DEFAULT 'member',
  joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (group_id, user_id)
);

CREATE INDEX idx_group_memberships_user ON group_memberships(user_id);

CREATE TABLE group_invites (
  id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  group_id        UUID NOT NULL REFERENCES groups(id) ON DELETE CASCADE,
  invited_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  invited_by      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  accepted_at     TIMESTAMPTZ,
  UNIQUE (group_id, invited_user_id)
);

CREATE INDEX idx_group_invites_invited_user ON group_invites(invited_user_id);

-- node_visible_to(nodes, uuid) depends on the nodes composite type, so drop
-- it before changing columns that participate in that type.
DROP FUNCTION IF EXISTS node_visible_to(nodes, UUID);

ALTER TABLE nodes DROP CONSTRAINT IF EXISTS visibility_list_consistency;

-- Extend visibility_kind with 'group'. Postgres enum values cannot be
-- removed directly, so use the same type-swap pattern used by node_type
-- migrations.
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
  ADD COLUMN parent_node_id UUID REFERENCES nodes(id) ON DELETE RESTRICT,
  ADD COLUMN group_id UUID REFERENCES groups(id) ON DELETE SET NULL,
  ADD COLUMN visibility_group_id UUID REFERENCES groups(id) ON DELETE RESTRICT;

ALTER TABLE nodes ADD CONSTRAINT no_self_parent CHECK (
  parent_node_id IS NULL OR parent_node_id <> id
);

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

CREATE INDEX idx_nodes_parent_node ON nodes(parent_node_id);
CREATE INDEX idx_nodes_group ON nodes(group_id);
CREATE INDEX idx_nodes_visibility_group ON nodes(visibility_group_id);

-- Backfill canonical parents from existing relationship edges where the
-- relationship matches the app's hierarchy rules.
WITH candidate_parents AS (
  SELECT DISTINCT ON (e.from_node)
    e.from_node AS child_id,
    e.to_node AS parent_id
  FROM edges e
  JOIN nodes child ON child.id = e.from_node
  JOIN nodes parent ON parent.id = e.to_node
  WHERE e.kind = 'relates_to'
    AND (
      (child.type = 'view' AND parent.type = 'topic')
      OR (child.type = 'topic' AND parent.type = 'topic')
      OR (child.type = 'finding' AND parent.type IN ('view', 'finding'))
    )
  ORDER BY e.from_node, e.created_at ASC
)
UPDATE nodes n
SET parent_node_id = c.parent_id
FROM candidate_parents c
WHERE n.id = c.child_id
  AND n.parent_node_id IS NULL;

-- Local visibility checks only this node's own restriction. The public branch
-- means "no additional restriction"; parent restrictions are applied by the
-- recursive node_visible_to() wrapper below.
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

-- Effective visibility intersects this node's local restriction with every
-- ancestor restriction. That makes child 'public' behave like "inherit" when
-- the parent is private, connections-only, list-only, or group-only.
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

DROP FUNCTION IF EXISTS node_visible_to(nodes, UUID);
DROP FUNCTION IF EXISTS node_local_visible_to(nodes, UUID);

DROP INDEX IF EXISTS idx_nodes_visibility_group;
DROP INDEX IF EXISTS idx_nodes_group;
DROP INDEX IF EXISTS idx_nodes_parent_node;

ALTER TABLE nodes DROP CONSTRAINT IF EXISTS visibility_target_consistency;
ALTER TABLE nodes DROP CONSTRAINT IF EXISTS no_self_parent;
ALTER TABLE nodes
  DROP COLUMN IF EXISTS visibility_group_id,
  DROP COLUMN IF EXISTS group_id,
  DROP COLUMN IF EXISTS parent_node_id;

ALTER TYPE visibility_kind RENAME TO visibility_kind_old;
CREATE TYPE visibility_kind AS ENUM ('public', 'connections', 'list', 'private');

ALTER TABLE nodes ALTER COLUMN visibility DROP DEFAULT;
ALTER TABLE nodes
  ALTER COLUMN visibility TYPE visibility_kind
  USING (
    CASE WHEN visibility::text = 'group' THEN 'private' ELSE visibility::text END
  )::visibility_kind;
ALTER TABLE nodes ALTER COLUMN visibility SET DEFAULT 'public';

ALTER TABLE users ALTER COLUMN default_node_visibility DROP DEFAULT;
ALTER TABLE users
  ALTER COLUMN default_node_visibility TYPE visibility_kind
  USING (
    CASE WHEN default_node_visibility::text = 'group' THEN 'private' ELSE default_node_visibility::text END
  )::visibility_kind;
ALTER TABLE users ALTER COLUMN default_node_visibility SET DEFAULT 'public';

DROP TYPE visibility_kind_old;

ALTER TABLE nodes ADD CONSTRAINT visibility_list_consistency CHECK (
  (visibility = 'list' AND visibility_list_id IS NOT NULL)
  OR (visibility <> 'list' AND visibility_list_id IS NULL)
);

DROP TABLE IF EXISTS group_invites;
DROP TABLE IF EXISTS group_memberships;
DROP TABLE IF EXISTS groups;
DROP TYPE IF EXISTS group_member_role;
DROP TYPE IF EXISTS group_visibility_kind;

CREATE FUNCTION node_visible_to(node nodes, viewer_id UUID) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    SELECT
      node.visibility = 'public'
      OR (viewer_id IS NOT NULL AND node.created_by = viewer_id)
      OR (
        viewer_id IS NOT NULL
        AND node.visibility = 'connections'
        AND EXISTS (
          SELECT 1 FROM follows f1
          JOIN follows f2
            ON f2.follower_id = f1.followed_id
           AND f2.followed_id = f1.follower_id
          WHERE f1.follower_id = viewer_id
            AND f1.followed_id = node.created_by
        )
      )
      OR (
        viewer_id IS NOT NULL
        AND node.visibility = 'list'
        AND EXISTS (
          SELECT 1 FROM audience_list_members m
          WHERE m.list_id = node.visibility_list_id
            AND m.member_user_id = viewer_id
        )
      );
  $$;

-- +goose StatementEnd
