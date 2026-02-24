package action

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/nylas"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

// --- Mock Nylas client ---

type mockNylas struct {
	event   *nylas.Event
	err     error
	called  bool
	lastReq nylas.CreateEventRequest
}

func (m *mockNylas) CreateEvent(ctx context.Context, grantID, calendarID string, req nylas.CreateEventRequest) (*nylas.Event, error) {
	m.called = true
	m.lastReq = req
	return m.event, m.err
}

// --- Test helpers ---

func setup(t *testing.T) (*sqlcdb.Queries, *preferences.Store) {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	prefs.Set(t.Context(), "timezone", "UTC")
	prefs.Set(t.Context(), "active_hours_start", "07:00")
	prefs.Set(t.Context(), "active_hours_end", "22:00")
	return q, prefs
}

func createSuggestion(t *testing.T, q *sqlcdb.Queries) sqlcdb.ActivitySuggestion {
	t.Helper()
	tomorrow := time.Now().UTC().AddDate(0, 0, 1)
	tomorrow = time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 0, 0, 0, 0, time.UTC)

	s, err := q.CreateActivitySuggestion(t.Context(), sqlcdb.CreateActivitySuggestionParams{
		SuggestedDate: tomorrow,
		StartTime:     tomorrow.Add(9 * time.Hour),
		EndTime:       tomorrow.Add(10 * time.Hour),
		Title:         "Morning Walk",
		Description:   "Take a walk in the park",
		Category:      "outdoor",
		EnergyLevel:   "low",
		EstimatedCost: "free",
		Location:      sql.NullString{String: "Central Park", Valid: true},
	})
	if err != nil {
		t.Fatalf("creating test suggestion: %v", err)
	}
	return s
}

func createGoalAndInstance(t *testing.T, q *sqlcdb.Queries) (sqlcdb.RecurringGoal, sqlcdb.GoalInstance) {
	t.Helper()

	g, err := q.CreateGoal(t.Context(), sqlcdb.CreateGoalParams{
		Label:              "Study Go",
		Category:           "study",
		DurationMinutes:    60,
		TimesPerWeek:       3,
		EnergyLevel:        "medium",
		EarliestHour:       "07:00",
		LatestHour:         "22:00",
		MinGapBetweenHours: 24,
		Priority:           3,
	})
	if err != nil {
		t.Fatalf("creating test goal: %v", err)
	}

	now := time.Now().UTC()
	weekStart := now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
	weekStart = time.Date(weekStart.Year(), weekStart.Month(), weekStart.Day(), 0, 0, 0, 0, time.UTC)

	instance, err := q.CreateGoalInstance(t.Context(), sqlcdb.CreateGoalInstanceParams{
		GoalID:         g.ID,
		WeekStart:      weekStart,
		ScheduledStart: now.Add(2 * time.Hour),
		ScheduledEnd:   now.Add(3 * time.Hour),
	})
	if err != nil {
		t.Fatalf("creating test goal instance: %v", err)
	}
	return g, instance
}

// --- Approve tests ---

func TestApproveSuggestion_Success(t *testing.T) {
	q, _ := setup(t)
	s := createSuggestion(t, q)
	log := zerolog.Nop()

	mn := &mockNylas{event: &nylas.Event{ID: "evt-1"}}
	// Set up Nylas prefs so the push works.
	q.UpsertPreference(t.Context(), sqlcdb.UpsertPreferenceParams{Key: "nylas_grant_id", Value: "grant-1"})
	q.UpsertPreference(t.Context(), sqlcdb.UpsertPreferenceParams{Key: "nylas_calendar_id", Value: "cal-1"})

	updated, err := ApproveSuggestion(t.Context(), q, mn, s.ID, log)
	if err != nil {
		t.Fatalf("ApproveSuggestion: %v", err)
	}
	if updated.Status != "approved" {
		t.Errorf("Status = %q, want approved", updated.Status)
	}
	if !mn.called {
		t.Error("expected Nylas CreateEvent to be called")
	}
}

func TestApproveSuggestion_Conflict(t *testing.T) {
	q, _ := setup(t)
	s := createSuggestion(t, q)
	log := zerolog.Nop()

	// Create a blocking event that overlaps the suggestion time.
	acct, err := q.CreateAccount(t.Context(), sqlcdb.CreateAccountParams{
		NylasGrantID: "grant-1", Provider: "google", Email: "test@test.com",
		DisplayName: sql.NullString{String: "Test", Valid: true},
	})
	if err != nil {
		t.Fatalf("creating account: %v", err)
	}
	cal, err := q.CreateCalendar(t.Context(), sqlcdb.CreateCalendarParams{
		AccountID: acct.ID, NylasCalendarID: "cal-nylas-1", Name: "Main",
		Color: sql.NullString{String: "#000", Valid: true},
	})
	if err != nil {
		t.Fatalf("creating calendar: %v", err)
	}
	// Mark the calendar as blocking and active.
	q.UpdateCalendarFlags(t.Context(), sqlcdb.UpdateCalendarFlagsParams{
		IsBlocking: true, IsActive: true, ID: cal.ID,
	})
	// Insert a conflicting event.
	q.UpsertEvent(t.Context(), sqlcdb.UpsertEventParams{
		CalendarID:   cal.ID,
		NylasEventID: "evt-conflict",
		Title:        sql.NullString{String: "Meeting", Valid: true},
		StartTime:    s.StartTime.Add(-30 * time.Minute),
		EndTime:      s.StartTime.Add(30 * time.Minute), // overlaps
		Status:       "confirmed",
		Busy:         "busy",
	})

	_, err = ApproveSuggestion(t.Context(), q, nil, s.ID, log)
	if !errors.Is(err, ErrConflict) {
		t.Errorf("expected ErrConflict, got %v", err)
	}
}

func TestApproveSuggestion_AlreadyApproved(t *testing.T) {
	q, _ := setup(t)
	s := createSuggestion(t, q)
	log := zerolog.Nop()

	// Approve first time.
	_, err := ApproveSuggestion(t.Context(), q, nil, s.ID, log)
	if err != nil {
		t.Fatalf("first approve: %v", err)
	}

	// Second approve should return ErrInvalidStatus, not crash.
	_, err = ApproveSuggestion(t.Context(), q, nil, s.ID, log)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got %v", err)
	}
}

func TestApproveSuggestion_NylasError(t *testing.T) {
	q, _ := setup(t)
	s := createSuggestion(t, q)
	log := zerolog.Nop()

	// Nylas will fail, but local state should still be approved.
	mn := &mockNylas{err: errors.New("nylas connection refused")}
	q.UpsertPreference(t.Context(), sqlcdb.UpsertPreferenceParams{Key: "nylas_grant_id", Value: "grant-1"})
	q.UpsertPreference(t.Context(), sqlcdb.UpsertPreferenceParams{Key: "nylas_calendar_id", Value: "cal-1"})

	updated, err := ApproveSuggestion(t.Context(), q, mn, s.ID, log)
	if err != nil {
		t.Fatalf("ApproveSuggestion should succeed even if Nylas fails: %v", err)
	}
	if updated.Status != "approved" {
		t.Errorf("Status = %q, want approved", updated.Status)
	}
}

// --- Dismiss/Reject tests ---

func TestDismissSuggestion_Success(t *testing.T) {
	q, _ := setup(t)
	s := createSuggestion(t, q)
	log := zerolog.Nop()

	updated, err := RejectSuggestion(t.Context(), q, s.ID, "not interested", log)
	if err != nil {
		t.Fatalf("RejectSuggestion: %v", err)
	}
	if updated.Status != "rejected" {
		t.Errorf("Status = %q, want rejected", updated.Status)
	}
}

func TestDismissSuggestion_NotFound(t *testing.T) {
	q, _ := setup(t)
	log := zerolog.Nop()

	_, err := RejectSuggestion(t.Context(), q, 99999, "", log)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

// --- Snooze/Skip tests ---

func TestSnoozeSuggestion_Success(t *testing.T) {
	q, prefs := setup(t)
	log := zerolog.Nop()
	_, instance := createGoalAndInstance(t, q)

	skipped, _, err := SkipGoalInstance(t.Context(), q, prefs, nil, instance.GoalID, instance.ID, log)
	if err != nil {
		t.Fatalf("SkipGoalInstance: %v", err)
	}
	if skipped.Status != "skipped" {
		t.Errorf("Status = %q, want skipped", skipped.Status)
	}
}

func TestSnoozeSuggestion_AlreadyActioned(t *testing.T) {
	q, prefs := setup(t)
	log := zerolog.Nop()
	_, instance := createGoalAndInstance(t, q)

	// Complete the instance first.
	_, err := CompleteGoalInstance(t.Context(), q, instance.GoalID, instance.ID, log)
	if err != nil {
		t.Fatalf("CompleteGoalInstance: %v", err)
	}

	// Now trying to skip should fail.
	_, _, err = SkipGoalInstance(t.Context(), q, prefs, nil, instance.GoalID, instance.ID, log)
	if !errors.Is(err, ErrInvalidStatus) {
		t.Errorf("expected ErrInvalidStatus, got %v", err)
	}
}

// --- PushToNylas tests ---

func TestPushToNylas_MapsFieldsCorrectly(t *testing.T) {
	q, _ := setup(t)
	s := createSuggestion(t, q)
	log := zerolog.Nop()

	q.UpsertPreference(t.Context(), sqlcdb.UpsertPreferenceParams{Key: "nylas_grant_id", Value: "grant-1"})
	q.UpsertPreference(t.Context(), sqlcdb.UpsertPreferenceParams{Key: "nylas_calendar_id", Value: "cal-1"})

	mn := &mockNylas{event: &nylas.Event{ID: "evt-2"}}
	PushSuggestionToNylas(t.Context(), q, mn, s, log)

	if !mn.called {
		t.Fatal("expected CreateEvent to be called")
	}
	if mn.lastReq.Title != s.Title {
		t.Errorf("Title = %q, want %q", mn.lastReq.Title, s.Title)
	}
	if mn.lastReq.When.StartTime != s.StartTime.Unix() {
		t.Errorf("StartTime = %d, want %d", mn.lastReq.When.StartTime, s.StartTime.Unix())
	}
	if mn.lastReq.When.EndTime != s.EndTime.Unix() {
		t.Errorf("EndTime = %d, want %d", mn.lastReq.When.EndTime, s.EndTime.Unix())
	}
	if mn.lastReq.Busy != false {
		t.Error("suggestions should be created as not busy")
	}
}

func TestPushToNylas_NoNylasClient(t *testing.T) {
	q, _ := setup(t)
	s := createSuggestion(t, q)
	log := zerolog.Nop()

	// No prefs set — should return silently without panic.
	PushSuggestionToNylas(t.Context(), q, nil, s, log)
	// If we get here without panic, the test passes.
}

func TestPushToNylas_UsesPrefsForCalendarID(t *testing.T) {
	q, prefs := setup(t)
	log := zerolog.Nop()

	prefs.Set(t.Context(), "nylas_grant_id", "my-grant")
	prefs.Set(t.Context(), "nylas_calendar_id", "my-cal")

	_, instance := createGoalAndInstance(t, q)

	mn := &mockNylas{event: &nylas.Event{ID: "evt-3"}}
	PushGoalInstanceToNylas(t.Context(), q, mn, prefs, instance, "Study Go", log)

	if !mn.called {
		t.Fatal("expected CreateEvent to be called")
	}
	if mn.lastReq.Title != "Study Go" {
		t.Errorf("Title = %q, want %q", mn.lastReq.Title, "Study Go")
	}
	if mn.lastReq.Busy != true {
		t.Error("goal instances should be created as busy")
	}
}
