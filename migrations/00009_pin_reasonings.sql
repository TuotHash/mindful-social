-- +goose Up
-- +goose StatementBegin

-- A pin can now reference multiple reasoning nodes that explain the user's
-- stance. Previously this was a single nullable column on user_node_pins;
-- the junction table lets a pin attach any number of reasonings. Reasoning
-- nodes don't have to be authored by the pinning user — any reasoning the
-- user is permitted to see can be linked.
CREATE TABLE pin_reasonings (
  pin_id       UUID NOT NULL REFERENCES user_node_pins(id) ON DELETE CASCADE,
  reasoning_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (pin_id, reasoning_id)
);

CREATE INDEX pin_reasonings_pin_idx ON pin_reasonings (pin_id);

-- Carry over existing single-reasoning pins.
INSERT INTO pin_reasonings (pin_id, reasoning_id)
SELECT id, reasoning_id
FROM user_node_pins
WHERE reasoning_id IS NOT NULL;

ALTER TABLE user_node_pins DROP COLUMN reasoning_id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE user_node_pins
  ADD COLUMN reasoning_id UUID REFERENCES nodes(id) ON DELETE SET NULL;

-- Take the first reasoning per pin if any exist. Multi-reasoning data is
-- collapsed on downgrade — only the earliest one survives.
UPDATE user_node_pins p
SET reasoning_id = (
    SELECT pr.reasoning_id
    FROM pin_reasonings pr
    WHERE pr.pin_id = p.id
    ORDER BY pr.created_at ASC
    LIMIT 1
);

DROP TABLE pin_reasonings;

-- +goose StatementEnd
