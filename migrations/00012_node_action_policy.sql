-- +goose Up
-- +goose StatementBegin

-- Per-node action policies. visibility already governs *who can see* a
-- node; edit_policy and link_policy govern *who can act on* one.
--   author      — only the node's author
--   connections — author + any mutual follower of the author
--   public      — anyone logged in (still requires auth)
CREATE TYPE node_action_policy AS ENUM ('author', 'connections', 'public');

-- Defaults: edit is author-only (safe default — wiki-open editing is an
-- opt-in), link is public so the argument graph stays freely connectable.
ALTER TABLE nodes
  ADD COLUMN edit_policy node_action_policy NOT NULL DEFAULT 'author',
  ADD COLUMN link_policy node_action_policy NOT NULL DEFAULT 'public';

-- Centralised policy predicate. Returns TRUE when `viewer` is allowed to
-- take an action gated by `policy` on a node authored by `author`. The
-- "connections" branch uses the same mutual-follow check as
-- node_visible_to() so the two concepts stay aligned.
CREATE FUNCTION node_action_allowed(
    policy node_action_policy,
    author UUID,
    viewer UUID
) RETURNS BOOLEAN
  LANGUAGE sql STABLE AS $$
    SELECT viewer IS NOT NULL AND (
      viewer = author
      OR policy = 'public'
      OR (
        policy = 'connections'
        AND EXISTS (
          SELECT 1 FROM follows f1
          JOIN follows f2
            ON f2.follower_id = f1.followed_id
           AND f2.followed_id = f1.follower_id
          WHERE f1.follower_id = viewer
            AND f1.followed_id = author
        )
      )
    );
  $$;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP FUNCTION IF EXISTS node_action_allowed(node_action_policy, UUID, UUID);
ALTER TABLE nodes
  DROP COLUMN IF EXISTS edit_policy,
  DROP COLUMN IF EXISTS link_policy;
DROP TYPE IF EXISTS node_action_policy;

-- +goose StatementEnd
