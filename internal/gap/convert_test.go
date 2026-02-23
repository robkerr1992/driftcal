package gap

import (
	"database/sql"
	"testing"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

func TestConvertBlockingEvents(t *testing.T) {
	t.Run("NullTitle", func(t *testing.T) {
		events := []sqlcdb.Event{
			{
				StartTime: time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC),
				AllDay:    false,
				Busy:      "busy",
				Title:     sql.NullString{Valid: false},
			},
		}
		result := ConvertBlockingEvents(events)
		if len(result) != 1 {
			t.Fatalf("got %d events, want 1", len(result))
		}
		if result[0].Title != "" {
			t.Errorf("Title = %q, want empty for null", result[0].Title)
		}
	})

	t.Run("FieldMapping", func(t *testing.T) {
		start := time.Date(2026, 2, 20, 14, 0, 0, 0, time.UTC)
		end := time.Date(2026, 2, 20, 15, 30, 0, 0, time.UTC)
		events := []sqlcdb.Event{
			{
				StartTime: start,
				EndTime:   end,
				AllDay:    true,
				Busy:      "tentative",
				Title:     sql.NullString{String: "Team standup", Valid: true},
			},
		}
		result := ConvertBlockingEvents(events)
		if len(result) != 1 {
			t.Fatalf("got %d events, want 1", len(result))
		}
		ev := result[0]
		if !ev.StartTime.Equal(start) {
			t.Errorf("StartTime = %v, want %v", ev.StartTime, start)
		}
		if !ev.EndTime.Equal(end) {
			t.Errorf("EndTime = %v, want %v", ev.EndTime, end)
		}
		if !ev.AllDay {
			t.Error("AllDay should be true")
		}
		if ev.Busy != "tentative" {
			t.Errorf("Busy = %q, want tentative", ev.Busy)
		}
		if ev.Title != "Team standup" {
			t.Errorf("Title = %q, want Team standup", ev.Title)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		result := ConvertBlockingEvents(nil)
		if result != nil {
			t.Errorf("expected nil for empty input, got %v", result)
		}
	})
}

func TestExpandProtectedBlocksForDate(t *testing.T) {
	est, _ := time.LoadLocation("America/New_York")

	blocks := []sqlcdb.ProtectedBlock{
		{
			StartTime: "18:00",
			EndTime:   "20:00",
			DayOfWeek: sql.NullInt64{Int64: 5, Valid: true}, // Friday
		},
		{
			StartTime: "07:00",
			EndTime:   "08:00",
			DayOfWeek: sql.NullInt64{Valid: false}, // daily
		},
		{
			StartTime: "09:00",
			EndTime:   "10:00",
			DayOfWeek: sql.NullInt64{Int64: 1, Valid: true}, // Monday only
		},
	}

	// Use times that are clearly the correct day in EST (noon UTC = 7am EST).
	// This matches the pipeline pattern: dayLocal := date.In(loc), then dayLocal.UTC().
	t.Run("Friday", func(t *testing.T) {
		// 2026-02-20 is a Friday. Use noon UTC so EST is still the 20th.
		friday := time.Date(2026, 2, 20, 12, 0, 0, 0, time.UTC)
		result := ExpandProtectedBlocksForDate(blocks, friday, est)
		// Should get the Friday block + the daily block = 2
		if len(result) != 2 {
			t.Fatalf("got %d blocks, want 2", len(result))
		}
	})

	t.Run("Monday", func(t *testing.T) {
		// 2026-02-23 is a Monday.
		monday := time.Date(2026, 2, 23, 12, 0, 0, 0, time.UTC)
		result := ExpandProtectedBlocksForDate(blocks, monday, est)
		// Should get the Monday block + the daily block = 2
		if len(result) != 2 {
			t.Fatalf("got %d blocks, want 2", len(result))
		}
	})

	t.Run("Tuesday", func(t *testing.T) {
		// 2026-02-24 is a Tuesday.
		tuesday := time.Date(2026, 2, 24, 12, 0, 0, 0, time.UTC)
		result := ExpandProtectedBlocksForDate(blocks, tuesday, est)
		// Should get only the daily block = 1
		if len(result) != 1 {
			t.Fatalf("got %d blocks, want 1", len(result))
		}
	})

	t.Run("UTCNoonFriday", func(t *testing.T) {
		// Use UTC timezone — no offset, so weekday matches directly.
		friday := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC) // Friday in UTC
		result := ExpandProtectedBlocksForDate(blocks, friday, time.UTC)
		// Friday block + daily block = 2
		if len(result) != 2 {
			t.Fatalf("got %d blocks, want 2", len(result))
		}
	})
}

func TestConvertGoalInstances(t *testing.T) {
	t.Run("LabelMapping", func(t *testing.T) {
		start := time.Date(2026, 2, 20, 9, 0, 0, 0, time.UTC)
		end := time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC)
		instances := []sqlcdb.ListScheduledGoalInstancesInRangeWithLabelRow{
			{
				ScheduledStart: start,
				ScheduledEnd:   end,
				GoalLabel:      "Study Go",
			},
		}
		result := ConvertGoalInstances(instances)
		if len(result) != 1 {
			t.Fatalf("got %d instances, want 1", len(result))
		}
		gi := result[0]
		if !gi.StartTime.Equal(start) {
			t.Errorf("StartTime = %v, want %v", gi.StartTime, start)
		}
		if !gi.EndTime.Equal(end) {
			t.Errorf("EndTime = %v, want %v", gi.EndTime, end)
		}
		if gi.Label != "Study Go" {
			t.Errorf("Label = %q, want Study Go", gi.Label)
		}
	})

	t.Run("Empty", func(t *testing.T) {
		result := ConvertGoalInstances(nil)
		if result != nil {
			t.Errorf("expected nil for empty input, got %v", result)
		}
	})
}

func TestExpandProtectedBlock(t *testing.T) {
	est, _ := time.LoadLocation("America/New_York")

	tests := []struct {
		name      string
		block     sqlcdb.ProtectedBlock
		date      time.Time
		loc       *time.Location
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name: "Normal",
			block: sqlcdb.ProtectedBlock{
				StartTime: "18:00",
				EndTime:   "20:00",
				DayOfWeek: sql.NullInt64{Int64: 5, Valid: true}, // Friday
			},
			date:      time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC), // a Friday
			loc:       est,
			wantStart: time.Date(2026, 2, 20, 18, 0, 0, 0, est).UTC(), // 23:00 UTC
			wantEnd:   time.Date(2026, 2, 20, 20, 0, 0, 0, est).UTC(), // 01:00 UTC Feb 21
		},
		{
			name: "DST",
			block: sqlcdb.ProtectedBlock{
				StartTime: "18:00",
				EndTime:   "20:00",
				DayOfWeek: sql.NullInt64{Int64: 0, Valid: true}, // Sunday
			},
			// Spring forward day: March 8, 2026
			date: time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
			loc:  est,
			// After spring forward, 18:00 is EDT (UTC-4), not EST (UTC-5).
			wantStart: time.Date(2026, 3, 8, 18, 0, 0, 0, est).UTC(), // 22:00 UTC (EDT)
			wantEnd:   time.Date(2026, 3, 8, 20, 0, 0, 0, est).UTC(), // 00:00 UTC March 9 (EDT)
		},
		{
			name: "DailyBlock",
			block: sqlcdb.ProtectedBlock{
				StartTime: "07:00",
				EndTime:   "08:00",
				DayOfWeek: sql.NullInt64{Valid: false}, // null = daily
			},
			date:      time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC),
			loc:       est,
			wantStart: time.Date(2026, 2, 20, 7, 0, 0, 0, est).UTC(), // 12:00 UTC
			wantEnd:   time.Date(2026, 2, 20, 8, 0, 0, 0, est).UTC(), // 13:00 UTC
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pb, err := ExpandProtectedBlock(tt.block, tt.date, tt.loc)
			if err != nil {
				t.Fatalf("ExpandProtectedBlock() error: %v", err)
			}

			if !pb.StartTime.Equal(tt.wantStart) {
				t.Errorf("StartTime = %v, want %v", pb.StartTime, tt.wantStart)
			}
			if !pb.EndTime.Equal(tt.wantEnd) {
				t.Errorf("EndTime = %v, want %v", pb.EndTime, tt.wantEnd)
			}
		})
	}
}

func TestParseHHMM_Invalid(t *testing.T) {
	loc := time.UTC
	date := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		input string
	}{
		{"Empty", ""},
		{"TooShort", "9:00"},
		{"TooLong", "09:000"},
		{"NoColon", "09-00"},
		{"BadHour", "25:00"},
		{"BadMinute", "09:60"},
		{"Letters", "ab:cd"},
		{"NegativeHour", "-1:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseHHMM(tt.input, date, loc)
			if err == nil {
				t.Errorf("ParseHHMM(%q) = nil error, want error", tt.input)
			}
		})
	}
}

func TestParseHHMM_Valid(t *testing.T) {
	loc := time.UTC
	date := time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		input    string
		wantHour int
		wantMin  int
	}{
		{"00:00", 0, 0},
		{"09:30", 9, 30},
		{"23:59", 23, 59},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseHHMM(tt.input, date, loc)
			if err != nil {
				t.Fatalf("ParseHHMM(%q) error: %v", tt.input, err)
			}
			if got.Hour() != tt.wantHour || got.Minute() != tt.wantMin {
				t.Errorf("ParseHHMM(%q) = %02d:%02d, want %02d:%02d",
					tt.input, got.Hour(), got.Minute(), tt.wantHour, tt.wantMin)
			}
		})
	}
}
