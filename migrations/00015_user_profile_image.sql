-- +goose Up
-- +goose StatementBegin

ALTER TABLE users
  ADD COLUMN profile_image_path TEXT NOT NULL DEFAULT '';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE users
  DROP COLUMN IF EXISTS profile_image_path;

-- +goose StatementEnd
