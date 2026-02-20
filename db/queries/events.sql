-- name: UpsertEvent :one
INSERT INTO events (
    calendar_id, nylas_event_id, title, description, location,
    start_time, end_time, original_tz, all_day,
    status, busy, category, recurrence_rule, raw_data
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (nylas_event_id) DO UPDATE SET
    title = excluded.title,
    description = excluded.description,
    location = excluded.location,
    start_time = excluded.start_time,
    end_time = excluded.end_time,
    original_tz = excluded.original_tz,
    all_day = excluded.all_day,
    status = excluded.status,
    busy = excluded.busy,
    category = excluded.category,
    recurrence_rule = excluded.recurrence_rule,
    raw_data = excluded.raw_data,
    updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: GetEventByNylasID :one
SELECT * FROM events WHERE nylas_event_id = ?;

-- name: SoftDeleteEventByNylasID :exec
UPDATE events
SET status = 'cancelled', updated_at = CURRENT_TIMESTAMP
WHERE nylas_event_id = ?;

-- name: ListEventsByCalendarAndRange :many
SELECT * FROM events
WHERE calendar_id = ?
  AND start_time >= ?
  AND end_time <= ?
  AND status != 'cancelled'
ORDER BY start_time;

-- name: CountEventsByCalendar :one
SELECT COUNT(*) FROM events WHERE calendar_id = ? AND status != 'cancelled';
