CREATE TABLE records (
    id TEXT PRIMARY KEY,
    project TEXT NOT NULL,
    value INTEGER NOT NULL,
    done_at TEXT NOT NULL
);

CREATE INDEX idx_records_project_done_at 
ON records(project, done_at);