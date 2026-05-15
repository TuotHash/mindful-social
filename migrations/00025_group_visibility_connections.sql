-- +goose Up
-- +goose StatementBegin

-- Reshape group_visibility_kind from the original (public, invite, closed)
-- to the user-facing model (public, connections, private). The new vocabulary
-- mirrors how node_visible_to() already talks about audiences:
--
--   public      — anyone can see the group (and self-join, see handler).
--   connections — visible only to the owner's mutual followers (i.e. their
--                 connections) and to existing members; invite-required join.
--   private     — visible only to members; invite-required join.
--
-- Existing 'invite' rows become 'connections' (narrower visibility, but
-- still visible to the owner's social graph) and existing 'closed' rows
-- become 'private'. Postgres can't drop enum values directly, so we use
-- the same type-swap dance other migrations in this repo use.

ALTER TABLE groups ALTER COLUMN visibility DROP DEFAULT;

ALTER TYPE group_visibility_kind RENAME TO group_visibility_kind_old;
CREATE TYPE group_visibility_kind AS ENUM ('public', 'connections', 'private');

ALTER TABLE groups
  ALTER COLUMN visibility TYPE group_visibility_kind
  USING (
    CASE visibility::text
      WHEN 'invite' THEN 'connections'
      WHEN 'closed' THEN 'private'
      ELSE visibility::text
    END
  )::group_visibility_kind;

ALTER TABLE groups ALTER COLUMN visibility SET DEFAULT 'private';

DROP TYPE group_visibility_kind_old;

-- Trigram index on group names so /search can match groups the same way
-- it matches node titles. pg_trgm is already installed by 00005.
CREATE INDEX IF NOT EXISTS groups_name_trgm_idx
  ON groups USING GIN (name gin_trgm_ops);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS groups_name_trgm_idx;

ALTER TABLE groups ALTER COLUMN visibility DROP DEFAULT;

ALTER TYPE group_visibility_kind RENAME TO group_visibility_kind_old;
CREATE TYPE group_visibility_kind AS ENUM ('public', 'invite', 'closed');

ALTER TABLE groups
  ALTER COLUMN visibility TYPE group_visibility_kind
  USING (
    CASE visibility::text
      WHEN 'connections' THEN 'invite'
      WHEN 'private' THEN 'closed'
      ELSE visibility::text
    END
  )::group_visibility_kind;

ALTER TABLE groups ALTER COLUMN visibility SET DEFAULT 'invite';

DROP TYPE group_visibility_kind_old;

-- +goose StatementEnd
