package sync

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
)

type mockFetcher struct {
	events map[string][]nylas.Event // keyed by calendarID
	err    error
}

func (m *mockFetcher) ListEvents(ctx context.Context, grantID, calendarID string, start, end time.Time) ([]nylas.Event, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.events[calendarID], nil
}

func setupSyncerTestDB(t *testing.T) (*sqlcdb.Queries, sqlcdb.CalendarAccount, sqlcdb.Calendar) {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	account, err := q.CreateAccount(context.Background(), sqlcdb.CreateAccountParams{
		NylasGrantID: "grant-sync",
		Provider:     "google",
		Email:        "sync@example.com",
	})
	if err != nil {
		t.Fatalf("creating test account: %v", err)
	}

	cal, err := q.CreateCalendar(context.Background(), sqlcdb.CreateCalendarParams{
		AccountID:       account.ID,
		NylasCalendarID: "nylas-cal-sync",
		Name:            "Sync Test Cal",
	})
	if err != nil {
		t.Fatalf("creating test calendar: %v", err)
	}

	return q, account, cal
}

func TestSyncAll_InsertsEvents(t *testing.T) {
	q, _, _ := setupSyncerTestDB(t)
	log := zerolog.Nop()

	fetcher := &mockFetcher{
		events: map[string][]nylas.Event{
			"nylas-cal-sync": {
				{
					ID:     "evt-sync-1",
					Title:  "Synced Meeting",
					Status: "confirmed",
					Busy:   true,
					When: nylas.EventWhen{
						Object:    "timespan",
						StartTime: 1704067200,
						EndTime:   1704070800,
					},
				},
			},
		},
	}

	s := New(fetcher, q, log, time.Hour)
	err := s.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	evt, err := q.GetEventByNylasID(context.Background(), "evt-sync-1")
	if err != nil {
		t.Fatalf("event not found after sync: %v", err)
	}
	if evt.Title.String != "Synced Meeting" {
		t.Errorf("title = %q, want %q", evt.Title.String, "Synced Meeting")
	}
}

func TestSyncAll_UpsertsExisting(t *testing.T) {
	q, _, cal := setupSyncerTestDB(t)
	log := zerolog.Nop()

	// Pre-insert an event
	if _, err := q.UpsertEvent(context.Background(), sqlcdb.UpsertEventParams{
		CalendarID:   cal.ID,
		NylasEventID: "evt-upsert",
		Title:        sql.NullString{String: "Original", Valid: true},
		Status:       "confirmed",
		Busy:         "busy",
		Category:     sql.NullString{String: "other", Valid: true},
		StartTime:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		EndTime:      time.Date(2024, 1, 1, 1, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("pre-inserting event: %v", err)
	}

	fetcher := &mockFetcher{
		events: map[string][]nylas.Event{
			"nylas-cal-sync": {
				{
					ID:     "evt-upsert",
					Title:  "Updated",
					Status: "confirmed",
					Busy:   true,
					When: nylas.EventWhen{
						Object:    "timespan",
						StartTime: 1704067200,
						EndTime:   1704070800,
					},
				},
			},
		},
	}

	s := New(fetcher, q, log, time.Hour)
	err := s.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll failed: %v", err)
	}

	evt, err := q.GetEventByNylasID(context.Background(), "evt-upsert")
	if err != nil {
		t.Fatalf("event not found: %v", err)
	}
	if evt.Title.String != "Updated" {
		t.Errorf("title = %q, want %q (should be upserted)", evt.Title.String, "Updated")
	}
}

func TestSyncAll_NoActiveCalendars(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	fetcher := &mockFetcher{}

	s := New(fetcher, q, log, time.Hour)
	err := s.SyncAll(context.Background())
	if err != nil {
		t.Fatalf("SyncAll should succeed with no calendars: %v", err)
	}
}

func TestSyncAll_PartialFailure(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	// Create two accounts with calendars
	acc1, _ := q.CreateAccount(context.Background(), sqlcdb.CreateAccountParams{
		NylasGrantID: "grant-ok",
		Provider:     "google",
		Email:        "ok@example.com",
	})
	q.CreateCalendar(context.Background(), sqlcdb.CreateCalendarParams{
		AccountID:       acc1.ID,
		NylasCalendarID: "cal-ok",
		Name:            "OK Calendar",
	})

	acc2, _ := q.CreateAccount(context.Background(), sqlcdb.CreateAccountParams{
		NylasGrantID: "grant-fail",
		Provider:     "google",
		Email:        "fail@example.com",
	})
	q.CreateCalendar(context.Background(), sqlcdb.CreateCalendarParams{
		AccountID:       acc2.ID,
		NylasCalendarID: "cal-fail",
		Name:            "Fail Calendar",
	})

	// Use a fetcher that fails for one calendar
	failFetcher := &selectiveFetcher{
		events: map[string][]nylas.Event{
			"cal-ok": {{
				ID: "evt-ok", Title: "OK Event", Status: "confirmed", Busy: true,
				When: nylas.EventWhen{Object: "timespan", StartTime: 1704067200, EndTime: 1704070800},
			}},
		},
		failCalendar: "cal-fail",
	}

	s := New(failFetcher, q, log, time.Hour)
	err := s.SyncAll(context.Background())

	// Should report partial failure
	if err == nil {
		t.Fatal("expected error for partial failure")
	}

	// The OK calendar's event should still be synced
	_, err = q.GetEventByNylasID(context.Background(), "evt-ok")
	if err != nil {
		t.Errorf("OK event should be synced despite other calendar failing: %v", err)
	}
}

type selectiveFetcher struct {
	events       map[string][]nylas.Event
	failCalendar string
}

func (f *selectiveFetcher) ListEvents(ctx context.Context, grantID, calendarID string, start, end time.Time) ([]nylas.Event, error) {
	if calendarID == f.failCalendar {
		return nil, errors.New("simulated API failure")
	}
	return f.events[calendarID], nil
}

func TestSyncer_StartStop(t *testing.T) {
	q, _, _ := setupSyncerTestDB(t)
	log := zerolog.Nop()

	fetcher := &mockFetcher{
		events: map[string][]nylas.Event{
			"nylas-cal-sync": {{
				ID: "evt-lifecycle", Title: "Lifecycle Event", Status: "confirmed",
				When: nylas.EventWhen{Object: "timespan", StartTime: 1704067200, EndTime: 1704070800},
			}},
		},
	}

	s := New(fetcher, q, log, 50*time.Millisecond)
	s.Start()

	// Poll for the expected event with a timeout instead of a fixed sleep
	deadline := time.After(2 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for initial sync")
		case <-ticker.C:
			if _, err := q.GetEventByNylasID(context.Background(), "evt-lifecycle"); err == nil {
				goto synced
			}
		}
	}
synced:
	s.Stop()
}

func TestSyncer_StopWithoutStart(t *testing.T) {
	q, _, _ := setupSyncerTestDB(t)
	log := zerolog.Nop()

	s := New(&mockFetcher{}, q, log, time.Hour)
	// Should not panic
	s.Stop()
}

func TestSyncAccount_FiltersGrant(t *testing.T) {
	q, _, _ := setupSyncerTestDB(t)
	log := zerolog.Nop()

	fetcher := &mockFetcher{
		events: map[string][]nylas.Event{
			"nylas-cal-sync": {{
				ID: "evt-account", Title: "Account Event", Status: "confirmed",
				When: nylas.EventWhen{Object: "timespan", StartTime: 1704067200, EndTime: 1704070800},
			}},
		},
	}

	s := New(fetcher, q, log, time.Hour)

	// Sync specific account
	err := s.SyncAccount(context.Background(), "grant-sync")
	if err != nil {
		t.Fatalf("SyncAccount failed: %v", err)
	}

	_, err = q.GetEventByNylasID(context.Background(), "evt-account")
	if err != nil {
		t.Errorf("event should be synced for matching grant: %v", err)
	}
}

func TestSyncAccount_NonMatchingGrant(t *testing.T) {
	q, _, _ := setupSyncerTestDB(t)
	log := zerolog.Nop()

	fetcher := &mockFetcher{
		events: map[string][]nylas.Event{
			"nylas-cal-sync": {{
				ID: "evt-should-not-appear", Title: "Ghost", Status: "confirmed",
				When: nylas.EventWhen{Object: "timespan", StartTime: 1704067200, EndTime: 1704070800},
			}},
		},
	}

	s := New(fetcher, q, log, time.Hour)

	// Sync a non-existent grant — should succeed with zero events
	err := s.SyncAccount(context.Background(), "grant-nonexistent")
	if err != nil {
		t.Fatalf("SyncAccount should succeed for non-matching grant: %v", err)
	}

	// The event should NOT have been inserted since no calendars match
	_, err = q.GetEventByNylasID(context.Background(), "evt-should-not-appear")
	if err == nil {
		t.Error("event should not be inserted for non-matching grant")
	}
}
