-- +goose Up
-- +goose StatementBegin

-- Generalize per-user reactions to nodes. The previous `commitments` table
-- only modelled "supports a view"; this one adds opposing and bare featuring,
-- and works for any node type. The handler enforces that supports/opposes
-- only apply to view-typed nodes; featured can pin any node ("important to
-- me" on a topic, "I find this compelling" on a reasoning, etc.).
CREATE TYPE pin_kind AS ENUM ('supports', 'opposes', 'featured');

CREATE TABLE user_node_pins (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  node_id      UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  kind         pin_kind NOT NULL,
  reasoning_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, node_id)
);

-- Carry over existing commitment rows as 'supports' pins. View_id maps to
-- node_id; reasoning_id and created_at carry over verbatim.
INSERT INTO user_node_pins (user_id, node_id, kind, reasoning_id, created_at)
SELECT user_id, view_id, 'supports'::pin_kind, reasoning_id, created_at
FROM commitments;

DROP TABLE commitments;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE commitments (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  view_id      UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  reasoning_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, view_id)
);

INSERT INTO commitments (user_id, view_id, reasoning_id, created_at)
SELECT user_id, node_id, reasoning_id, created_at
FROM user_node_pins
WHERE kind = 'supports';

DROP TABLE user_node_pins;
DROP TYPE pin_kind;

-- +goose StatementEnd
