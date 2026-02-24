-- +goose Up
CREATE TABLE oauth_states (
    state TEXT PRIMARY KEY,
    expires_at DATETIME NOT NULL
);

-- +goose Down
DROP TABLE IF EXISTS oauth_states;
