-- name: CreateRecord :exec
INSERT INTO records (id, project, value, timestamp)
VALUES (?, ?, ?, ?);

-- name: CreateRecordTag :exec
INSERT INTO tags (record_id, tag)
VALUES (?, ?);

-- name: GetRecord :one
SELECT id, project, value, timestamp
FROM records
WHERE id = ?;

-- name: GetRecordTags :many
SELECT tag
FROM tags
WHERE record_id = ?;

-- name: DeleteRecord :execresult
DELETE FROM records WHERE id = ?;

-- name: ListRecords :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
-- Optimized query to avoid n+1 problem by using GROUP_CONCAT for tags
-- Cursor-based pagination: uses cursor_timestamp and cursor_id for pagination
SELECT
    r.id,
    r.project,
    r.value,
    r.timestamp,
    COALESCE(GROUP_CONCAT(t.tag, ' '), '') as tags
FROM records r
LEFT JOIN tags t ON r.id = t.record_id
WHERE r.timestamp BETWEEN ? AND ? AND r.project = ?
  AND (? IS NULL OR r.timestamp < ? OR (r.timestamp = ? AND r.id > ?))
GROUP BY r.id, r.project, r.value, r.timestamp
ORDER BY r.timestamp DESC, r.id
LIMIT ?;

-- name: ListRecordsWithTags :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
-- Returns records that have all of the specified tags
-- Optimized query to avoid n+1 problem by using GROUP_CONCAT for all tags
-- Cursor-based pagination: uses cursor_timestamp and cursor_id for pagination
SELECT
    r.id,
    r.project,
    r.value,
    r.timestamp,
    COALESCE(GROUP_CONCAT(t.tag, ' '), '') as all_tags
FROM records r
INNER JOIN tags t ON r.id = t.record_id
WHERE r.timestamp BETWEEN ? AND ? AND r.project = ?
  AND t.tag IN (sqlc.slice(tags))
  AND (? IS NULL OR r.timestamp < ? OR (r.timestamp = ? AND r.id > ?))
GROUP BY r.id, r.project, r.value, r.timestamp
HAVING COUNT(DISTINCT t.tag) = CAST(? AS INTEGER)
ORDER BY r.timestamp DESC, r.id
LIMIT ?;


-- name: DeleteProject :exec
DELETE FROM records WHERE project = ?;

-- name: DeleteRecordsUntil :execresult
DELETE FROM records WHERE timestamp < ?;

-- name: UpdateRecord :execresult
UPDATE records SET project = ?, value = ?, timestamp = ?
WHERE id = ?;

-- name: DeleteRecordTags :exec
DELETE FROM tags WHERE record_id = ?;

-- name: DeleteRecordsUntilByProject :execresult
DELETE FROM records WHERE project = ? AND timestamp < ?;

-- name: CreateProject :exec
INSERT INTO projects (name, description, created_at, updated_at)
VALUES (?, ?, ?, ?);

-- name: GetProject :one
SELECT name, description, created_at, updated_at
FROM projects
WHERE name = ?;

-- name: UpdateProject :execresult
UPDATE projects SET description = ?, updated_at = ?
WHERE name = ?;

-- name: DeleteProjectEntity :exec
DELETE FROM projects WHERE name = ?;

-- name: ListProjects :many
-- Cursor-based pagination: uses cursor_updated_at and cursor_name for pagination
SELECT name, description, created_at, updated_at
FROM projects
WHERE ? IS NULL OR updated_at < ? OR (updated_at = ? AND name > ?)
ORDER BY updated_at DESC, name
LIMIT ?;

-- name: GetProjectTags :many
SELECT DISTINCT tag
FROM tags t
JOIN records r ON t.record_id = r.id
WHERE r.project = ?
ORDER BY tag;
