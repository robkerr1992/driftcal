-- name: ListScheduledGoalInstancesInRange :many
-- Fetches scheduled goal instances overlapping a time range.
-- Used as a blocking source in gap detection (architecture.md lines 203-205).
SELECT * FROM goal_instances
WHERE status = 'scheduled'
  AND scheduled_start < sqlc.arg(range_end) AND scheduled_end > sqlc.arg(range_start)
ORDER BY scheduled_start;

-- name: ListScheduledGoalInstancesInRangeWithLabel :many
-- Same as above but joins recurring_goals for the goal label.
SELECT gi.*, rg.label AS goal_label
FROM goal_instances gi
JOIN recurring_goals rg ON gi.goal_id = rg.id
WHERE gi.status = 'scheduled'
  AND gi.scheduled_start < sqlc.arg(range_end) AND gi.scheduled_end > sqlc.arg(range_start)
ORDER BY gi.scheduled_start;

-- name: CreateGoalInstance :one
INSERT INTO goal_instances (goal_id, week_start, scheduled_start, scheduled_end)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetGoalInstance :one
SELECT * FROM goal_instances WHERE id = ?;

-- name: UpdateGoalInstanceStatus :one
UPDATE goal_instances SET
    status = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: ListGoalInstancesByGoalAndWeek :many
SELECT * FROM goal_instances
WHERE goal_id = ? AND week_start = ?
ORDER BY scheduled_start;

-- name: CountFulfilledInstancesForWeek :one
SELECT COUNT(*) AS count FROM goal_instances
WHERE goal_id = ? AND week_start = ?
  AND status IN ('scheduled', 'completed');
