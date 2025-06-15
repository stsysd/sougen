-- Create projects table to store project entities
CREATE TABLE projects (
    name TEXT PRIMARY KEY,
    description TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

-- Create index for efficient queries
CREATE INDEX idx_projects_updated_at ON projects(updated_at);