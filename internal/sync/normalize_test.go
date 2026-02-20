package sync

import (
	"testing"
	"time"

	"github.com/robkerr1992/driftcal/internal/nylas"
)

func TestNormalizeEvent_Timespan(t *testing.T) {
	ev := &nylas.Event{
		ID:         "evt-1",
		CalendarID: "cal-1",
		Title:      "Team Meeting",
		Status:     "confirmed",
		Busy:       true,
		When: nylas.EventWhen{
			Object:        "timespan",
			StartTime:     1704067200, // 2024-01-01 00:00:00 UTC
			EndTime:       1704070800, // 2024-01-01 01:00:00 UTC
			StartTimezone: "America/New_York",
		},
	}

	params, err := NormalizeEvent(42, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if params.CalendarID != 42 {
		t.Errorf("CalendarID = %d, want 42", params.CalendarID)
	}
	if params.NylasEventID != "evt-1" {
		t.Errorf("NylasEventID = %q, want %q", params.NylasEventID, "evt-1")
	}
	if params.AllDay {
		t.Error("AllDay should be false for timespan")
	}
	if params.Busy != "busy" {
		t.Errorf("Busy = %q, want %q", params.Busy, "busy")
	}
	if params.Status != "confirmed" {
		t.Errorf("Status = %q, want %q", params.Status, "confirmed")
	}
	if !params.OriginalTz.Valid || params.OriginalTz.String != "America/New_York" {
		t.Errorf("OriginalTz = %v, want America/New_York", params.OriginalTz)
	}

	wantStart := time.Unix(1704067200, 0).UTC()
	if !params.StartTime.Equal(wantStart) {
		t.Errorf("StartTime = %v, want %v", params.StartTime, wantStart)
	}
}

func TestNormalizeEvent_Date(t *testing.T) {
	ev := &nylas.Event{
		ID:     "evt-2",
		Title:  "Holiday",
		Status: "confirmed",
		Busy:   false,
		When: nylas.EventWhen{
			Object: "date",
			Date:   "2024-12-25",
		},
	}

	params, err := NormalizeEvent(1, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if !params.AllDay {
		t.Error("AllDay should be true for date")
	}
	if params.Busy != "free" {
		t.Errorf("Busy = %q, want %q", params.Busy, "free")
	}

	wantStart := time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)
	if !params.StartTime.Equal(wantStart) {
		t.Errorf("StartTime = %v, want %v", params.StartTime, wantStart)
	}

	wantEnd := time.Date(2024, 12, 25, 23, 59, 59, 0, time.UTC)
	if !params.EndTime.Equal(wantEnd) {
		t.Errorf("EndTime = %v, want %v", params.EndTime, wantEnd)
	}
}

func TestNormalizeEvent_Datespan(t *testing.T) {
	ev := &nylas.Event{
		ID:     "evt-3",
		Title:  "Vacation",
		Status: "confirmed",
		Busy:   true,
		When: nylas.EventWhen{
			Object:    "datespan",
			StartDate: "2024-07-01",
			EndDate:   "2024-07-05",
		},
	}

	params, err := NormalizeEvent(1, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if !params.AllDay {
		t.Error("AllDay should be true for datespan")
	}

	wantStart := time.Date(2024, 7, 1, 0, 0, 0, 0, time.UTC)
	if !params.StartTime.Equal(wantStart) {
		t.Errorf("StartTime = %v, want %v", params.StartTime, wantStart)
	}

	wantEnd := time.Date(2024, 7, 5, 23, 59, 59, 0, time.UTC)
	if !params.EndTime.Equal(wantEnd) {
		t.Errorf("EndTime = %v, want %v", params.EndTime, wantEnd)
	}
}

func TestNormalizeEvent_UnknownWhenType(t *testing.T) {
	ev := &nylas.Event{
		ID: "evt-4",
		When: nylas.EventWhen{
			Object: "unknown_type",
		},
	}

	_, err := NormalizeEvent(1, ev)
	if err == nil {
		t.Fatal("expected error for unknown when type")
	}
}

func TestNormalizeEvent_StatusMapping(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"confirmed", "confirmed"},
		{"tentative", "tentative"},
		{"cancelled", "cancelled"},
		{"weird_status", "confirmed"},
		{"", "confirmed"},
	}

	for _, tc := range cases {
		t.Run(tc.input, func(t *testing.T) {
			ev := &nylas.Event{
				ID:     "evt-status",
				Status: tc.input,
				When: nylas.EventWhen{
					Object:    "timespan",
					StartTime: 1704067200,
					EndTime:   1704070800,
				},
			}
			params, err := NormalizeEvent(1, ev)
			if err != nil {
				t.Fatalf("NormalizeEvent failed: %v", err)
			}
			if params.Status != tc.want {
				t.Errorf("Status = %q, want %q", params.Status, tc.want)
			}
		})
	}
}

func TestNormalizeEvent_RawDataPresent(t *testing.T) {
	ev := &nylas.Event{
		ID:    "evt-raw",
		Title: "Has raw data",
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}

	params, err := NormalizeEvent(1, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if !params.RawData.Valid {
		t.Error("RawData should be valid")
	}
	if params.RawData.String == "" {
		t.Error("RawData should not be empty")
	}
}

func TestNormalizeEvent_InvalidDate(t *testing.T) {
	ev := &nylas.Event{
		ID:     "evt-bad-date",
		Title:  "Bad Date",
		Status: "confirmed",
		When: nylas.EventWhen{
			Object: "date",
			Date:   "not-a-date",
		},
	}

	_, err := NormalizeEvent(1, ev)
	if err == nil {
		t.Fatal("expected error for invalid date")
	}
}

func TestNormalizeEvent_RecurrenceRule(t *testing.T) {
	ev := &nylas.Event{
		ID:             "evt-recur",
		Title:          "Weekly Standup",
		Status:         "confirmed",
		RecurrenceRule: []string{"RRULE:FREQ=WEEKLY"},
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}

	params, err := NormalizeEvent(1, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if !params.RecurrenceRule.Valid {
		t.Fatal("RecurrenceRule should be valid")
	}
	if params.RecurrenceRule.String != "RRULE:FREQ=WEEKLY" {
		t.Errorf("RecurrenceRule = %q, want %q", params.RecurrenceRule.String, "RRULE:FREQ=WEEKLY")
	}
}

func TestNormalizeEvent_TentativeBusy(t *testing.T) {
	ev := &nylas.Event{
		ID:     "evt-tentative",
		Title:  "Maybe Meeting",
		Status: "tentative",
		Busy:   true,
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}

	params, err := NormalizeEvent(1, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if params.Busy != "tentative" {
		t.Errorf("Busy = %q, want %q for tentative+busy event", params.Busy, "tentative")
	}
}

func TestNormalizeEvent_InvalidDatespanStart(t *testing.T) {
	ev := &nylas.Event{
		ID:     "evt-bad-ds-start",
		Status: "confirmed",
		When: nylas.EventWhen{
			Object:    "datespan",
			StartDate: "not-a-date",
			EndDate:   "2024-07-05",
		},
	}

	_, err := NormalizeEvent(1, ev)
	if err == nil {
		t.Fatal("expected error for invalid datespan start_date")
	}
}

func TestNormalizeEvent_InvalidDatespanEnd(t *testing.T) {
	ev := &nylas.Event{
		ID:     "evt-bad-ds-end",
		Status: "confirmed",
		When: nylas.EventWhen{
			Object:    "datespan",
			StartDate: "2024-07-01",
			EndDate:   "not-a-date",
		},
	}

	_, err := NormalizeEvent(1, ev)
	if err == nil {
		t.Fatal("expected error for invalid datespan end_date")
	}
}

func TestNormalizeEvent_MultipleRecurrenceRules(t *testing.T) {
	ev := &nylas.Event{
		ID:             "evt-multi-recur",
		Title:          "Complex Recurrence",
		Status:         "confirmed",
		RecurrenceRule: []string{"RRULE:FREQ=WEEKLY", "EXDATE:20240101"},
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}

	params, err := NormalizeEvent(1, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if !params.RecurrenceRule.Valid {
		t.Fatal("RecurrenceRule should be valid")
	}
	want := "RRULE:FREQ=WEEKLY\nEXDATE:20240101"
	if params.RecurrenceRule.String != want {
		t.Errorf("RecurrenceRule = %q, want %q", params.RecurrenceRule.String, want)
	}
}

func TestNormalizeEvent_OptionalFields(t *testing.T) {
	// Empty optional fields
	ev := &nylas.Event{
		ID:     "evt-empty-fields",
		Status: "confirmed",
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}

	params, err := NormalizeEvent(1, ev)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if params.Title.Valid {
		t.Errorf("empty title should produce Valid=false, got %v", params.Title)
	}
	if params.Description.Valid {
		t.Errorf("empty description should produce Valid=false, got %v", params.Description)
	}
	if params.Location.Valid {
		t.Errorf("empty location should produce Valid=false, got %v", params.Location)
	}

	// Non-empty optional fields
	ev2 := &nylas.Event{
		ID:          "evt-full-fields",
		Title:       "Meeting",
		Description: "A description",
		Location:    "Room 42",
		Status:      "confirmed",
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}

	params2, err := NormalizeEvent(1, ev2)
	if err != nil {
		t.Fatalf("NormalizeEvent failed: %v", err)
	}

	if !params2.Title.Valid || params2.Title.String != "Meeting" {
		t.Errorf("Title = %v, want {Meeting true}", params2.Title)
	}
	if !params2.Description.Valid || params2.Description.String != "A description" {
		t.Errorf("Description = %v, want {A description true}", params2.Description)
	}
	if !params2.Location.Valid || params2.Location.String != "Room 42" {
		t.Errorf("Location = %v, want {Room 42 true}", params2.Location)
	}
}
