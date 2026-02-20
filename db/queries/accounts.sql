-- name: CreateAccount :one
INSERT INTO calendar_accounts (nylas_grant_id, provider, email, display_name)
VALUES (?, ?, ?, ?)
RETURNING *;

-- name: GetAccountByID :one
SELECT * FROM calendar_accounts WHERE id = ?;

-- name: GetAccountByGrantID :one
SELECT * FROM calendar_accounts WHERE nylas_grant_id = ?;

-- name: ListActiveAccounts :many
SELECT * FROM calendar_accounts WHERE is_active = 1 ORDER BY created_at;

-- name: DeactivateAccount :exec
UPDATE calendar_accounts
SET is_active = 0, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;
