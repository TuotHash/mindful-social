-- +goose Up
-- +goose StatementBegin

CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Users authenticate with email + password. Username is a public handle.
CREATE TABLE users (
  id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  username      TEXT NOT NULL UNIQUE,
  email         TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- A node is anything in the graph: a topic, a stance, a piece of reasoning,
-- or an external fact/source. Type drives navigation; tags add free-form
-- grouping (domain, nature, etc.) without bloating the type system.
CREATE TYPE node_type AS ENUM ('topic', 'view', 'reasoning', 'fact');

CREATE TABLE nodes (
  id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  type        node_type NOT NULL,
  title       TEXT NOT NULL,
  body        TEXT NOT NULL DEFAULT '',
  source_url  TEXT,
  created_by  UUID NOT NULL REFERENCES users(id),
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_nodes_type       ON nodes(type);
CREATE INDEX idx_nodes_created_at ON nodes(created_at DESC);

-- Edges are typed and directed. (from_node, to_node, kind) is unique so the
-- same relationship can't be added twice. Self-edges are forbidden.
CREATE TYPE edge_kind AS ENUM ('supports', 'opposes', 'refines', 'cites', 'relates_to');

CREATE TABLE edges (
  id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_node  UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  to_node    UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  kind       edge_kind NOT NULL,
  created_by UUID NOT NULL REFERENCES users(id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  CONSTRAINT no_self_edge CHECK (from_node <> to_node),
  UNIQUE (from_node, to_node, kind)
);

CREATE INDEX idx_edges_from ON edges(from_node);
CREATE INDEX idx_edges_to   ON edges(to_node);
CREATE INDEX idx_edges_kind ON edges(kind);

-- Tags are free-form, shared across the system. node_tags is the join table.
CREATE TABLE tags (
  id   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name TEXT NOT NULL UNIQUE
);

CREATE TABLE node_tags (
  node_id UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  tag_id  UUID NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
  PRIMARY KEY (node_id, tag_id)
);

CREATE INDEX idx_node_tags_tag ON node_tags(tag_id);

-- A commitment pins a user to a view. The optional reasoning_id points at
-- a Reasoning node the user wrote when committing. UNIQUE (user_id, view_id)
-- means a user holds at most one commitment per view.
CREATE TABLE commitments (
  id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id      UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  view_id      UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
  reasoning_id UUID REFERENCES nodes(id) ON DELETE SET NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_id, view_id)
);

CREATE INDEX idx_commitments_user ON commitments(user_id);
CREATE INDEX idx_commitments_view ON commitments(view_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS commitments;
DROP TABLE IF EXISTS node_tags;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS edges;
DROP TYPE  IF EXISTS edge_kind;
DROP TABLE IF EXISTS nodes;
DROP TYPE  IF EXISTS node_type;
DROP TABLE IF EXISTS users;

-- +goose StatementEnd
