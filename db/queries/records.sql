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
SELECT id, project, value, timestamp
FROM records
WHERE timestamp BETWEEN ? AND ? AND project = ?
ORDER BY timestamp;

-- name: ListRecordsWithTags :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
-- Returns records that have any of the specified tags
SELECT DISTINCT r.id, r.project, r.value, r.timestamp
FROM records r
JOIN tags t ON r.id = t.record_id
WHERE r.timestamp BETWEEN ? AND ? AND r.project = ? AND t.tag IN (sqlc.slice(tags))
ORDER BY r.timestamp;

-- name: GetProjectInfo :one
SELECT 
    COUNT(*) as record_count,
    COALESCE(SUM(value), 0) as total_value,
    MIN(timestamp) as first_record_at,
    MAX(timestamp) as last_record_at
FROM records
WHERE project = ?;

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
