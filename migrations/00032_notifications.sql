-- +goose Up
-- +goose StatementBegin

CREATE TABLE notifications (
  id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
  recipient_id UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  actor_id     UUID        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  kind         TEXT        NOT NULL,
  node_id      UUID                   REFERENCES nodes(id) ON DELETE CASCADE,
  read_at      TIMESTAMPTZ,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT notifications_no_self_notify CHECK (recipient_id <> actor_id)
);

CREATE INDEX notifications_recipient_created ON notifications (recipient_id, created_at DESC);
CREATE INDEX notifications_recipient_unread  ON notifications (recipient_id, read_at) WHERE read_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE notifications;

-- +goose StatementEnd
