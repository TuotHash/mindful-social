-- +goose Up
-- +goose StatementBegin

-- Admin-assigned roles. Distinct from the future trust-tier system (which
-- is earned through participation): user_role is granted manually.
--   user      — default; permissions follow per-node policies.
--   moderator — bypasses per-node edit/link policies and can delete any
--               node, for community curation.
--   admin     — moderator powers plus user-role management and any
--               future site-administration UIs.
CREATE TYPE user_role AS ENUM ('user', 'moderator', 'admin');

ALTER TABLE users ADD COLUMN role user_role NOT NULL DEFAULT 'user';

CREATE INDEX idx_users_role ON users(role) WHERE role <> 'user';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_users_role;
ALTER TABLE users DROP COLUMN IF EXISTS role;
DROP TYPE IF EXISTS user_role;

-- +goose StatementEnd
