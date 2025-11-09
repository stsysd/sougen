-- Add composite index for cursor-based pagination
-- This index supports efficient queries with ORDER BY timestamp DESC, id
CREATE INDEX idx_records_project_timestamp_id
ON records(project, timestamp DESC, id);
