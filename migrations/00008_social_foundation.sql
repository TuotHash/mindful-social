-- +goose Up
-- +goose StatementBegin

-- Social foundation: follows (one-directional), audience lists, per-node
-- visibility. Mutual follows are computed from the follows table — there is
-- no separate "friends" or "connections" record.

-- One row per (follower, followed). Self-follow is forbidden.
CREATE TABLE follows (
  follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followed_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (follower_id, followed_id),
  CONSTRAINT no_self_follow CHECK (follower_id <> followed_id)
);

CREATE INDEX idx_follows_followed ON follows(followed_id);

-- A user-curated list of other users. Every user has exactly one built-in
-- "Trusted" list (is_trusted = TRUE) plus any number of custom lists.
CREATE TABLE audience_lists (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  owner_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  is_trusted BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (owner_id, name)
);

CREATE INDEX idx_audience_lists_owner ON audience_lists(owner_id);
-- One trusted list per user, enforced at the index level.
CREATE UNIQUE INDEX idx_audience_lists_trusted
  ON audience_lists(owner_id) WHERE is_trusted;

CREATE TABLE audience_list_members (
  list_id        UUID NOT NULL REFERENCES audience_lists(id) ON DELETE CASCADE,
  member_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  added_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (list_id, member_user_id)
);

CREATE INDEX idx_audience_list_members_user ON audience_list_members(member_user_id);

-- Visibility levels in increasing order of restriction:
--   public      — anyone, including logged-out users
--   connections — mutual followers only
--   list        — members of a specific audience list (visibility_list_id)
--   private     — author only
CREATE TYPE visibility_kind AS ENUM ('public', 'connections', 'list', 'private');

ALTER TABLE nodes
  ADD COLUMN visibility visibility_kind NOT NULL DEFAULT 'public',
  ADD COLUMN visibility_list_id UUID REFERENCES audience_lists(id) ON DELETE RESTRICT;

-- visibility_list_id is required iff visibility = 'list'.
ALTER TABLE nodes ADD CONSTRAINT visibility_list_consistency CHECK (
  (visibility = 'list' AND visibility_list_id IS NOT NULL)
  OR (visibility <> 'list' AND visibility_list_id IS NULL)
);

CREATE INDEX idx_nodes_visibility ON nodes(visibility);

-- Centralised visibility predicate. Used by every listing query that should
-- respect node visibility, plus the node-detail handler. STABLE so it can be
-- inlined and indexed-around.
--   viewer_id NULL  → only public nodes are visible (logged-out users).
--   viewer_id set   → public + own + connections (when visibility='connections'
--                     and there is a mutual follow) + list (when visibility='list'
--                     and viewer is a member of visibility_list_id).
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

-- Backfill: create a Trusted list for every existing user so the visibility
-- selector always has it available.
INSERT INTO audience_lists (owner_id, name, is_trusted)
SELECT id, 'Trusted', TRUE FROM users;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP FUNCTION IF EXISTS node_visible_to(nodes, UUID);
DROP INDEX IF EXISTS idx_nodes_visibility;
ALTER TABLE nodes DROP CONSTRAINT IF EXISTS visibility_list_consistency;
ALTER TABLE nodes DROP COLUMN IF EXISTS visibility_list_id;
ALTER TABLE nodes DROP COLUMN IF EXISTS visibility;
DROP TYPE IF EXISTS visibility_kind;
DROP TABLE IF EXISTS audience_list_members;
DROP TABLE IF EXISTS audience_lists;
DROP TABLE IF EXISTS follows;

-- +goose StatementEnd
