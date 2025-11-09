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
SELECT
    r.id,
    r.project,
    r.value,
    r.timestamp,
    COALESCE(GROUP_CONCAT(t.tag, ' '), '') as tags
FROM records r
LEFT JOIN tags t ON r.id = t.record_id
WHERE r.timestamp BETWEEN ? AND ? AND r.project = ?
GROUP BY r.id, r.project, r.value, r.timestamp
ORDER BY r.timestamp DESC
LIMIT ? OFFSET ?;

-- name: ListRecordsWithTags :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
-- Returns records that have all of the specified tags
-- Optimized query to avoid n+1 problem by using GROUP_CONCAT for all tags
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
GROUP BY r.id, r.project, r.value, r.timestamp
HAVING COUNT(DISTINCT t.tag) = CAST(? AS INTEGER)
ORDER BY r.timestamp DESC
LIMIT ? OFFSET ?;


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
SELECT name, description, created_at, updated_at
FROM projects
ORDER BY updated_at DESC
LIMIT ? OFFSET ?;

-- name: GetProjectTags :many
SELECT DISTINCT tag
FROM tags t
JOIN records r ON t.record_id = r.id
WHERE r.project = ?
ORDER BY tag;
