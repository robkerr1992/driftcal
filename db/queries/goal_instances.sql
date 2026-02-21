-- name: ListScheduledGoalInstancesInRange :many
-- Fetches scheduled goal instances overlapping a time range.
-- Used as a blocking source in gap detection (architecture.md lines 203-205).
SELECT * FROM goal_instances
WHERE status = 'scheduled'
  AND scheduled_start < sqlc.arg(range_end) AND scheduled_end > sqlc.arg(range_start)
ORDER BY scheduled_start;
