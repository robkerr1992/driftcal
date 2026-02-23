-- name: CreateSuggestionFeedback :one
INSERT INTO suggestion_feedback (suggestion_id, action, edit_notes)
VALUES (?, ?, ?)
RETURNING *;

-- name: ListFeedbackForSuggestion :many
SELECT * FROM suggestion_feedback
WHERE suggestion_id = ?
ORDER BY created_at;
