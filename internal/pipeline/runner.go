package pipeline

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/gap"
	"github.com/robkerr1992/driftcal/internal/goal"
	"github.com/robkerr1992/driftcal/internal/nylas"
	"github.com/robkerr1992/driftcal/internal/preferences"
	"github.com/robkerr1992/driftcal/internal/suggest"
	"github.com/robkerr1992/driftcal/internal/weather"
)

// SyncRunner syncs calendar data from external providers.
type SyncRunner interface {
	SyncAll(ctx context.Context) error
}

// WeatherFetcher retrieves weather forecast data.
type WeatherFetcher interface {
	FetchForecast(ctx context.Context, lat, lon float64) (weather.Forecast, error)
}

// SuggestionGenerator generates activity suggestions via LLM.
type SuggestionGenerator interface {
	Generate(ctx context.Context, systemPrompt, userPrompt string) (*suggest.GenerateResult, error)
}

// NylasEventCreator creates events on an external calendar.
type NylasEventCreator interface {
	CreateEvent(ctx context.Context, grantID, calendarID string, req nylas.CreateEventRequest) (*nylas.Event, error)
}

// Runner orchestrates the daily pipeline: sync → gaps → enrich → goals → suggest.
type Runner struct {
	queries  *sqlcdb.Queries
	syncer   SyncRunner
	prefs    *preferences.Store
	weather  WeatherFetcher
	suggest  SuggestionGenerator
	nylas    NylasEventCreator
	log      zerolog.Logger
}

// New creates a pipeline Runner with all dependencies.
func New(
	queries *sqlcdb.Queries,
	syncer SyncRunner,
	prefs *preferences.Store,
	weather WeatherFetcher,
	suggest SuggestionGenerator,
	nylasClient NylasEventCreator,
	log zerolog.Logger,
) *Runner {
	return &Runner{
		queries: queries,
		syncer:  syncer,
		prefs:   prefs,
		weather: weather,
		suggest: suggest,
		nylas:   nylasClient,
		log:     log,
	}
}

// RunDailyPipeline executes the 5-step daily pipeline for the given target date.
func (r *Runner) RunDailyPipeline(ctx context.Context, targetDate time.Time) error {
	r.log.Info().Time("target_date", targetDate).Msg("pipeline: starting daily run")

	// Create pipeline run record.
	run, err := r.queries.CreatePipelineRun(ctx, sqlcdb.CreatePipelineRunParams{
		RunDate:   targetDate,
		StartedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("creating pipeline run: %w", err)
	}

	// Step 1: Sync (hard-fail).
	if err := r.stepSync(ctx); err != nil {
		r.failRun(ctx, run.ID, fmt.Sprintf("sync: %v", err))
		return fmt.Errorf("pipeline step 1 (sync): %w", err)
	}
	r.completeStep(ctx, run.ID, "sync")

	// Load preferences needed by multiple steps.
	userTZ, err := r.prefs.Timezone(ctx)
	if err != nil {
		r.failRun(ctx, run.ID, fmt.Sprintf("loading timezone: %v", err))
		return fmt.Errorf("loading timezone: %w", err)
	}
	activeStart, _ := r.prefs.ActiveHoursStart(ctx)
	activeEnd, _ := r.prefs.ActiveHoursEnd(ctx)
	energyProfile, _ := r.prefs.EnergyProfile(ctx)

	// Step 2: Compute and store gaps for 3 days (hard-fail).
	if err := r.stepGaps(ctx, targetDate, run.ID, userTZ, activeStart, activeEnd); err != nil {
		r.failRun(ctx, run.ID, fmt.Sprintf("gaps: %v", err))
		return fmt.Errorf("pipeline step 2 (gaps): %w", err)
	}
	r.completeStep(ctx, run.ID, "gaps")

	// Step 3: Weather enrichment (soft-fail).
	forecast := r.stepEnrich(ctx)
	r.completeStep(ctx, run.ID, "enrich")

	// Step 4: Goal scheduling (soft-fail).
	r.stepGoals(ctx, targetDate, userTZ, activeStart, activeEnd, energyProfile)
	r.completeStep(ctx, run.ID, "goals")

	// Step 5: Generate suggestions (hard-fail).
	if err := r.stepSuggest(ctx, targetDate, run.ID, userTZ, activeStart, activeEnd, energyProfile, forecast); err != nil {
		r.failRun(ctx, run.ID, fmt.Sprintf("suggest: %v", err))
		return fmt.Errorf("pipeline step 5 (suggest): %w", err)
	}
	r.completeStep(ctx, run.ID, "suggest")

	// Mark pipeline completed.
	r.queries.CompletePipelineRun(ctx, run.ID)
	r.log.Info().Int64("run_id", run.ID).Msg("pipeline: completed successfully")
	return nil
}

// Step 1: Sync calendar data.
func (r *Runner) stepSync(ctx context.Context) error {
	if r.syncer == nil {
		r.log.Info().Msg("pipeline: syncer not configured, skipping sync")
		return nil
	}
	return r.syncer.SyncAll(ctx)
}

// Step 2: Compute and store gaps for target date + 2 more days.
func (r *Runner) stepGaps(ctx context.Context, targetDate time.Time, runID int64, loc *time.Location, activeStartStr, activeEndStr string) error {
	for dayOffset := 0; dayOffset < 3; dayOffset++ {
		date := targetDate.AddDate(0, 0, dayOffset)
		dayLocal := date.In(loc)

		activeStart, err := gap.ParseHHMM(activeStartStr, dayLocal, loc)
		if err != nil {
			return fmt.Errorf("parsing active start: %w", err)
		}
		activeEnd, err := gap.ParseHHMM(activeEndStr, dayLocal, loc)
		if err != nil {
			return fmt.Errorf("parsing active end: %w", err)
		}

		rangeStart := activeStart.UTC()
		rangeEnd := activeEnd.UTC()

		// Load blocking events.
		events, err := r.queries.ListBlockingEventsInRange(ctx, sqlcdb.ListBlockingEventsInRangeParams{
			RangeStart: rangeStart,
			RangeEnd:   rangeEnd,
		})
		if err != nil {
			return fmt.Errorf("loading events for %s: %w", date.Format("2006-01-02"), err)
		}

		protectedBlocks, err := r.queries.ListActiveProtectedBlocks(ctx)
		if err != nil {
			return fmt.Errorf("loading protected blocks: %w", err)
		}

		goalInstances, err := r.queries.ListScheduledGoalInstancesInRangeWithLabel(ctx, sqlcdb.ListScheduledGoalInstancesInRangeWithLabelParams{
			RangeStart: rangeStart,
			RangeEnd:   rangeEnd,
		})
		if err != nil {
			return fmt.Errorf("loading goal instances: %w", err)
		}

		// Convert to gap types.
		var dayEvents []gap.BlockingEvent
		for _, ev := range events {
			title := ""
			if ev.Title.Valid {
				title = ev.Title.String
			}
			dayEvents = append(dayEvents, gap.BlockingEvent{
				StartTime: ev.StartTime, EndTime: ev.EndTime,
				AllDay: ev.AllDay, Busy: ev.Busy, Title: title,
			})
		}

		dow := int64(dayLocal.Weekday())
		var dayProtected []gap.ProtectedBlock
		for _, pb := range protectedBlocks {
			if pb.DayOfWeek.Valid && pb.DayOfWeek.Int64 != dow {
				continue
			}
			expanded, err := gap.ExpandProtectedBlock(pb, dayLocal.UTC(), loc)
			if err != nil {
				continue
			}
			dayProtected = append(dayProtected, expanded)
		}

		var dayGoals []gap.GoalInstance
		for _, gi := range goalInstances {
			dayGoals = append(dayGoals, gap.GoalInstance{
				StartTime: gi.ScheduledStart, EndTime: gi.ScheduledEnd,
				Label: gi.GoalLabel,
			})
		}

		prefMin, _ := r.prefs.MinGapMinutes(ctx)
		input := gap.Input{
			Date: dayLocal.UTC(), ActiveStart: rangeStart, ActiveEnd: rangeEnd,
			MinGapMinutes: prefMin, BlockingEvents: dayEvents,
			ProtectedBlocks: dayProtected, GoalInstances: dayGoals,
			UserTimezone: loc,
		}

		computed := gap.ComputeGaps(input)

		// Delete existing gaps for this date and insert fresh.
		r.queries.DeleteDailyGapsForDate(ctx, date)
		for _, g := range computed {
			r.queries.CreateDailyGap(ctx, sqlcdb.CreateDailyGapParams{
				GapDate:          date,
				StartTime:        g.StartTime,
				EndTime:          g.EndTime,
				DurationMinutes:  int64(g.DurationMinutes),
				TimeOfDay:        g.TimeOfDay,
				DurationBucket:   g.DurationBucket,
				BeforeEventTitle: toNullString(g.BeforeEventTitle),
				AfterEventTitle:  toNullString(g.AfterEventTitle),
				PipelineRunID:    sql.NullInt64{Int64: runID, Valid: true},
			})
		}

		r.log.Info().Str("date", date.Format("2006-01-02")).Int("gaps", len(computed)).Msg("pipeline: gaps computed")
	}
	return nil
}

// Step 3: Weather enrichment (soft-fail).
func (r *Runner) stepEnrich(ctx context.Context) weather.Forecast {
	if r.weather == nil {
		r.log.Info().Msg("pipeline: weather client not configured, skipping enrichment")
		return weather.Forecast{}
	}

	lat, _ := r.prefs.Latitude(ctx)
	lon, _ := r.prefs.Longitude(ctx)
	if lat == 0 && lon == 0 {
		r.log.Warn().Msg("pipeline: no location configured, skipping weather")
		return weather.Forecast{}
	}

	forecast, err := r.weather.FetchForecast(ctx, lat, lon)
	if err != nil {
		r.log.Warn().Err(err).Msg("pipeline: weather fetch failed (soft-fail)")
		return weather.Forecast{}
	}

	r.log.Info().Str("conditions", forecast.Conditions).Float64("high", forecast.TempHighF).Msg("pipeline: weather enriched")
	return forecast
}

// Step 4: Goal scheduling (soft-fail).
func (r *Runner) stepGoals(ctx context.Context, targetDate time.Time, loc *time.Location, activeStart, activeEnd string, energyProfile map[string]string) {
	weekStart := goal.MondayOf(targetDate)
	weekEnd := weekStart.AddDate(0, 0, 7)

	goals, err := r.queries.ListActiveGoals(ctx)
	if err != nil {
		r.log.Warn().Err(err).Msg("pipeline: failed to load goals (soft-fail)")
		return
	}
	if len(goals) == 0 {
		return
	}

	existingInstances, err := r.queries.ListScheduledGoalInstancesInRange(ctx, sqlcdb.ListScheduledGoalInstancesInRangeParams{
		RangeStart: weekStart,
		RangeEnd:   weekEnd,
	})
	if err != nil {
		r.log.Warn().Err(err).Msg("pipeline: failed to load existing instances (soft-fail)")
		return
	}

	events, err := r.queries.ListBlockingEventsInRange(ctx, sqlcdb.ListBlockingEventsInRangeParams{
		RangeStart: weekStart,
		RangeEnd:   weekEnd,
	})
	if err != nil {
		r.log.Warn().Err(err).Msg("pipeline: failed to load events (soft-fail)")
		return
	}

	var blockingEvents []gap.BlockingEvent
	for _, ev := range events {
		title := ""
		if ev.Title.Valid {
			title = ev.Title.String
		}
		blockingEvents = append(blockingEvents, gap.BlockingEvent{
			StartTime: ev.StartTime, EndTime: ev.EndTime,
			AllDay: ev.AllDay, Busy: ev.Busy, Title: title,
		})
	}

	protectedBlocks, _ := r.queries.ListActiveProtectedBlocks(ctx)
	var allProtected []gap.ProtectedBlock
	for dayOffset := 0; dayOffset < 7; dayOffset++ {
		day := weekStart.AddDate(0, 0, dayOffset)
		dayLocal := day.In(loc)
		dow := int64(dayLocal.Weekday())
		for _, pb := range protectedBlocks {
			if pb.DayOfWeek.Valid && pb.DayOfWeek.Int64 != dow {
				continue
			}
			expanded, err := gap.ExpandProtectedBlock(pb, day, loc)
			if err != nil {
				continue
			}
			allProtected = append(allProtected, expanded)
		}
	}

	schedInput := goal.ScheduleInput{
		Goals: goals, ExistingInstances: existingInstances,
		BlockingEvents: blockingEvents, ProtectedBlocks: allProtected,
		WeekStart: weekStart, ActiveStartStr: activeStart, ActiveEndStr: activeEnd,
		UserTimezone: loc, EnergyProfile: energyProfile,
	}

	result, err := goal.Schedule(schedInput)
	if err != nil {
		r.log.Warn().Err(err).Msg("pipeline: goal scheduling failed (soft-fail)")
		return
	}

	// Create instances for placements.
	for _, p := range result.Placements {
		instance, err := r.queries.CreateGoalInstance(ctx, sqlcdb.CreateGoalInstanceParams{
			GoalID: p.GoalID, WeekStart: weekStart,
			ScheduledStart: p.ScheduledStart, ScheduledEnd: p.ScheduledEnd,
		})
		if err != nil {
			r.log.Warn().Err(err).Int64("goal_id", p.GoalID).Msg("pipeline: failed to create goal instance")
			continue
		}

		// Push to Nylas if configured.
		if r.nylas != nil {
			r.pushGoalToNylas(ctx, instance, p.GoalID)
		}
	}

	r.log.Info().Int("placed", len(result.Placements)).Int("unfulfilled", len(result.Unfulfilled)).Msg("pipeline: goals scheduled")
}

// Step 5: Generate suggestions.
func (r *Runner) stepSuggest(
	ctx context.Context, targetDate time.Time, runID int64,
	loc *time.Location, activeStartStr, activeEndStr string,
	energyProfile map[string]string, forecast weather.Forecast,
) error {
	if r.suggest == nil {
		r.log.Info().Msg("pipeline: suggestion client not configured, skipping")
		return nil
	}

	// Idempotency check: skip if pending suggestions exist.
	count, err := r.queries.CountPendingSuggestionsForDate(ctx, targetDate)
	if err != nil {
		return fmt.Errorf("checking existing suggestions: %w", err)
	}
	if count > 0 {
		r.log.Info().Int64("existing_count", count).Msg("pipeline: pending suggestions exist, skipping")
		return nil
	}

	// Re-compute gaps for tomorrow (post-goal-placement).
	dayLocal := targetDate.In(loc)
	activeStart, err := gap.ParseHHMM(activeStartStr, dayLocal, loc)
	if err != nil {
		return fmt.Errorf("parsing active start: %w", err)
	}
	activeEnd, err := gap.ParseHHMM(activeEndStr, dayLocal, loc)
	if err != nil {
		return fmt.Errorf("parsing active end: %w", err)
	}

	rangeStart := activeStart.UTC()
	rangeEnd := activeEnd.UTC()

	events, _ := r.queries.ListBlockingEventsInRange(ctx, sqlcdb.ListBlockingEventsInRangeParams{
		RangeStart: rangeStart, RangeEnd: rangeEnd,
	})
	goalInstances, _ := r.queries.ListScheduledGoalInstancesInRangeWithLabel(ctx, sqlcdb.ListScheduledGoalInstancesInRangeWithLabelParams{
		RangeStart: rangeStart, RangeEnd: rangeEnd,
	})
	protectedBlocks, _ := r.queries.ListActiveProtectedBlocks(ctx)

	var dayEvents []gap.BlockingEvent
	for _, ev := range events {
		title := ""
		if ev.Title.Valid {
			title = ev.Title.String
		}
		dayEvents = append(dayEvents, gap.BlockingEvent{
			StartTime: ev.StartTime, EndTime: ev.EndTime,
			AllDay: ev.AllDay, Busy: ev.Busy, Title: title,
		})
	}

	dow := int64(dayLocal.Weekday())
	var dayProtected []gap.ProtectedBlock
	for _, pb := range protectedBlocks {
		if pb.DayOfWeek.Valid && pb.DayOfWeek.Int64 != dow {
			continue
		}
		expanded, err := gap.ExpandProtectedBlock(pb, dayLocal.UTC(), loc)
		if err != nil {
			continue
		}
		dayProtected = append(dayProtected, expanded)
	}

	var dayGoals []gap.GoalInstance
	for _, gi := range goalInstances {
		dayGoals = append(dayGoals, gap.GoalInstance{
			StartTime: gi.ScheduledStart, EndTime: gi.ScheduledEnd,
			Label: gi.GoalLabel,
		})
	}

	prefMin, _ := r.prefs.MinGapMinutes(ctx)
	gapInput := gap.Input{
		Date: dayLocal.UTC(), ActiveStart: rangeStart, ActiveEnd: rangeEnd,
		MinGapMinutes: prefMin, BlockingEvents: dayEvents,
		ProtectedBlocks: dayProtected, GoalInstances: dayGoals,
		UserTimezone: loc,
	}

	computed := gap.ComputeGaps(gapInput)
	if len(computed) == 0 {
		r.log.Info().Msg("pipeline: no gaps for suggestions")
		return nil
	}

	// Build prompt input.
	var gapContexts []suggest.GapContext
	for i, g := range computed {
		gapContexts = append(gapContexts, suggest.GapContext{
			Number:          i + 1,
			StartTime:       g.StartTime.In(loc).Format("15:04"),
			EndTime:         g.EndTime.In(loc).Format("15:04"),
			DurationMinutes: g.DurationMinutes,
			TimeOfDay:       g.TimeOfDay,
			BeforeEvent:     g.BeforeEventTitle,
			AfterEvent:      g.AfterEventTitle,
		})
	}

	var goalContexts []suggest.GoalContext
	for _, gi := range goalInstances {
		goalContexts = append(goalContexts, suggest.GoalContext{
			StartTime:   gi.ScheduledStart.In(loc).Format("15:04"),
			EndTime:     gi.ScheduledEnd.In(loc).Format("15:04"),
			Label:       gi.GoalLabel,
			Category:    "", // goal instances don't carry category in the join
			EnergyLevel: "",
		})
	}

	// Load recent history.
	lookback := targetDate.AddDate(0, 0, -14)
	recentSuggestions, _ := r.queries.ListRecentSuggestions(ctx, lookback)
	var history []suggest.HistoryItem
	for _, s := range recentSuggestions {
		history = append(history, suggest.HistoryItem{
			Date:     s.SuggestedDate.Format("2006-01-02"),
			Title:    s.Title,
			Category: s.Category,
			Status:   s.Status,
		})
	}

	allPrefs, _ := r.prefs.All(ctx)

	promptInput := suggest.PromptInput{
		Date: targetDate, Gaps: gapContexts, Weather: forecast,
		Preferences: allPrefs, EnergyProfile: energyProfile,
		ScheduledGoals: goalContexts, RecentHistory: history,
	}

	systemPrompt, userPrompt := suggest.BuildPrompt(promptInput)

	result, err := r.suggest.Generate(ctx, systemPrompt, userPrompt)
	if err != nil {
		return fmt.Errorf("generating suggestions: %w", err)
	}

	// Map suggestions to gap times and store.
	for _, s := range result.Suggestions {
		idx := s.GapNumber - 1
		if idx < 0 || idx >= len(computed) {
			r.log.Warn().Int("gap_number", s.GapNumber).Msg("pipeline: suggestion gap_number out of range")
			continue
		}

		g := computed[idx]
		weatherCtx := ""
		if forecast.Conditions != "" {
			b, _ := json.Marshal(forecast)
			weatherCtx = string(b)
		}

		r.queries.CreateActivitySuggestion(ctx, sqlcdb.CreateActivitySuggestionParams{
			SuggestedDate:  targetDate,
			StartTime:      g.StartTime,
			EndTime:        g.EndTime,
			Title:          s.Title,
			Description:    s.Description,
			Category:       s.Category,
			EnergyLevel:    s.EnergyLevel,
			EstimatedCost:  s.EstimatedCost,
			Location:       toNullString(s.Location),
			LocationUrl:    toNullString(s.LocationURL),
			WeatherContext: toNullString(weatherCtx),
			Reasoning:      toNullString(s.Reasoning),
			LlmRequestID:   toNullString(result.RequestID),
		})
	}

	r.log.Info().Int("suggestions", len(result.Suggestions)).
		Int("input_tokens", result.InputTokens).
		Int("output_tokens", result.OutputTokens).
		Msg("pipeline: suggestions generated")

	return nil
}

func (r *Runner) pushGoalToNylas(ctx context.Context, instance sqlcdb.GoalInstance, goalID int64) {
	grantID, _ := r.prefs.Get(ctx, "nylas_grant_id")
	calendarID, _ := r.prefs.Get(ctx, "nylas_calendar_id")
	if grantID == "" || calendarID == "" {
		return
	}

	g, err := r.queries.GetGoal(ctx, goalID)
	if err != nil {
		return
	}

	evt, err := r.nylas.CreateEvent(ctx, grantID, calendarID, nylas.CreateEventRequest{
		Title: g.Label,
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: instance.ScheduledStart.Unix(),
			EndTime:   instance.ScheduledEnd.Unix(),
		},
		Busy: true,
	})
	if err != nil {
		r.log.Warn().Err(err).Int64("instance_id", instance.ID).Msg("pipeline: failed to push goal to Nylas")
		return
	}

	r.queries.UpdateGoalInstanceNylasEventID(ctx, sqlcdb.UpdateGoalInstanceNylasEventIDParams{
		NylasEventID: sql.NullString{String: evt.ID, Valid: true},
		ID:           instance.ID,
	})
}

func (r *Runner) completeStep(ctx context.Context, runID int64, step string) {
	r.queries.UpdatePipelineRunStep(ctx, sqlcdb.UpdatePipelineRunStepParams{
		LastCompletedStep: sql.NullString{String: step, Valid: true},
		Metrics:           sql.NullString{},
		ID:                runID,
	})
}

func (r *Runner) failRun(ctx context.Context, runID int64, errMsg string) {
	r.queries.FailPipelineRun(ctx, sqlcdb.FailPipelineRunParams{
		ErrorMessage: sql.NullString{String: errMsg, Valid: true},
		ID:           runID,
	})
}

func toNullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}
