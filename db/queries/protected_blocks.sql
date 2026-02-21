-- name: ListActiveProtectedBlocks :many
SELECT * FROM protected_blocks WHERE is_active = 1 ORDER BY start_time;

-- name: CreateProtectedBlock :one
INSERT INTO protected_blocks (label, day_of_week, start_time, end_time)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetProtectedBlock :one
SELECT * FROM protected_blocks WHERE id = ?;

-- name: DeleteProtectedBlock :exec
DELETE FROM protected_blocks WHERE id = ?;
