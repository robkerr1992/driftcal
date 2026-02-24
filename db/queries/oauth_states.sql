-- name: InsertOAuthState :exec
INSERT INTO oauth_states (state, expires_at) VALUES (?, ?);

-- name: ConsumeOAuthState :one
DELETE FROM oauth_states WHERE state = ? AND expires_at > ? RETURNING *;

-- name: DeleteExpiredOAuthStates :exec
DELETE FROM oauth_states WHERE expires_at <= ?;
