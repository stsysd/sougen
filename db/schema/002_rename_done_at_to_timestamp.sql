-- Rename done_at column to timestamp
ALTER TABLE records RENAME COLUMN done_at TO timestamp;

-- Drop old index
DROP INDEX idx_records_project_done_at;

-- Create new index with updated column name
CREATE INDEX idx_records_project_timestamp 
ON records(project, timestamp)
