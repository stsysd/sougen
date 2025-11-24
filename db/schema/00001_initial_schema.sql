-- +goose Up
-- Projects table
CREATE TABLE projects (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

-- Records table
CREATE TABLE records (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL,
	value INTEGER NOT NULL,
	timestamp TEXT NOT NULL,
	FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Tags table
CREATE TABLE tags (
	record_id INTEGER NOT NULL,
	tag TEXT NOT NULL,
	PRIMARY KEY (record_id, tag),
	FOREIGN KEY (record_id) REFERENCES records(id) ON DELETE CASCADE
);

-- Indexes
CREATE INDEX idx_projects_updated_at ON projects(updated_at);
CREATE INDEX idx_projects_name ON projects(name);
CREATE INDEX idx_records_project_id_timestamp ON records(project_id, timestamp);
CREATE INDEX idx_tags_record_id ON tags(record_id);
CREATE INDEX idx_tags_tag ON tags(tag);

-- +goose Down
DROP INDEX IF EXISTS idx_tags_tag;
DROP INDEX IF EXISTS idx_tags_record_id;
DROP INDEX IF EXISTS idx_records_project_id_timestamp;
DROP INDEX IF EXISTS idx_projects_name;
DROP INDEX IF EXISTS idx_projects_updated_at;

DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS records;
DROP TABLE IF EXISTS projects;
