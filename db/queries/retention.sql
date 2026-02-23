-- Retention cleanup queries for the maintenance cron job.
-- Order matters: suggestion_feedback must be deleted before activity_suggestions
-- because of the foreign key constraint (no ON DELETE CASCADE).

-- name: DeleteOldSuggestionFeedback :exec
DELETE FROM suggestion_feedback
WHERE suggestion_id IN (
    SELECT id FROM activity_suggestions WHERE suggested_date < ?
);

-- name: DeleteOldSuggestions :exec
DELETE FROM activity_suggestions WHERE suggested_date < ?;

-- name: DeleteOldEvents :exec
DELETE FROM events WHERE end_time < ?;

-- name: DeleteOldDailyGaps :exec
DELETE FROM daily_gaps WHERE gap_date < ?;

-- name: DeleteOldPipelineRuns :exec
DELETE FROM pipeline_runs WHERE started_at < ?;
