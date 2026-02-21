-- name: ListActiveGoals :many
SELECT * FROM recurring_goals
WHERE is_active = 1
ORDER BY priority DESC, id;

-- name: GetGoal :one
SELECT * FROM recurring_goals WHERE id = ?;

-- name: CreateGoal :one
INSERT INTO recurring_goals (
    label, category, duration_minutes, times_per_week,
    preferred_time_of_day, energy_level, earliest_hour, latest_hour,
    allowed_days, min_gap_between_hours, priority
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: UpdateGoal :one
UPDATE recurring_goals SET
    label = ?,
    category = ?,
    duration_minutes = ?,
    times_per_week = ?,
    preferred_time_of_day = ?,
    energy_level = ?,
    earliest_hour = ?,
    latest_hour = ?,
    allowed_days = ?,
    min_gap_between_hours = ?,
    priority = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: DeactivateGoal :exec
UPDATE recurring_goals SET
    is_active = 0,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;
