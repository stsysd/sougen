-- +goose Up
-- Add order_index column to tags table to preserve tag insertion order
ALTER TABLE tags ADD COLUMN order_index INTEGER NOT NULL DEFAULT 0;

-- Set initial order_index based on ROWID for existing records
-- For each record_id group, assign sequential numbers starting from 0
UPDATE tags
SET order_index = (
    SELECT COUNT(*)
    FROM tags t2
    WHERE t2.record_id = tags.record_id
      AND t2.ROWID < tags.ROWID
);

-- Add unique constraint to prevent duplicate order_index within same record
CREATE UNIQUE INDEX idx_tags_record_order ON tags(record_id, order_index);

-- +goose Down
-- Remove unique index
DROP INDEX IF EXISTS idx_tags_record_order;

-- Remove order_index column from tags table
ALTER TABLE tags DROP COLUMN order_index;
