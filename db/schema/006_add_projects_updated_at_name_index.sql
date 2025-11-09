-- Add composite index for cursor-based pagination on projects
-- This index supports efficient queries with ORDER BY updated_at DESC, name
CREATE INDEX idx_projects_updated_at_name
ON projects(updated_at DESC, name);
