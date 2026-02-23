package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/action"
	"github.com/robkerr1992/driftcal/internal/pipeline"
)

// ListSuggestions returns pending suggestions for a given date.
// Query param: ?date=YYYY-MM-DD (defaults to tomorrow).
func ListSuggestions(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dateStr := r.URL.Query().Get("date")
		var date time.Time
		if dateStr == "" {
			now := time.Now().UTC()
			date = time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
		} else {
			var err error
			date, err = time.Parse("2006-01-02", dateStr)
			if err != nil {
				RespondError(w, http.StatusBadRequest, "bad_request", "date must be YYYY-MM-DD format", log)
				return
			}
		}

		suggestions, err := q.ListPendingSuggestionsForDate(r.Context(), date)
		if err != nil {
			log.Error().Err(err).Msg("failed to list suggestions")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to list suggestions", log)
			return
		}

		RespondJSON(w, http.StatusOK, map[string]any{"suggestions": suggestions}, log)
	}
}

// ApproveSuggestion transitions a pending suggestion to approved and records feedback.
func ApproveSuggestion(q *sqlcdb.Queries, nylasClient action.NylasEventCreator, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid suggestion ID", log)
			return
		}

		updated, err := action.ApproveSuggestion(r.Context(), q, nylasClient, id, log)
		if err != nil {
			switch {
			case errors.Is(err, action.ErrNotFound):
				RespondError(w, http.StatusNotFound, "not_found", "suggestion not found", log)
			case errors.Is(err, action.ErrInvalidStatus):
				RespondError(w, http.StatusBadRequest, "bad_request", "only pending suggestions can be approved", log)
			default:
				log.Error().Err(err).Int64("id", id).Msg("failed to approve suggestion")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to approve suggestion", log)
			}
			return
		}

		RespondJSON(w, http.StatusOK, map[string]any{"suggestion": updated}, log)
	}
}

// RejectSuggestion transitions a pending suggestion to rejected and records feedback.
func RejectSuggestion(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid suggestion ID", log)
			return
		}

		var body struct {
			Notes string `json:"notes"`
		}
		json.NewDecoder(r.Body).Decode(&body) // OK if body is empty

		updated, err := action.RejectSuggestion(r.Context(), q, id, body.Notes, log)
		if err != nil {
			switch {
			case errors.Is(err, action.ErrNotFound):
				RespondError(w, http.StatusNotFound, "not_found", "suggestion not found", log)
			case errors.Is(err, action.ErrInvalidStatus):
				RespondError(w, http.StatusBadRequest, "bad_request", "only pending suggestions can be rejected", log)
			default:
				log.Error().Err(err).Int64("id", id).Msg("failed to reject suggestion")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to reject suggestion", log)
			}
			return
		}

		RespondJSON(w, http.StatusOK, map[string]any{"suggestion": updated}, log)
	}
}

// TriggerPipeline manually triggers the daily pipeline for tomorrow.
func TriggerPipeline(runner *pipeline.Runner, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if runner == nil {
			RespondError(w, http.StatusServiceUnavailable, "service_unavailable", "pipeline not configured", log)
			return
		}

		now := time.Now().UTC()
		tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)

		if err := runner.RunDailyPipeline(r.Context(), tomorrow); err != nil {
			log.Error().Err(err).Msg("pipeline trigger failed")
			RespondError(w, http.StatusInternalServerError, "internal_error", "pipeline execution failed", log)
			return
		}

		RespondJSON(w, http.StatusOK, map[string]any{"status": "completed", "target_date": tomorrow.Format("2006-01-02")}, log)
	}
}
