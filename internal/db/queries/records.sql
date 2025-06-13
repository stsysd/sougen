-- name: CreateRecord :exec
INSERT INTO records (id, project, value, timestamp)
VALUES (?, ?, ?, ?);

-- name: GetRecord :one
SELECT id, project, value, timestamp
FROM records
WHERE id = ?;

-- name: DeleteRecord :execresult
DELETE FROM records WHERE id = ?;

-- name: ListRecords :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
SELECT id, project, value, timestamp
FROM records
WHERE timestamp BETWEEN ? AND ? AND project = ?
ORDER BY timestamp;

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

-- name: DeleteRecordsUntilByProject :execresult
DELETE FROM records WHERE project = ? AND timestamp < ?;
