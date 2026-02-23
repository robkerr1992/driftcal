package gap

import (
	"database/sql"
	"testing"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

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
