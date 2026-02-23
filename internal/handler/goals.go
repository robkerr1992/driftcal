package handler

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/action"
	"github.com/robkerr1992/driftcal/internal/goal"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

var validCategories = map[string]bool{
	"study": true, "workout": true, "creative": true, "social": true,
	"errand": true, "health": true, "hobby": true, "other": true,
}

var validEnergyLevels = map[string]bool{
	"low": true, "medium": true, "high": true,
}

var validTimeOfDay = map[string]bool{
	"any": true, "morning": true, "afternoon": true, "evening": true,
}

var validDays = map[string]bool{
	"mon": true, "tue": true, "wed": true, "thu": true,
	"fri": true, "sat": true, "sun": true,
}

// goalResponse wraps a RecurringGoal with computed this_week counts.
type goalResponse struct {
	ID                 int64          `json:"id"`
	Label              string         `json:"label"`
	Category           string         `json:"category"`
	DurationMinutes    int64          `json:"duration_minutes"`
	TimesPerWeek       int64          `json:"times_per_week"`
	PreferredTimeOfDay string         `json:"preferred_time_of_day"`
	EnergyLevel        string         `json:"energy_level"`
	EarliestHour       string         `json:"earliest_hour"`
	LatestHour         string         `json:"latest_hour"`
	AllowedDays        []string       `json:"allowed_days"`
	MinGapBetweenHours int64          `json:"min_gap_between_hours"`
	IsActive           bool           `json:"is_active"`
	Priority           int64          `json:"priority"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	ThisWeek           thisWeekCounts `json:"this_week"`
}

type thisWeekCounts struct {
	Scheduled int `json:"scheduled"`
	Completed int `json:"completed"`
	Remaining int `json:"remaining"`
}

func goalToResponse(g sqlcdb.RecurringGoal) goalResponse {
	return goalResponse{
		ID:                 g.ID,
		Label:              g.Label,
		Category:           g.Category,
		DurationMinutes:    g.DurationMinutes,
		TimesPerWeek:       g.TimesPerWeek,
		PreferredTimeOfDay: g.PreferredTimeOfDay.String,
		EnergyLevel:        g.EnergyLevel,
		EarliestHour:       g.EarliestHour,
		LatestHour:         g.LatestHour,
		AllowedDays:        goal.ParseAllowedDays(g.AllowedDays),
		MinGapBetweenHours: g.MinGapBetweenHours,
		IsActive:           g.IsActive,
		Priority:           g.Priority,
		CreatedAt:          g.CreatedAt,
		UpdatedAt:          g.UpdatedAt,
	}
}

func marshalAllowedDays(days []string) sql.NullString {
	if len(days) == 0 {
		return sql.NullString{Valid: false}
	}
	b, err := json.Marshal(days)
	if err != nil {
		return sql.NullString{Valid: false}
	}
	return sql.NullString{String: string(b), Valid: true}
}

// ListGoals returns all active goals with this_week instance counts.
// Uses a batch query (2 queries total) instead of N+1.
func ListGoals(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		goals, err := q.ListActiveGoals(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to list goals")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to list goals", log)
			return
		}

		weekStart := goal.MondayOf(time.Now())

		// Batch fetch instance counts for all goals in one query.
		counts, err := q.ListGoalInstanceCountsForWeek(ctx, weekStart)
		if err != nil {
			log.Error().Err(err).Msg("failed to list instance counts")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load goal instance counts", log)
			return
		}

		// Build map[goalID] → {scheduled, completed}.
		type weekCounts struct{ scheduled, completed int }
		countsMap := make(map[int64]*weekCounts)
		for _, c := range counts {
			wc, ok := countsMap[c.GoalID]
			if !ok {
				wc = &weekCounts{}
				countsMap[c.GoalID] = wc
			}
			switch c.Status {
			case "scheduled":
				wc.scheduled = int(c.Count)
			case "completed":
				wc.completed = int(c.Count)
			}
		}

		responses := make([]goalResponse, 0, len(goals))
		for _, g := range goals {
			resp := goalToResponse(g)

			scheduled := 0
			completed := 0
			if wc, ok := countsMap[g.ID]; ok {
				scheduled = wc.scheduled
				completed = wc.completed
			}

			fulfilled := scheduled + completed
			remaining := int(g.TimesPerWeek) - fulfilled
			if remaining < 0 {
				remaining = 0
			}

			resp.ThisWeek = thisWeekCounts{
				Scheduled: scheduled,
				Completed: completed,
				Remaining: remaining,
			}
			responses = append(responses, resp)
		}

		RespondJSON(w, http.StatusOK, map[string]any{"goals": responses}, log)
	}
}

// CreateGoal creates a new recurring goal with full validation.
func CreateGoal(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Label              string   `json:"label"`
			Category           string   `json:"category"`
			DurationMinutes    int64    `json:"duration_minutes"`
			TimesPerWeek       int64    `json:"times_per_week"`
			Priority           *int64   `json:"priority"`
			EnergyLevel        *string  `json:"energy_level"`
			PreferredTimeOfDay *string  `json:"preferred_time_of_day"`
			EarliestHour       *string  `json:"earliest_hour"`
			LatestHour         *string  `json:"latest_hour"`
			AllowedDays        []string `json:"allowed_days"`
			MinGapBetweenHours *int64   `json:"min_gap_between_hours"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", log)
			return
		}

		// Required field validation.
		if body.Label == "" {
			RespondError(w, http.StatusBadRequest, "bad_request", "label is required", log)
			return
		}
		if !validCategories[body.Category] {
			RespondError(w, http.StatusBadRequest, "bad_request", "category must be one of: study, workout, creative, social, errand, health, hobby, other", log)
			return
		}
		if body.DurationMinutes < 15 {
			RespondError(w, http.StatusBadRequest, "bad_request", "duration_minutes must be at least 15", log)
			return
		}
		if body.TimesPerWeek < 1 || body.TimesPerWeek > 14 {
			RespondError(w, http.StatusBadRequest, "bad_request", "times_per_week must be between 1 and 14", log)
			return
		}

		// Defaults for optional fields.
		priority := int64(3)
		if body.Priority != nil {
			priority = *body.Priority
		}
		if priority < 1 || priority > 5 {
			RespondError(w, http.StatusBadRequest, "bad_request", "priority must be between 1 and 5", log)
			return
		}

		energyLevel := "medium"
		if body.EnergyLevel != nil {
			energyLevel = *body.EnergyLevel
		}
		if !validEnergyLevels[energyLevel] {
			RespondError(w, http.StatusBadRequest, "bad_request", "energy_level must be one of: low, medium, high", log)
			return
		}

		preferredTimeOfDay := "any"
		if body.PreferredTimeOfDay != nil {
			preferredTimeOfDay = *body.PreferredTimeOfDay
		}
		if !validTimeOfDay[preferredTimeOfDay] {
			RespondError(w, http.StatusBadRequest, "bad_request", "preferred_time_of_day must be one of: any, morning, afternoon, evening", log)
			return
		}

		earliestHour := "07:00"
		if body.EarliestHour != nil {
			earliestHour = *body.EarliestHour
		}
		if !hhmmRegex.MatchString(earliestHour) {
			RespondError(w, http.StatusBadRequest, "bad_request", "earliest_hour must be HH:MM format", log)
			return
		}

		latestHour := "22:00"
		if body.LatestHour != nil {
			latestHour = *body.LatestHour
		}
		if !hhmmRegex.MatchString(latestHour) {
			RespondError(w, http.StatusBadRequest, "bad_request", "latest_hour must be HH:MM format", log)
			return
		}
		if latestHour <= earliestHour {
			RespondError(w, http.StatusBadRequest, "bad_request", "latest_hour must be after earliest_hour", log)
			return
		}

		// Validate allowed_days.
		if len(body.AllowedDays) > 0 {
			seen := make(map[string]bool)
			for _, day := range body.AllowedDays {
				if !validDays[day] {
					RespondError(w, http.StatusBadRequest, "bad_request", "allowed_days must contain only: mon, tue, wed, thu, fri, sat, sun", log)
					return
				}
				if seen[day] {
					RespondError(w, http.StatusBadRequest, "bad_request", "allowed_days must not contain duplicates", log)
					return
				}
				seen[day] = true
			}
		}

		minGapBetween := int64(24)
		if body.MinGapBetweenHours != nil {
			minGapBetween = *body.MinGapBetweenHours
		}
		if minGapBetween < 0 {
			RespondError(w, http.StatusBadRequest, "bad_request", "min_gap_between_hours must be non-negative", log)
			return
		}

		created, err := q.CreateGoal(r.Context(), sqlcdb.CreateGoalParams{
			Label:              body.Label,
			Category:           body.Category,
			DurationMinutes:    body.DurationMinutes,
			TimesPerWeek:       body.TimesPerWeek,
			PreferredTimeOfDay: sql.NullString{String: preferredTimeOfDay, Valid: true},
			EnergyLevel:        energyLevel,
			EarliestHour:       earliestHour,
			LatestHour:         latestHour,
			AllowedDays:        marshalAllowedDays(body.AllowedDays),
			MinGapBetweenHours: minGapBetween,
			Priority:           priority,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to create goal")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to create goal", log)
			return
		}

		RespondJSON(w, http.StatusCreated, goalToResponse(created), log)
	}
}

// UpdateGoal updates a recurring goal with partial update support.
func UpdateGoal(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid goal ID", log)
			return
		}

		ctx := r.Context()

		existing, err := q.GetGoal(ctx, id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "goal not found", log)
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get goal")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get goal", log)
			return
		}

		var body struct {
			Label              *string  `json:"label"`
			Category           *string  `json:"category"`
			DurationMinutes    *int64   `json:"duration_minutes"`
			TimesPerWeek       *int64   `json:"times_per_week"`
			Priority           *int64   `json:"priority"`
			EnergyLevel        *string  `json:"energy_level"`
			PreferredTimeOfDay *string  `json:"preferred_time_of_day"`
			EarliestHour       *string  `json:"earliest_hour"`
			LatestHour         *string  `json:"latest_hour"`
			AllowedDays        []string `json:"allowed_days"`
			MinGapBetweenHours *int64   `json:"min_gap_between_hours"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", log)
			return
		}

		// Merge with existing values.
		params := sqlcdb.UpdateGoalParams{
			ID:                 id,
			Label:              existing.Label,
			Category:           existing.Category,
			DurationMinutes:    existing.DurationMinutes,
			TimesPerWeek:       existing.TimesPerWeek,
			PreferredTimeOfDay: existing.PreferredTimeOfDay,
			EnergyLevel:        existing.EnergyLevel,
			EarliestHour:       existing.EarliestHour,
			LatestHour:         existing.LatestHour,
			AllowedDays:        existing.AllowedDays,
			MinGapBetweenHours: existing.MinGapBetweenHours,
			Priority:           existing.Priority,
		}

		if body.Label != nil {
			if *body.Label == "" {
				RespondError(w, http.StatusBadRequest, "bad_request", "label cannot be empty", log)
				return
			}
			params.Label = *body.Label
		}
		if body.Category != nil {
			if !validCategories[*body.Category] {
				RespondError(w, http.StatusBadRequest, "bad_request", "category must be one of: study, workout, creative, social, errand, health, hobby, other", log)
				return
			}
			params.Category = *body.Category
		}
		if body.DurationMinutes != nil {
			if *body.DurationMinutes < 15 {
				RespondError(w, http.StatusBadRequest, "bad_request", "duration_minutes must be at least 15", log)
				return
			}
			params.DurationMinutes = *body.DurationMinutes
		}
		if body.TimesPerWeek != nil {
			if *body.TimesPerWeek < 1 || *body.TimesPerWeek > 14 {
				RespondError(w, http.StatusBadRequest, "bad_request", "times_per_week must be between 1 and 14", log)
				return
			}
			params.TimesPerWeek = *body.TimesPerWeek
		}
		if body.Priority != nil {
			if *body.Priority < 1 || *body.Priority > 5 {
				RespondError(w, http.StatusBadRequest, "bad_request", "priority must be between 1 and 5", log)
				return
			}
			params.Priority = *body.Priority
		}
		if body.EnergyLevel != nil {
			if !validEnergyLevels[*body.EnergyLevel] {
				RespondError(w, http.StatusBadRequest, "bad_request", "energy_level must be one of: low, medium, high", log)
				return
			}
			params.EnergyLevel = *body.EnergyLevel
		}
		if body.PreferredTimeOfDay != nil {
			if !validTimeOfDay[*body.PreferredTimeOfDay] {
				RespondError(w, http.StatusBadRequest, "bad_request", "preferred_time_of_day must be one of: any, morning, afternoon, evening", log)
				return
			}
			params.PreferredTimeOfDay = sql.NullString{String: *body.PreferredTimeOfDay, Valid: true}
		}
		if body.EarliestHour != nil {
			if !hhmmRegex.MatchString(*body.EarliestHour) {
				RespondError(w, http.StatusBadRequest, "bad_request", "earliest_hour must be HH:MM format", log)
				return
			}
			params.EarliestHour = *body.EarliestHour
		}
		if body.LatestHour != nil {
			if !hhmmRegex.MatchString(*body.LatestHour) {
				RespondError(w, http.StatusBadRequest, "bad_request", "latest_hour must be HH:MM format", log)
				return
			}
			params.LatestHour = *body.LatestHour
		}
		// Validate earliest < latest after merge.
		if params.LatestHour <= params.EarliestHour {
			RespondError(w, http.StatusBadRequest, "bad_request", "latest_hour must be after earliest_hour", log)
			return
		}
		if body.AllowedDays != nil {
			seen := make(map[string]bool)
			for _, day := range body.AllowedDays {
				if !validDays[day] {
					RespondError(w, http.StatusBadRequest, "bad_request", "allowed_days must contain only: mon, tue, wed, thu, fri, sat, sun", log)
					return
				}
				if seen[day] {
					RespondError(w, http.StatusBadRequest, "bad_request", "allowed_days must not contain duplicates", log)
					return
				}
				seen[day] = true
			}
			params.AllowedDays = marshalAllowedDays(body.AllowedDays)
		}
		if body.MinGapBetweenHours != nil {
			if *body.MinGapBetweenHours < 0 {
				RespondError(w, http.StatusBadRequest, "bad_request", "min_gap_between_hours must be non-negative", log)
				return
			}
			params.MinGapBetweenHours = *body.MinGapBetweenHours
		}

		updated, err := q.UpdateGoal(ctx, params)
		if err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to update goal")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to update goal", log)
			return
		}

		RespondJSON(w, http.StatusOK, goalToResponse(updated), log)
	}
}

// DeleteGoal soft-deletes a goal by setting is_active = false.
func DeleteGoal(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid goal ID", log)
			return
		}

		ctx := r.Context()

		if _, err := q.GetGoal(ctx, id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "goal not found", log)
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get goal")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get goal", log)
			return
		}

		if err := q.DeactivateGoal(ctx, id); err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to deactivate goal")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to deactivate goal", log)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// ListGoalInstances returns instances for a goal, optionally filtered by week.
func ListGoalInstances(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		goalID, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid goal ID", log)
			return
		}

		ctx := r.Context()

		// Verify goal exists.
		if _, err := q.GetGoal(ctx, goalID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "goal not found", log)
				return
			}
			log.Error().Err(err).Int64("goal_id", goalID).Msg("failed to get goal")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get goal", log)
			return
		}

		weekStart := goal.MondayOf(time.Now())
		if ws := r.URL.Query().Get("week_start"); ws != "" {
			parsed, err := time.Parse("2006-01-02", ws)
			if err != nil {
				RespondError(w, http.StatusBadRequest, "bad_request", "week_start must be YYYY-MM-DD format", log)
				return
			}
			weekStart = goal.MondayOf(parsed)
		}

		instances, err := q.ListGoalInstancesByGoalAndWeek(ctx, sqlcdb.ListGoalInstancesByGoalAndWeekParams{
			GoalID:    goalID,
			WeekStart: weekStart,
		})
		if err != nil {
			log.Error().Err(err).Int64("goal_id", goalID).Msg("failed to list goal instances")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to list goal instances", log)
			return
		}

		RespondJSON(w, http.StatusOK, map[string]any{"instances": instances}, log)
	}
}

// SkipGoalInstance marks an instance as skipped and attempts to reschedule
// it into a remaining gap within the same week.
func SkipGoalInstance(q *sqlcdb.Queries, prefs *preferences.Store, nylasClient action.NylasEventCreator, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		goalID, instanceID, ok := parseGoalInstanceIDs(w, r, log)
		if !ok {
			return
		}

		updated, rescheduled, err := action.SkipGoalInstance(r.Context(), q, prefs, nylasClient, goalID, instanceID, log)
		if err != nil {
			switch {
			case errors.Is(err, action.ErrNotFound):
				RespondError(w, http.StatusNotFound, "not_found", "goal instance not found", log)
			case errors.Is(err, action.ErrInvalidStatus):
				RespondError(w, http.StatusBadRequest, "bad_request", "only scheduled instances can be skipped", log)
			default:
				log.Error().Err(err).Int64("instance_id", instanceID).Msg("failed to skip goal instance")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to skip goal instance", log)
			}
			return
		}

		RespondJSON(w, http.StatusOK, map[string]any{"instance": updated, "rescheduled": rescheduled}, log)
	}
}

// CompleteGoalInstance marks an instance as completed.
func CompleteGoalInstance(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		goalID, instanceID, ok := parseGoalInstanceIDs(w, r, log)
		if !ok {
			return
		}

		updated, err := action.CompleteGoalInstance(r.Context(), q, goalID, instanceID, log)
		if err != nil {
			switch {
			case errors.Is(err, action.ErrNotFound):
				RespondError(w, http.StatusNotFound, "not_found", "goal instance not found", log)
			case errors.Is(err, action.ErrInvalidStatus):
				RespondError(w, http.StatusBadRequest, "bad_request", "only scheduled instances can be completed", log)
			default:
				log.Error().Err(err).Int64("instance_id", instanceID).Msg("failed to complete goal instance")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to complete goal instance", log)
			}
			return
		}

		RespondJSON(w, http.StatusOK, map[string]any{"instance": updated}, log)
	}
}

// parseGoalInstanceIDs extracts and validates goal ID and instance ID from the URL.
func parseGoalInstanceIDs(w http.ResponseWriter, r *http.Request, log zerolog.Logger) (goalID, instanceID int64, ok bool) {
	idStr := chi.URLParam(r, "id")
	goalID, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "bad_request", "invalid goal ID", log)
		return 0, 0, false
	}

	instStr := chi.URLParam(r, "instance_id")
	instanceID, err = strconv.ParseInt(instStr, 10, 64)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "bad_request", "invalid instance ID", log)
		return 0, 0, false
	}

	return goalID, instanceID, true
}
