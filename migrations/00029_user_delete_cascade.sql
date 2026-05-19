-- +goose Up
-- +goose StatementBegin

-- nodes.created_by and edges.created_by were left at the default RESTRICT
-- when the schema was first laid down. That blocks user deletion: any user
-- who has authored a node or edge can't be removed without first deleting
-- all of their contributions by hand. The admin "delete user" action now
-- cascades the same way the rest of the user-owned tables already do
-- (follows, pins, comments, group memberships, etc.), so the constraint
-- here just needs to allow it.

ALTER TABLE nodes
  DROP CONSTRAINT nodes_created_by_fkey,
  ADD CONSTRAINT nodes_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE;

ALTER TABLE edges
  DROP CONSTRAINT edges_created_by_fkey,
  ADD CONSTRAINT edges_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id) ON DELETE CASCADE;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE nodes
  DROP CONSTRAINT nodes_created_by_fkey,
  ADD CONSTRAINT nodes_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id);

ALTER TABLE edges
  DROP CONSTRAINT edges_created_by_fkey,
  ADD CONSTRAINT edges_created_by_fkey
    FOREIGN KEY (created_by) REFERENCES users(id);

-- +goose StatementEnd
