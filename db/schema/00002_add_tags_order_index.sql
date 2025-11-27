-- +goose Up
-- Add order_index column to tags table to preserve tag insertion order
ALTER TABLE tags ADD COLUMN order_index INTEGER NOT NULL DEFAULT 0;

-- +goose Down
-- Remove order_index column from tags table
ALTER TABLE tags DROP COLUMN order_index;
