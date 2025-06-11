-- name: CreateRecord :exec
INSERT INTO records (id, project, value, done_at)
VALUES (?, ?, ?, ?);

-- name: GetRecord :one
SELECT id, project, value, done_at
FROM records
WHERE id = ?;

-- name: DeleteRecord :execresult
DELETE FROM records WHERE id = ?;

-- name: ListRecords :many
-- Note: BETWEEN clause must come first due to sqlc bug with SQLite parameter handling
SELECT id, project, value, done_at
FROM records
WHERE done_at BETWEEN ? AND ? AND project = ?
ORDER BY done_at;

-- name: GetProjectInfo :one
SELECT 
    COUNT(*) as record_count,
    COALESCE(SUM(value), 0) as total_value,
    MIN(done_at) as first_record_at,
    MAX(done_at) as last_record_at
FROM records
WHERE project = ?;

-- name: DeleteProject :exec
DELETE FROM records WHERE project = ?;

-- name: DeleteRecordsUntil :execresult
DELETE FROM records WHERE done_at < ?;

-- name: DeleteRecordsUntilByProject :execresult
DELETE FROM records WHERE project = ? AND done_at < ?;