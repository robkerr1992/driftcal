package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/nylas"
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
func ApproveSuggestion(q *sqlcdb.Queries, nylasClient NylasEventCreator, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid suggestion ID", log)
			return
		}

		ctx := r.Context()

		suggestion, err := q.GetActivitySuggestion(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "suggestion not found", log)
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get suggestion")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get suggestion", log)
			return
		}

		if suggestion.Status != "pending" {
			RespondError(w, http.StatusBadRequest, "bad_request", "only pending suggestions can be approved", log)
			return
		}

		updated, err := q.UpdateSuggestionStatus(ctx, sqlcdb.UpdateSuggestionStatusParams{
			Status: "approved",
			ID:     id,
		})
		if err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to approve suggestion")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to approve suggestion", log)
			return
		}

		// Record feedback.
		if _, err := q.CreateSuggestionFeedback(ctx, sqlcdb.CreateSuggestionFeedbackParams{
			SuggestionID: id,
			Action:       "approved",
		}); err != nil {
			log.Error().Err(err).Int64("suggestion_id", id).Msg("failed to create feedback record")
		}

		// Optionally push to Nylas.
		if nylasClient != nil {
			pushSuggestionToNylas(ctx, q, nylasClient, updated, log)
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

		ctx := r.Context()

		suggestion, err := q.GetActivitySuggestion(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "suggestion not found", log)
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get suggestion")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get suggestion", log)
			return
		}

		if suggestion.Status != "pending" {
			RespondError(w, http.StatusBadRequest, "bad_request", "only pending suggestions can be rejected", log)
			return
		}

		// Parse optional notes.
		var body struct {
			Notes string `json:"notes"`
		}
		json.NewDecoder(r.Body).Decode(&body) // OK if body is empty

		updated, err := q.UpdateSuggestionStatus(ctx, sqlcdb.UpdateSuggestionStatusParams{
			Status: "rejected",
			ID:     id,
		})
		if err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to reject suggestion")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to reject suggestion", log)
			return
		}

		// Record feedback.
		if _, err := q.CreateSuggestionFeedback(ctx, sqlcdb.CreateSuggestionFeedbackParams{
			SuggestionID: id,
			Action:       "rejected",
			EditNotes:    sql.NullString{String: body.Notes, Valid: body.Notes != ""},
		}); err != nil {
			log.Error().Err(err).Int64("suggestion_id", id).Msg("failed to create feedback record")
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

func pushSuggestionToNylas(ctx context.Context, q *sqlcdb.Queries, nylasClient NylasEventCreator, s sqlcdb.ActivitySuggestion, log zerolog.Logger) {
	// Best-effort: need grant_id and calendar_id from preferences.
	// We can't access the preferences store directly here, so we check the DB.
	grantPref, err := q.GetPreference(ctx, "nylas_grant_id")
	if err != nil {
		return
	}
	calPref, err := q.GetPreference(ctx, "nylas_calendar_id")
	if err != nil {
		return
	}

	evt, err := nylasClient.CreateEvent(ctx, grantPref.Value, calPref.Value, nylas.CreateEventRequest{
		Title:       s.Title,
		Description: s.Description,
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: s.StartTime.Unix(),
			EndTime:   s.EndTime.Unix(),
		},
		Busy: false,
	})
	if err != nil {
		log.Warn().Err(err).Int64("suggestion_id", s.ID).Msg("failed to push suggestion to Nylas")
		return
	}

	if err := q.UpdateSuggestionNylasEventID(ctx, sqlcdb.UpdateSuggestionNylasEventIDParams{
		NylasEventID: sql.NullString{String: evt.ID, Valid: true},
		ID:           s.ID,
	}); err != nil {
		log.Warn().Err(err).Int64("suggestion_id", s.ID).Msg("failed to store nylas_event_id on suggestion")
	}
}
