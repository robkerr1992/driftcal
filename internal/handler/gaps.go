package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/gap"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

const maxGapRangeDays = 90

// GapsHandler computes free gaps for a date range on-the-fly.
func GapsHandler(q *sqlcdb.Queries, prefs *preferences.Store, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Parse required query params.
		startStr := r.URL.Query().Get("start")
		endStr := r.URL.Query().Get("end")
		if startStr == "" || endStr == "" {
			RespondError(w, http.StatusBadRequest, "bad_request", "start and end query parameters are required", log)
			return
		}

		rangeStart, err := time.Parse(time.RFC3339, startStr)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "start must be ISO 8601 format", log)
			return
		}
		rangeEnd, err := time.Parse(time.RFC3339, endStr)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "end must be ISO 8601 format", log)
			return
		}

		if rangeEnd.Before(rangeStart) || rangeEnd.Equal(rangeStart) {
			RespondError(w, http.StatusBadRequest, "bad_request", "end must be after start", log)
			return
		}

		if rangeEnd.After(rangeStart.AddDate(0, 0, maxGapRangeDays)) {
			RespondError(w, http.StatusBadRequest, "bad_request", "date range must not exceed 90 days", log)
			return
		}

		// Parse optional min_minutes. Track whether it was explicitly provided
		// so that min_minutes=0 (show all gaps) is distinguishable from "not provided"
		// (fall back to preference default).
		minMinutes := 0
		minMinutesProvided := r.URL.Query().Has("min_minutes")
		if minMinutesProvided {
			mm, err := strconv.Atoi(r.URL.Query().Get("min_minutes"))
			if err != nil || mm < 0 {
				RespondError(w, http.StatusBadRequest, "bad_request", "min_minutes must be a non-negative integer", log)
				return
			}
			minMinutes = mm
		}

		// Load preferences.
		userTZ, err := prefs.Timezone(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to load timezone")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load preferences", log)
			return
		}
		activeStartStr, err := prefs.ActiveHoursStart(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to load active_hours_start")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load preferences", log)
			return
		}
		activeEndStr, err := prefs.ActiveHoursEnd(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to load active_hours_end")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load preferences", log)
			return
		}

		// Use preference default if min_minutes not specified in the query string.
		if !minMinutesProvided {
			prefMin, err := prefs.MinGapMinutes(ctx)
			if err != nil {
				log.Error().Err(err).Msg("failed to load min_gap_minutes")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load preferences", log)
				return
			}
			minMinutes = prefMin
		}

		computeStart := time.Now()

		// Batch-query all blocking events for the entire range.
		events, err := q.ListBlockingEventsInRange(ctx, sqlcdb.ListBlockingEventsInRangeParams{
			RangeStart: rangeStart,
			RangeEnd:   rangeEnd,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to list blocking events")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load events", log)
			return
		}

		protectedBlocks, err := q.ListActiveProtectedBlocks(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to list protected blocks")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load protected blocks", log)
			return
		}

		goalInstances, err := q.ListScheduledGoalInstancesInRangeWithLabel(ctx, sqlcdb.ListScheduledGoalInstancesInRangeWithLabelParams{
			RangeStart: rangeStart,
			RangeEnd:   rangeEnd,
		})
		if err != nil {
			log.Error().Err(err).Msg("failed to list goal instances")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load goal instances", log)
			return
		}

		// Iterate each day in the range, computing gaps per-day.
		var allGaps []gap.Gap
		dateCount := 0

		// Walk dates in the user's timezone.
		current := rangeStart.In(userTZ)
		endLocal := rangeEnd.In(userTZ)

		for current.Before(endLocal) {
			dateCount++
			y, m, d := current.Date()
			dayStart := time.Date(y, m, d, 0, 0, 0, 0, userTZ)
			dayEnd := dayStart.AddDate(0, 0, 1)

			// Active hours for this day (in UTC).
			activeStart, err := gap.ParseHHMM(activeStartStr, dayStart, userTZ)
			if err != nil {
				log.Error().Err(err).Str("active_hours_start", activeStartStr).Msg("invalid active_hours_start")
				RespondError(w, http.StatusInternalServerError, "internal_error", "invalid active hours configuration", log)
				return
			}
			activeStart = activeStart.UTC()
			activeEnd, err := gap.ParseHHMM(activeEndStr, dayStart, userTZ)
			if err != nil {
				log.Error().Err(err).Str("active_hours_end", activeEndStr).Msg("invalid active_hours_end")
				RespondError(w, http.StatusInternalServerError, "internal_error", "invalid active hours configuration", log)
				return
			}
			activeEnd = activeEnd.UTC()

			// Partition events to this day.
			var dayEvents []gap.BlockingEvent
			for _, ev := range events {
				if ev.EndTime.After(activeStart) && ev.StartTime.Before(activeEnd) {
					title := ""
					if ev.Title.Valid {
						title = ev.Title.String
					}
					dayEvents = append(dayEvents, gap.BlockingEvent{
						StartTime: ev.StartTime,
						EndTime:   ev.EndTime,
						AllDay:    ev.AllDay,
						Busy:      ev.Busy,
						Title:     title,
					})
				}
			}

			// Partition protected blocks: match day_of_week, expand to UTC.
			dow := int64(dayStart.Weekday()) // Sunday=0
			var dayProtected []gap.ProtectedBlock
			for _, pb := range protectedBlocks {
				if pb.DayOfWeek.Valid && pb.DayOfWeek.Int64 != dow {
					continue
				}
				expanded, err := gap.ExpandProtectedBlock(pb, dayStart.UTC(), userTZ)
				if err != nil {
					log.Error().Err(err).Int64("block_id", pb.ID).Msg("failed to expand protected block")
					continue
				}
				dayProtected = append(dayProtected, expanded)
			}

			// Partition goal instances to this day.
			var dayGoals []gap.GoalInstance
			for _, gi := range goalInstances {
				if gi.ScheduledEnd.After(activeStart) && gi.ScheduledStart.Before(activeEnd) {
					dayGoals = append(dayGoals, gap.GoalInstance{
						StartTime: gi.ScheduledStart,
						EndTime:   gi.ScheduledEnd,
						Label:     gi.GoalLabel,
					})
				}
			}

			input := gap.Input{
				Date:            dayStart.UTC(),
				ActiveStart:     activeStart,
				ActiveEnd:       activeEnd,
				MinGapMinutes:   minMinutes,
				BlockingEvents:  dayEvents,
				ProtectedBlocks: dayProtected,
				GoalInstances:   dayGoals,
				UserTimezone:    userTZ,
			}

			dayGaps := gap.ComputeGaps(input)
			allGaps = append(allGaps, dayGaps...)

			current = dayEnd
		}

		if allGaps == nil {
			allGaps = []gap.Gap{}
		}

		log.Info().
			Int("dates", dateCount).
			Int("gaps", len(allGaps)).
			Dur("compute_time", time.Since(computeStart)).
			Msg("gap computation complete")

		RespondJSON(w, http.StatusOK, map[string]any{"gaps": allGaps}, log)
	}
}

