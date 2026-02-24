-- name: ListBlockingEventsInRange :many
-- Cross-calendar query for all blocking events in a time range.
-- Joins events -> calendars to filter by is_blocking=1, is_active=1.
-- Filters: status != 'cancelled', busy != 'free', time range overlap.
-- Note: all-day tentative events are filtered in Go (see algorithm step 1).
SELECT e.* FROM events e
JOIN calendars c ON e.calendar_id = c.id
WHERE c.is_blocking = 1 AND c.is_active = 1
  AND e.status != 'cancelled' AND e.busy != 'free'
  AND e.start_time < sqlc.arg(range_end) AND e.end_time > sqlc.arg(range_start)
ORDER BY e.start_time;

-- name: CountBlockingEventsInRange :one
-- Returns the number of blocking events that overlap the given time range.
-- Used for conflict checks at suggestion approval time.
SELECT COUNT(*) AS count FROM events e
JOIN calendars c ON e.calendar_id = c.id
WHERE c.is_blocking = 1 AND c.is_active = 1
  AND e.status != 'cancelled' AND e.busy != 'free'
  AND e.start_time < sqlc.arg(range_end) AND e.end_time > sqlc.arg(range_start);
