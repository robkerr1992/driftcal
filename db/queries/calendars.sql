-- name: CreateCalendar :one
INSERT INTO calendars (account_id, nylas_calendar_id, name, color)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetCalendarByID :one
SELECT * FROM calendars WHERE id = ?;

-- name: GetCalendarByNylasID :one
SELECT * FROM calendars WHERE nylas_calendar_id = ?;

-- name: ListCalendars :many
SELECT
    c.*,
    ca.email AS account_email,
    ca.provider AS account_provider
FROM calendars c
JOIN calendar_accounts ca ON ca.id = c.account_id
ORDER BY ca.email, c.name;

-- name: ListCalendarsByAccountID :many
SELECT * FROM calendars WHERE account_id = ? ORDER BY name;

-- name: UpdateCalendarFlags :exec
UPDATE calendars
SET is_blocking = ?, is_active = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: ListActiveCalendarIDs :many
SELECT
    c.nylas_calendar_id,
    c.id AS calendar_id,
    ca.nylas_grant_id
FROM calendars c
JOIN calendar_accounts ca ON ca.id = c.account_id
WHERE c.is_active = 1 AND ca.is_active = 1;

-- name: ListActiveCalendarIDsByGrantID :many
SELECT
    c.nylas_calendar_id,
    c.id AS calendar_id,
    ca.nylas_grant_id
FROM calendars c
JOIN calendar_accounts ca ON ca.id = c.account_id
WHERE c.is_active = 1
  AND ca.is_active = 1
  AND ca.nylas_grant_id = ?;
