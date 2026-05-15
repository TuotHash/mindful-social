-- +goose Up
-- +goose StatementBegin

-- Adds an `editor` tier between admin and member. Editors can edit and
-- delete any node hosted in the group (content moderation), but they
-- cannot change membership or settings — those stay with admin / owner.
-- BEFORE 'member' places editor in privilege order so a future
-- enum_range-driven check (or a future enum value) keeps the hierarchy
-- owner > admin > editor > member.
ALTER TYPE group_member_role ADD VALUE IF NOT EXISTS 'editor' BEFORE 'member';

-- member_visibility gates who can see the group's member list. Stored as
-- the role enum so the threshold uses the same vocabulary as the roles:
-- 'owner' = owner only (an audience-list-equivalent privacy mode),
-- 'admin' = admins+, 'editor' = editors+, 'member' = all members (the
-- default). Non-members never see the member list regardless of this
-- setting.
ALTER TABLE groups
  ADD COLUMN member_visibility group_member_role NOT NULL DEFAULT 'member';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE groups DROP COLUMN IF EXISTS member_visibility;

-- Postgres has no DROP VALUE for enums; removing 'editor' cleanly would
-- require a type swap with a USING expression remapping editor rows. We
-- skip that here — the down migration is best-effort for an in-dev
-- feature and the unused enum value is harmless.

-- +goose StatementEnd
