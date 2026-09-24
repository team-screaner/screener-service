-- +goose Up
ALTER TABLE evidence ADD COLUMN import_hash text;
-- +goose Down
ALTER TABLE evidence DROP COLUMN import_hash;
