-- name: CreatePipelineRun :one
INSERT INTO pipeline_runs (run_date, started_at, status)
VALUES (?, ?, 'running')
RETURNING *;

-- name: UpdatePipelineRunStep :exec
UPDATE pipeline_runs
SET last_completed_step = ?, metrics = ?, created_at = created_at
WHERE id = ?;

-- name: CompletePipelineRun :exec
UPDATE pipeline_runs
SET status = 'completed', completed_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: FailPipelineRun :exec
UPDATE pipeline_runs
SET status = 'failed', error_message = ?, completed_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetLatestPipelineRun :one
SELECT * FROM pipeline_runs
ORDER BY started_at DESC LIMIT 1;

-- name: GetPipelineRunByDate :one
SELECT * FROM pipeline_runs
WHERE run_date = ?
ORDER BY started_at DESC LIMIT 1;
