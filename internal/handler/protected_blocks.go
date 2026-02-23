package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

var hhmmRegex = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

// ListProtectedBlocks returns all active protected blocks.
func ListProtectedBlocks(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		blocks, err := q.ListActiveProtectedBlocks(r.Context())
		if err != nil {
			log.Error().Err(err).Msg("failed to list protected blocks")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to list protected blocks", log)
			return
		}
		RespondJSON(w, http.StatusOK, map[string]any{"blocks": blocks}, log)
	}
}

// CreateProtectedBlock creates a new protected time block.
func CreateProtectedBlock(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Label     string `json:"label"`
			DayOfWeek *int64 `json:"day_of_week"`
			StartTime string `json:"start_time"`
			EndTime   string `json:"end_time"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", log)
			return
		}

		if body.Label == "" {
			RespondError(w, http.StatusBadRequest, "bad_request", "label is required", log)
			return
		}
		if !hhmmRegex.MatchString(body.StartTime) {
			RespondError(w, http.StatusBadRequest, "bad_request", "start_time must be HH:MM format", log)
			return
		}
		if !hhmmRegex.MatchString(body.EndTime) {
			RespondError(w, http.StatusBadRequest, "bad_request", "end_time must be HH:MM format", log)
			return
		}
		if body.EndTime <= body.StartTime {
			RespondError(w, http.StatusBadRequest, "bad_request", "end_time must be after start_time (overnight blocks not supported)", log)
			return
		}
		if body.DayOfWeek != nil && (*body.DayOfWeek < 0 || *body.DayOfWeek > 6) {
			RespondError(w, http.StatusBadRequest, "bad_request", "day_of_week must be 0-6 or null", log)
			return
		}

		var dow sql.NullInt64
		if body.DayOfWeek != nil {
			dow = sql.NullInt64{Int64: *body.DayOfWeek, Valid: true}
		}

		block, err := q.CreateProtectedBlock(r.Context(), sqlcdb.CreateProtectedBlockParams{
			Label:     body.Label,
			DayOfWeek: dow,
			StartTime: body.StartTime,
			EndTime:   body.EndTime,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to create protected block")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to create protected block", log)
			return
		}

		RespondJSON(w, http.StatusCreated, block, log)
	}
}

// DeleteProtectedBlock deletes a protected block by ID.
func DeleteProtectedBlock(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid block ID", log)
			return
		}

		// Verify block exists
		if _, err := q.GetProtectedBlock(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "protected block not found", log)
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get protected block")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get protected block", log)
			return
		}

		if err := q.DeleteProtectedBlock(r.Context(), id); err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to delete protected block")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to delete protected block", log)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}
