-- name: DeleteDailyGapsForDate :exec
DELETE FROM daily_gaps WHERE gap_date = ?;

-- name: CreateDailyGap :one
INSERT INTO daily_gaps (gap_date, start_time, end_time, duration_minutes, time_of_day, duration_bucket, before_event_title, after_event_title, pipeline_run_id)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: ListDailyGapsByDate :many
SELECT * FROM daily_gaps
WHERE gap_date = ?
ORDER BY start_time;

-- name: ListDailyGapsForDateRange :many
SELECT * FROM daily_gaps
WHERE gap_date BETWEEN sqlc.arg(start_date) AND sqlc.arg(end_date)
ORDER BY gap_date, start_time;
