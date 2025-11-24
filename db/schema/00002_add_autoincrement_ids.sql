-- +goose Up
-- Step 1: Migrate projects table to add auto-increment id column
CREATE TABLE projects_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

INSERT INTO projects_new (name, description, created_at, updated_at)
SELECT name, description, created_at, updated_at FROM projects;

DROP TABLE projects;
ALTER TABLE projects_new RENAME TO projects;

CREATE INDEX idx_projects_updated_at ON projects(updated_at);
CREATE INDEX idx_projects_name ON projects(name);

-- Step 2: Migrate records table to use auto-increment id and project_id (foreign key)
-- Create new table with both old_id (for mapping) and new auto-increment id
-- Change project from TEXT (name) to INTEGER (project_id)
CREATE TABLE records_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	old_id TEXT NOT NULL,
	project_id INTEGER NOT NULL,
	value INTEGER NOT NULL,
	timestamp TEXT NOT NULL,
	FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

-- Copy data from old table, mapping project name to project_id
INSERT INTO records_new (old_id, project_id, value, timestamp)
SELECT r.id, p.id, r.value, r.timestamp
FROM records r
JOIN projects p ON r.project = p.name;

-- Step 3: Migrate tags table using old_id for mapping
CREATE TABLE tags_new (
	record_id INTEGER NOT NULL,
	tag TEXT NOT NULL,
	PRIMARY KEY (record_id, tag),
	FOREIGN KEY (record_id) REFERENCES records(id) ON DELETE CASCADE
);

-- Migrate tags data using old_id in records_new
INSERT INTO tags_new (record_id, tag)
SELECT rn.id, t.tag
FROM tags t
JOIN records_new rn ON t.record_id = rn.old_id;

DROP TABLE tags;
ALTER TABLE tags_new RENAME TO tags;

CREATE INDEX idx_tags_record_id ON tags(record_id);
CREATE INDEX idx_tags_tag ON tags(tag);

-- Step 4: Remove old_id column from records table
-- SQLite doesn't support DROP COLUMN directly, so recreate the table
CREATE TABLE records_final (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL,
	value INTEGER NOT NULL,
	timestamp TEXT NOT NULL,
	FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE
);

INSERT INTO records_final (id, project_id, value, timestamp)
SELECT id, project_id, value, timestamp FROM records_new;

DROP TABLE records;
ALTER TABLE records_final RENAME TO records;

CREATE INDEX idx_records_project_id_timestamp ON records(project_id, timestamp);

-- +goose Down
-- Note: Rollback will generate new UUIDs for records (original UUIDs cannot be restored)
-- However, all data (project, value, timestamp) will be preserved

-- Restore projects table without id column
CREATE TABLE projects_old (
	name TEXT PRIMARY KEY,
	description TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);

INSERT INTO projects_old (name, description, created_at, updated_at)
SELECT name, description, created_at, updated_at FROM projects;

DROP TABLE projects;
ALTER TABLE projects_old RENAME TO projects;

CREATE INDEX idx_projects_updated_at ON projects(updated_at);

-- Create intermediate table to map new UUIDs to integer IDs for tags migration
-- Also restore project_id to project name
CREATE TABLE records_with_new_uuid (
	new_uuid TEXT PRIMARY KEY,
	old_int_id INTEGER NOT NULL,
	project TEXT NOT NULL,
	value INTEGER NOT NULL,
	timestamp TEXT NOT NULL
);

-- Generate new UUID-like strings (32 hex characters) for each record
-- Convert project_id back to project name
INSERT INTO records_with_new_uuid (new_uuid, old_int_id, project, value, timestamp)
SELECT lower(hex(randomblob(16))), r.id, p.name, r.value, r.timestamp
FROM records r
JOIN projects p ON r.project_id = p.id;

-- Restore records with TEXT id and TEXT project name
CREATE TABLE records_old (
	id TEXT PRIMARY KEY,
	project TEXT NOT NULL,
	value INTEGER NOT NULL,
	timestamp TEXT NOT NULL
);

INSERT INTO records_old (id, project, value, timestamp)
SELECT new_uuid, project, value, timestamp FROM records_with_new_uuid;

DROP TABLE records;
ALTER TABLE records_old RENAME TO records;

CREATE INDEX idx_records_project_timestamp ON records(project, timestamp);

-- Restore tags table with new UUID references
CREATE TABLE tags_old (
	record_id TEXT NOT NULL,
	tag TEXT NOT NULL,
	PRIMARY KEY (record_id, tag)
);

-- Migrate tags using the UUID mapping
INSERT INTO tags_old (record_id, tag)
SELECT rwn.new_uuid, t.tag
FROM tags t
JOIN records_with_new_uuid rwn ON t.record_id = rwn.old_int_id;

DROP TABLE tags;
ALTER TABLE tags_old RENAME TO tags;

CREATE INDEX idx_tags_record_id ON tags(record_id);
CREATE INDEX idx_tags_tag ON tags(tag);

-- Clean up mapping table
DROP TABLE records_with_new_uuid;
