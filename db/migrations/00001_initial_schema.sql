-- +goose Up
-- Projects table
CREATE TABLE IF NOT EXISTS projects (
	name TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

-- Records table
CREATE TABLE IF NOT EXISTS records (
	id TEXT PRIMARY KEY,
	project TEXT NOT NULL,
	value INTEGER NOT NULL,
	timestamp TEXT NOT NULL
);

-- Tags table
CREATE TABLE IF NOT EXISTS tags (
	record_id TEXT NOT NULL,
	tag TEXT NOT NULL,
	PRIMARY KEY (record_id, tag)
);

-- Indexes
CREATE INDEX IF NOT EXISTS idx_records_project_timestamp
ON records(project, timestamp);

CREATE INDEX IF NOT EXISTS idx_tags_record_id ON tags(record_id);
CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);
CREATE INDEX IF NOT EXISTS idx_projects_updated_at ON projects(updated_at);

-- +goose Down
DROP INDEX IF EXISTS idx_projects_updated_at;
DROP INDEX IF EXISTS idx_tags_tag;
DROP INDEX IF EXISTS idx_tags_record_id;
DROP INDEX IF EXISTS idx_records_project_timestamp;

DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS records;
DROP TABLE IF EXISTS projects;
