-- name: CreateActivitySuggestion :one
INSERT INTO activity_suggestions (
    suggested_date, start_time, end_time, title, description,
    category, energy_level, estimated_cost, location, location_url,
    weather_context, reasoning, llm_request_id
)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING *;

-- name: GetActivitySuggestion :one
SELECT * FROM activity_suggestions WHERE id = ?;

-- name: ListPendingSuggestionsForDate :many
SELECT * FROM activity_suggestions
WHERE suggested_date = ? AND status = 'pending'
ORDER BY start_time;

-- name: CountPendingSuggestionsForDate :one
SELECT COUNT(*) AS count FROM activity_suggestions
WHERE suggested_date = ? AND status = 'pending';

-- name: UpdateSuggestionStatus :one
UPDATE activity_suggestions
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: UpdateSuggestionNylasEventID :exec
UPDATE activity_suggestions
SET nylas_event_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListRecentSuggestions :many
SELECT * FROM activity_suggestions
WHERE suggested_date >= ?
ORDER BY suggested_date DESC, start_time
LIMIT 30;

-- name: ExpireStaleSuggestions :exec
UPDATE activity_suggestions
SET status = 'expired', updated_at = CURRENT_TIMESTAMP
WHERE status = 'pending' AND suggested_date < ?;
