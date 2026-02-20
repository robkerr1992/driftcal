package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

// ListCalendars returns all calendars with their account info.
func ListCalendars(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		calendars, err := q.ListCalendars(r.Context())
		if err != nil {
			log.Error().Err(err).Msg("failed to list calendars")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to list calendars", log)
			return
		}
		RespondJSON(w, http.StatusOK, calendars, log)
	}
}

// UpdateCalendar updates the is_blocking and is_active flags on a calendar.
func UpdateCalendar(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid calendar ID", log)
			return
		}

		var body struct {
			IsBlocking *bool `json:"is_blocking"`
			IsActive   *bool `json:"is_active"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", log)
			return
		}

		ctx := r.Context()

		cal, err := q.GetCalendarByID(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "calendar not found", log)
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get calendar")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get calendar", log)
			return
		}

		isBlocking := cal.IsBlocking
		if body.IsBlocking != nil {
			isBlocking = *body.IsBlocking
		}
		isActive := cal.IsActive
		if body.IsActive != nil {
			isActive = *body.IsActive
		}

		if err := q.UpdateCalendarFlags(ctx, sqlcdb.UpdateCalendarFlagsParams{
			IsBlocking: isBlocking,
			IsActive:   isActive,
			ID:         id,
		}); err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to update calendar flags")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to update calendar", log)
			return
		}

		// Fetch updated calendar for response
		updated, err := q.GetCalendarByID(ctx, id)
		if err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to fetch updated calendar")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to fetch updated calendar", log)
			return
		}
		RespondJSON(w, http.StatusOK, updated, log)
	}
}
