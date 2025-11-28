-- name: CreateRecord :execresult
INSERT INTO records (project_id, value, timestamp)
VALUES (?, ?, ?);

-- name: CreateRecordTag :exec
INSERT INTO tags (record_id, tag, order_index)
VALUES (?, ?, ?);

-- name: GetRecord :one
SELECT id, project_id, value, timestamp
FROM records
WHERE id = ?;

-- name: GetRecordTags :many
SELECT tag
FROM tags
WHERE record_id = ?
ORDER BY order_index;

-- name: DeleteRecord :execresult
DELETE FROM records WHERE id = ?;

-- name: ListRecords :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
-- Optimized query to avoid n+1 problem by using GROUP_CONCAT for tags
-- Cursor-based pagination: uses cursor_timestamp and cursor_id for pagination
SELECT
    r.id,
    r.project_id,
    r.value,
    r.timestamp,
    COALESCE((
        SELECT GROUP_CONCAT(tag, ' ')
        FROM (
            SELECT tag
            FROM tags
            WHERE record_id = r.id
            ORDER BY order_index
        )
    ), '') as tags
FROM records r
WHERE r.timestamp BETWEEN ? AND ? AND r.project_id = ?
  AND (? IS NULL OR r.timestamp < ? OR (r.timestamp = ? AND r.id > ?))
ORDER BY r.timestamp DESC, r.id
LIMIT ?;

-- name: ListRecordsWithTags :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
-- Returns records that have all of the specified tags
-- Optimized query to avoid n+1 problem by using GROUP_CONCAT for all tags
-- Cursor-based pagination: uses cursor_timestamp and cursor_id for pagination
SELECT
    r.id,
    r.project_id,
    r.value,
    r.timestamp,
    COALESCE((
        SELECT GROUP_CONCAT(tag, ' ')
        FROM (
            SELECT tag
            FROM tags
            WHERE record_id = r.id
            ORDER BY order_index
        )
    ), '') as all_tags
FROM records r
INNER JOIN tags t ON r.id = t.record_id
WHERE r.timestamp BETWEEN ? AND ? AND r.project_id = ?
  AND t.tag IN (sqlc.slice(tags))
  AND (? IS NULL OR r.timestamp < ? OR (r.timestamp = ? AND r.id > ?))
GROUP BY r.id, r.project_id, r.value, r.timestamp
HAVING COUNT(DISTINCT t.tag) = CAST(? AS INTEGER)
ORDER BY r.timestamp DESC, r.id
LIMIT ?;


-- name: DeleteRecordsUntil :execresult
DELETE FROM records WHERE timestamp < ?;

-- name: UpdateRecord :execresult
UPDATE records SET project_id = ?, value = ?, timestamp = ?
WHERE id = ?;

-- name: DeleteRecordTags :exec
DELETE FROM tags WHERE record_id = ?;

-- name: DeleteRecordsUntilByProject :execresult
DELETE FROM records WHERE project_id = ? AND timestamp < ?;

-- name: CreateProject :execresult
INSERT INTO projects (name, description, created_at, updated_at)
VALUES (?, ?, ?, ?);

-- name: GetProject :one
SELECT id, name, description, created_at, updated_at
FROM projects
WHERE id = ?;

-- name: UpdateProject :execresult
UPDATE projects SET description = ?, updated_at = ?
WHERE id = ?;

-- name: DeleteProject :exec
DELETE FROM projects WHERE id = ?;

-- name: ListProjects :many
-- Cursor-based pagination: uses cursor_updated_at and cursor_name for pagination
SELECT id, name, description, created_at, updated_at
FROM projects
WHERE ? IS NULL OR updated_at < ? OR (updated_at = ? AND name > ?)
ORDER BY updated_at DESC, name
LIMIT ?;

-- name: GetProjectTags :many
SELECT DISTINCT tag
FROM tags t
JOIN records r ON t.record_id = r.id
WHERE r.project_id = ?
ORDER BY tag;
