package action

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/gap"
	"github.com/robkerr1992/driftcal/internal/goal"
	"github.com/robkerr1992/driftcal/internal/nylas"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

// NylasEventCreator abstracts Nylas event creation for testability.
// A nil value is safe — callers should check before use.
type NylasEventCreator interface {
	CreateEvent(ctx context.Context, grantID, calendarID string, req nylas.CreateEventRequest) (*nylas.Event, error)
}

// ErrNotFound indicates the requested resource does not exist.
var ErrNotFound = errors.New("not found")

// ErrInvalidStatus indicates the resource is not in a valid state for the operation.
var ErrInvalidStatus = errors.New("invalid status")

// ErrConflict indicates the time slot now has a conflicting event.
var ErrConflict = errors.New("time slot conflict")

// ApproveSuggestion transitions a pending suggestion to approved, records feedback,
// and optionally pushes it to Nylas. Returns the updated suggestion.
func ApproveSuggestion(ctx context.Context, q *sqlcdb.Queries, nylasClient NylasEventCreator, id int64, log zerolog.Logger) (sqlcdb.ActivitySuggestion, error) {
	suggestion, err := q.GetActivitySuggestion(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlcdb.ActivitySuggestion{}, fmt.Errorf("suggestion %d: %w", id, ErrNotFound)
		}
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("getting suggestion %d: %w", id, err)
	}

	if suggestion.Status != "pending" {
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("suggestion %d is %s: %w", id, suggestion.Status, ErrInvalidStatus)
	}

	// Conflict re-check: verify no blocking events now overlap this time slot.
	conflictCount, err := q.CountBlockingEventsInRange(ctx, sqlcdb.CountBlockingEventsInRangeParams{
		RangeEnd:   suggestion.EndTime,
		RangeStart: suggestion.StartTime,
	})
	if err != nil {
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("checking conflicts for suggestion %d: %w", id, err)
	}
	if conflictCount > 0 {
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("suggestion %d: %w", id, ErrConflict)
	}

	updated, err := q.UpdateSuggestionStatus(ctx, sqlcdb.UpdateSuggestionStatusParams{
		Status: "approved",
		ID:     id,
	})
	if err != nil {
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("approving suggestion %d: %w", id, err)
	}

	if _, err := q.CreateSuggestionFeedback(ctx, sqlcdb.CreateSuggestionFeedbackParams{
		SuggestionID: id,
		Action:       "approved",
	}); err != nil {
		log.Error().Err(err).Int64("suggestion_id", id).Msg("failed to create feedback record")
	}

	if nylasClient != nil {
		PushSuggestionToNylas(ctx, q, nylasClient, updated, log)
	}

	return updated, nil
}

// RejectSuggestion transitions a pending suggestion to rejected, records feedback
// with optional notes. Returns the updated suggestion.
func RejectSuggestion(ctx context.Context, q *sqlcdb.Queries, id int64, notes string, log zerolog.Logger) (sqlcdb.ActivitySuggestion, error) {
	suggestion, err := q.GetActivitySuggestion(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlcdb.ActivitySuggestion{}, fmt.Errorf("suggestion %d: %w", id, ErrNotFound)
		}
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("getting suggestion %d: %w", id, err)
	}

	if suggestion.Status != "pending" {
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("suggestion %d is %s: %w", id, suggestion.Status, ErrInvalidStatus)
	}

	updated, err := q.UpdateSuggestionStatus(ctx, sqlcdb.UpdateSuggestionStatusParams{
		Status: "rejected",
		ID:     id,
	})
	if err != nil {
		return sqlcdb.ActivitySuggestion{}, fmt.Errorf("rejecting suggestion %d: %w", id, err)
	}

	if _, err := q.CreateSuggestionFeedback(ctx, sqlcdb.CreateSuggestionFeedbackParams{
		SuggestionID: id,
		Action:       "rejected",
		EditNotes:    sql.NullString{String: notes, Valid: notes != ""},
	}); err != nil {
		log.Error().Err(err).Int64("suggestion_id", id).Msg("failed to create feedback record")
	}

	return updated, nil
}

// SkipGoalInstance marks a scheduled instance as skipped and attempts to reschedule
// it within the same week. Returns the skipped instance and optionally a rescheduled instance.
func SkipGoalInstance(
	ctx context.Context,
	q *sqlcdb.Queries,
	prefs *preferences.Store,
	nylasClient NylasEventCreator,
	goalID, instanceID int64,
	log zerolog.Logger,
) (skipped sqlcdb.GoalInstance, rescheduled *sqlcdb.GoalInstance, err error) {
	instance, err := q.GetGoalInstance(ctx, instanceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlcdb.GoalInstance{}, nil, fmt.Errorf("goal instance %d: %w", instanceID, ErrNotFound)
		}
		return sqlcdb.GoalInstance{}, nil, fmt.Errorf("getting goal instance %d: %w", instanceID, err)
	}

	if instance.GoalID != goalID {
		return sqlcdb.GoalInstance{}, nil, fmt.Errorf("goal instance %d: %w", instanceID, ErrNotFound)
	}

	if instance.Status != "scheduled" {
		return sqlcdb.GoalInstance{}, nil, fmt.Errorf("instance %d is %s: %w", instanceID, instance.Status, ErrInvalidStatus)
	}

	updated, err := q.UpdateGoalInstanceStatus(ctx, sqlcdb.UpdateGoalInstanceStatusParams{
		Status: "skipped",
		ID:     instanceID,
	})
	if err != nil {
		return sqlcdb.GoalInstance{}, nil, fmt.Errorf("skipping instance %d: %w", instanceID, err)
	}

	rescheduled = TryReschedule(ctx, q, prefs, nylasClient, instance, log)
	return updated, rescheduled, nil
}

// CompleteGoalInstance marks a scheduled instance as completed.
func CompleteGoalInstance(ctx context.Context, q *sqlcdb.Queries, goalID, instanceID int64, log zerolog.Logger) (sqlcdb.GoalInstance, error) {
	instance, err := q.GetGoalInstance(ctx, instanceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sqlcdb.GoalInstance{}, fmt.Errorf("goal instance %d: %w", instanceID, ErrNotFound)
		}
		return sqlcdb.GoalInstance{}, fmt.Errorf("getting goal instance %d: %w", instanceID, err)
	}

	if instance.GoalID != goalID {
		return sqlcdb.GoalInstance{}, fmt.Errorf("goal instance %d: %w", instanceID, ErrNotFound)
	}

	if instance.Status != "scheduled" {
		return sqlcdb.GoalInstance{}, fmt.Errorf("instance %d is %s: %w", instanceID, instance.Status, ErrInvalidStatus)
	}

	updated, err := q.UpdateGoalInstanceStatus(ctx, sqlcdb.UpdateGoalInstanceStatusParams{
		Status: "completed",
		ID:     instanceID,
	})
	if err != nil {
		return sqlcdb.GoalInstance{}, fmt.Errorf("completing instance %d: %w", instanceID, err)
	}

	return updated, nil
}

// TryReschedule attempts to find a new slot for a skipped instance in the
// remainder of the week. Returns the new instance or nil.
func TryReschedule(
	ctx context.Context,
	q *sqlcdb.Queries,
	prefs *preferences.Store,
	nylasClient NylasEventCreator,
	skipped sqlcdb.GoalInstance,
	log zerolog.Logger,
) *sqlcdb.GoalInstance {
	g, err := q.GetGoal(ctx, skipped.GoalID)
	if err != nil {
		log.Warn().Err(err).Int64("goal_id", skipped.GoalID).Msg("reschedule: failed to load goal")
		return nil
	}

	userTZ, err := prefs.Timezone(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to load timezone")
		return nil
	}
	activeStartStr, err := prefs.ActiveHoursStart(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to load active_hours_start")
		return nil
	}
	activeEndStr, err := prefs.ActiveHoursEnd(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to load active_hours_end")
		return nil
	}

	energyProfile := make(map[string]string)
	if epStr, err := prefs.Get(ctx, "energy_profile"); err == nil && epStr != "" {
		_ = json.Unmarshal([]byte(epStr), &energyProfile)
	}

	weekStart := skipped.WeekStart
	weekEnd := weekStart.AddDate(0, 0, 7)

	events, err := q.ListBlockingEventsInRange(ctx, sqlcdb.ListBlockingEventsInRangeParams{
		RangeStart: weekStart,
		RangeEnd:   weekEnd,
	})
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to load events")
		return nil
	}

	protectedBlocks, err := q.ListActiveProtectedBlocks(ctx)
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to load protected blocks")
		return nil
	}

	existingInstances, err := q.ListScheduledGoalInstancesInRange(ctx, sqlcdb.ListScheduledGoalInstancesInRangeParams{
		RangeStart: weekStart,
		RangeEnd:   weekEnd,
	})
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to load goal instances")
		return nil
	}

	blockingEvents := gap.ConvertBlockingEvents(events)

	var allProtected []gap.ProtectedBlock
	for dayOffset := 0; dayOffset < 7; dayOffset++ {
		day := weekStart.AddDate(0, 0, dayOffset)
		allProtected = append(allProtected, gap.ExpandProtectedBlocksForDate(protectedBlocks, day, userTZ)...)
	}

	schedInput := goal.ScheduleInput{
		Goals:             []sqlcdb.RecurringGoal{g},
		ExistingInstances: existingInstances,
		BlockingEvents:    blockingEvents,
		ProtectedBlocks:   allProtected,
		WeekStart:         weekStart,
		ActiveStartStr:    activeStartStr,
		ActiveEndStr:      activeEndStr,
		UserTimezone:      userTZ,
		EnergyProfile:     energyProfile,
	}

	gaps, err := goal.BuildWeekGaps(schedInput)
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to compute week gaps")
		return nil
	}

	placement := goal.FindNextSlot(g, skipped.ScheduledEnd, weekEnd, gaps, existingInstances, energyProfile, userTZ)
	if placement == nil {
		log.Info().Int64("goal_id", g.ID).Msg("reschedule: no available slot found")
		return nil
	}

	newInstance, err := q.CreateGoalInstance(ctx, sqlcdb.CreateGoalInstanceParams{
		GoalID:         g.ID,
		WeekStart:      weekStart,
		ScheduledStart: placement.ScheduledStart,
		ScheduledEnd:   placement.ScheduledEnd,
	})
	if err != nil {
		log.Warn().Err(err).Msg("reschedule: failed to create new instance")
		return nil
	}

	log.Info().
		Int64("goal_id", g.ID).
		Time("new_start", placement.ScheduledStart).
		Time("new_end", placement.ScheduledEnd).
		Msg("reschedule: new instance created")

	if nylasClient != nil {
		PushGoalInstanceToNylas(ctx, q, nylasClient, prefs, newInstance, g.Label, log)
	}

	return &newInstance
}

// PushSuggestionToNylas creates a Nylas calendar event for an approved suggestion.
// Best-effort: failures are logged but not propagated.
func PushSuggestionToNylas(ctx context.Context, q *sqlcdb.Queries, nylasClient NylasEventCreator, s sqlcdb.ActivitySuggestion, log zerolog.Logger) {
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

// PushGoalInstanceToNylas creates a Nylas calendar event for a goal instance.
// Best-effort: failures are logged but not propagated.
func PushGoalInstanceToNylas(
	ctx context.Context,
	q *sqlcdb.Queries,
	nylasClient NylasEventCreator,
	prefs *preferences.Store,
	instance sqlcdb.GoalInstance,
	label string,
	log zerolog.Logger,
) {
	grantID, _ := prefs.Get(ctx, "nylas_grant_id")
	calendarID, _ := prefs.Get(ctx, "nylas_calendar_id")
	if grantID == "" || calendarID == "" {
		return
	}

	evt, err := nylasClient.CreateEvent(ctx, grantID, calendarID, nylas.CreateEventRequest{
		Title: label,
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: instance.ScheduledStart.Unix(),
			EndTime:   instance.ScheduledEnd.Unix(),
		},
		Busy: true,
	})
	if err != nil {
		log.Warn().Err(err).Int64("instance_id", instance.ID).Msg("failed to push goal instance to Nylas")
		return
	}

	if _, err := q.UpdateGoalInstanceNylasEventID(ctx, sqlcdb.UpdateGoalInstanceNylasEventIDParams{
		NylasEventID: sql.NullString{String: evt.ID, Valid: true},
		ID:           instance.ID,
	}); err != nil {
		log.Warn().Err(err).Int64("instance_id", instance.ID).Msg("failed to update nylas_event_id")
	}
}
