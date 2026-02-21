-- name: GetPreference :one
SELECT * FROM user_preferences WHERE key = ?;

-- name: UpsertPreference :one
INSERT INTO user_preferences (key, value)
VALUES (?, ?)
ON CONFLICT (key) DO UPDATE SET
  value = excluded.value,
  updated_at = CURRENT_TIMESTAMP
RETURNING *;

-- name: ListPreferences :many
SELECT * FROM user_preferences ORDER BY key;
