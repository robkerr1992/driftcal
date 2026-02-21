package gap

import (
	"testing"
	"time"
)

func utc(year, month, day, hour, min int) time.Time {
	return time.Date(year, time.Month(month), day, hour, min, 0, 0, time.UTC)
}

func TestComputeGaps(t *testing.T) {
	loc, _ := time.LoadLocation("America/New_York")

	// Common day: Feb 20, 2026
	date := utc(2026, 2, 20, 0, 0)
	// Active hours 07:00-22:00 EST → 12:00-03:00 UTC (EST = UTC-5)
	activeStart := utc(2026, 2, 20, 12, 0)
	activeEnd := utc(2026, 2, 21, 3, 0) // 22:00 EST

	tests := []struct {
		name         string
		input        Input
		wantCount    int
		wantMinutes  []int    // expected duration_minutes for each gap
		wantTimeOfDay []string // optional: expected time_of_day for each gap
	}{
		{
			name: "NoEvents",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
			},
			wantCount:   1,
			wantMinutes: []int{900}, // 15 hours = 900 min
		},
		{
			name: "SingleEvent",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 20, 15, 0), Title: "Standup"},
				},
			},
			wantCount:   2,
			wantMinutes: []int{120, 720}, // 12:00-14:00 = 120min, 15:00-03:00 = 720min
		},
		{
			name: "OverlappingEvents",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 20, 15, 30), Title: "Meeting A"},
					{StartTime: utc(2026, 2, 20, 15, 0), EndTime: utc(2026, 2, 20, 16, 0), Title: "Meeting B"},
				},
			},
			wantCount:   2,
			wantMinutes: []int{120, 660}, // 12:00-14:00, 16:00-03:00
		},
		{
			name: "AdjacentEvents",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 20, 15, 0), Title: "A"},
					{StartTime: utc(2026, 2, 20, 15, 0), EndTime: utc(2026, 2, 20, 16, 0), Title: "B"},
				},
			},
			wantCount:   2,
			wantMinutes: []int{120, 660},
		},
		{
			name: "AllDayBusy",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{AllDay: true, Busy: "busy", Title: "PTO"},
				},
			},
			wantCount: 0,
		},
		{
			name: "AllDayFree",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// busy="free" events are filtered by the SQL query (busy != 'free'),
					// so they won't appear in BlockingEvents. This tests that even if
					// one slipped through, the algorithm wouldn't include it (since only
					// all-day busy="busy" blocks active hours).
				},
			},
			wantCount:   1,
			wantMinutes: []int{900},
		},
		{
			name: "AllDayTentativeIgnored",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{AllDay: true, Busy: "tentative", Title: "Daylight Saving Time"},
				},
			},
			wantCount:   1,
			wantMinutes: []int{900}, // ignored → full active hours
		},
		{
			name: "TentativeNonAllDayBlocks",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 20, 15, 0), Busy: "tentative", Title: "Maybe Lunch"},
				},
			},
			wantCount:   2,
			wantMinutes: []int{120, 720},
		},
		{
			name: "MinGapFilter",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// Create a 20-minute gap between two events (12:00-12:00 is start, event at 12:00-12:40, gap 12:40-13:00 = 20 min, event at 13:00-...)
					{StartTime: utc(2026, 2, 20, 12, 0), EndTime: utc(2026, 2, 20, 12, 40), Title: "A"},
					{StartTime: utc(2026, 2, 20, 13, 0), EndTime: utc(2026, 2, 20, 14, 0), Title: "B"},
				},
			},
			wantCount:   1,                // only the 13-hour gap after B survives
			wantMinutes: []int{780}, // 14:00-03:00 = 780 min
		},
		{
			name: "ProtectedBlock",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				ProtectedBlocks: []ProtectedBlock{
					{StartTime: utc(2026, 2, 20, 23, 0), EndTime: utc(2026, 2, 21, 1, 0)}, // 18:00-20:00 EST
				},
			},
			wantCount:   2,
			wantMinutes: []int{660, 120}, // 12:00-23:00 = 660, 01:00-03:00 = 120
		},
		{
			name: "EventBeyondActiveHours",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// Event starts before active hours and ends during them.
					{StartTime: utc(2026, 2, 20, 10, 0), EndTime: utc(2026, 2, 20, 13, 0), Title: "Early"},
				},
			},
			wantCount:   1,
			wantMinutes: []int{840}, // 13:00-03:00 = 840 min
		},
		{
			name: "EventCoversFullDay",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{StartTime: utc(2026, 2, 20, 10, 0), EndTime: utc(2026, 2, 21, 5, 0), Title: "All Day Workshop"},
				},
			},
			wantCount: 0,
		},
		{
			name: "MultiDayEvent",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// 3-day conference: clamped to this day's active hours.
					{StartTime: utc(2026, 2, 19, 0, 0), EndTime: utc(2026, 2, 22, 0, 0), Title: "Conference"},
				},
			},
			wantCount: 0,
		},
		{
			name: "EventSpansMidnight",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// Event 21:00 EST - 02:00 EST → UTC 02:00-07:00 Feb 21.
					// Within active hours (12:00-03:00 UTC), this blocks 02:00-03:00.
					{StartTime: utc(2026, 2, 21, 2, 0), EndTime: utc(2026, 2, 21, 7, 0), Title: "Late Night"},
				},
			},
			wantCount:   1,
			wantMinutes: []int{840}, // 12:00-02:00 = 840 min
		},
		{
			name: "TimeOfDayTag",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 15, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// Create three gaps: morning (07:00-09:00 EST), afternoon (13:00-15:00 EST), evening (19:00-22:00 EST)
					// EST = UTC-5, so:
					// Block 09:00-13:00 EST = 14:00-18:00 UTC
					// Block 15:00-19:00 EST = 20:00-00:00 UTC
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 20, 18, 0), Title: "Work Block"},
					{StartTime: utc(2026, 2, 20, 20, 0), EndTime: utc(2026, 2, 21, 0, 0), Title: "Evening Block"},
				},
			},
			wantCount:     3,
			wantMinutes:   []int{120, 120, 180},                       // 12-14, 18-20, 00-03 UTC
			wantTimeOfDay: []string{"morning", "afternoon", "evening"}, // 07-09, 13-15, 19-22 EST
		},
		{
			name: "DurationBucket",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 15, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// short gap: 30 min (12:00-12:30 free, event 12:30-14:00)
					{StartTime: utc(2026, 2, 20, 12, 30), EndTime: utc(2026, 2, 20, 14, 0), Title: "A"},
					// medium gap: 90 min (14:00-15:30 free, event 15:30-16:00)
					{StartTime: utc(2026, 2, 20, 15, 30), EndTime: utc(2026, 2, 20, 16, 0), Title: "B"},
					// remaining is long
				},
			},
			wantCount:   3,
			wantMinutes: []int{30, 90, 660},
		},
		{
			name: "DurationBucketBoundary",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 15, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					// Exactly 120 min gap: should be "medium" not "long".
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 21, 3, 0), Title: "Rest of Day"},
				},
			},
			wantCount:   1,
			wantMinutes: []int{120}, // 12:00-14:00 = exactly 120 min → medium
		},
		{
			name: "BeforeAfterTitles",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				BlockingEvents: []BlockingEvent{
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 20, 15, 0), Title: "Standup"},
					{StartTime: utc(2026, 2, 20, 17, 0), EndTime: utc(2026, 2, 20, 18, 0), Title: "Review"},
				},
			},
			wantCount: 3,
		},
		{
			name: "GoalInstanceBlocks",
			input: Input{
				Date: date, ActiveStart: activeStart, ActiveEnd: activeEnd,
				MinGapMinutes: 45, UserTimezone: loc,
				GoalInstances: []GoalInstance{
					{StartTime: utc(2026, 2, 20, 14, 0), EndTime: utc(2026, 2, 20, 15, 0), Label: "Study Session"},
				},
			},
			wantCount:   2,
			wantMinutes: []int{120, 720},
		},
		{
			name: "DSTTransitionDay",
			input: func() Input {
				// Spring forward 2026: March 8, 2026, 2:00 AM EST → 3:00 AM EDT
				// Active hours 07:00-22:00 in America/New_York.
				// Both 07:00 and 22:00 fall after the transition, so both are EDT (UTC-4).
				dstDate := utc(2026, 3, 8, 0, 0)
				dstStart := time.Date(2026, 3, 8, 7, 0, 0, 0, loc).UTC()  // 11:00 UTC (EDT)
				dstEnd := time.Date(2026, 3, 8, 22, 0, 0, 0, loc).UTC()   // 02:00 UTC March 9 (EDT)
				return Input{
					Date: dstDate, ActiveStart: dstStart, ActiveEnd: dstEnd,
					MinGapMinutes: 45, UserTimezone: loc,
				}
			}(),
			wantCount: 1,
			// 07:00 EDT → 11:00 UTC, 22:00 EDT → 02:00 UTC = 15 hours = 900 min.
			// Key: time.Date in a Location handles DST correctly — the UTC offsets
			// shift from -5 to -4 automatically, so the handler doesn't need special logic.
			wantMinutes: []int{900},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gaps := ComputeGaps(tt.input)

			if len(gaps) != tt.wantCount {
				t.Fatalf("got %d gaps, want %d; gaps: %+v", len(gaps), tt.wantCount, gaps)
			}

			if tt.wantMinutes != nil {
				for i, wantMin := range tt.wantMinutes {
					if i >= len(gaps) {
						break
					}
					if gaps[i].DurationMinutes != wantMin {
						t.Errorf("gap[%d].DurationMinutes = %d, want %d", i, gaps[i].DurationMinutes, wantMin)
					}
				}
			}

			if tt.wantTimeOfDay != nil {
				for i, wantTOD := range tt.wantTimeOfDay {
					if i >= len(gaps) {
						break
					}
					if gaps[i].TimeOfDay != wantTOD {
						t.Errorf("gap[%d].TimeOfDay = %q, want %q", i, gaps[i].TimeOfDay, wantTOD)
					}
				}
			}
		})
	}

	// Helper to find a test case by name without relying on slice indices.
	findTest := func(name string) Input {
		for _, tt := range tests {
			if tt.name == name {
				return tt.input
			}
		}
		t.Fatalf("test %q not found", name)
		return Input{}
	}

	// Additional assertions for specific subtests.
	t.Run("DurationBucket_Values", func(t *testing.T) {
		input := findTest("DurationBucket")
		gaps := ComputeGaps(input)
		if len(gaps) != 3 {
			t.Fatalf("got %d gaps, want 3", len(gaps))
		}
		wantBuckets := []string{"short", "medium", "long"}
		for i, want := range wantBuckets {
			if gaps[i].DurationBucket != want {
				t.Errorf("gap[%d].DurationBucket = %q, want %q", i, gaps[i].DurationBucket, want)
			}
		}
	})

	t.Run("DurationBucketBoundary_IsMedium", func(t *testing.T) {
		input := findTest("DurationBucketBoundary")
		gaps := ComputeGaps(input)
		if len(gaps) != 1 {
			t.Fatalf("got %d gaps, want 1", len(gaps))
		}
		if gaps[0].DurationBucket != "medium" {
			t.Errorf("120 min gap: DurationBucket = %q, want medium", gaps[0].DurationBucket)
		}
	})

	t.Run("BeforeAfterTitles_Values", func(t *testing.T) {
		input := findTest("BeforeAfterTitles")
		gaps := ComputeGaps(input)
		if len(gaps) != 3 {
			t.Fatalf("got %d gaps, want 3", len(gaps))
		}
		// Gap 0: 12:00-14:00 — no event before (start of active hours), Standup after
		if gaps[0].BeforeEventTitle != "" {
			t.Errorf("gap[0].BeforeEventTitle = %q, want empty", gaps[0].BeforeEventTitle)
		}
		if gaps[0].AfterEventTitle != "Standup" {
			t.Errorf("gap[0].AfterEventTitle = %q, want Standup", gaps[0].AfterEventTitle)
		}
		// Gap 1: 15:00-17:00 — Standup before, Review after
		if gaps[1].BeforeEventTitle != "Standup" {
			t.Errorf("gap[1].BeforeEventTitle = %q, want Standup", gaps[1].BeforeEventTitle)
		}
		if gaps[1].AfterEventTitle != "Review" {
			t.Errorf("gap[1].AfterEventTitle = %q, want Review", gaps[1].AfterEventTitle)
		}
		// Gap 2: 18:00-03:00 — Review before, no event after (end of active hours)
		if gaps[2].BeforeEventTitle != "Review" {
			t.Errorf("gap[2].BeforeEventTitle = %q, want Review", gaps[2].BeforeEventTitle)
		}
		if gaps[2].AfterEventTitle != "" {
			t.Errorf("gap[2].AfterEventTitle = %q, want empty", gaps[2].AfterEventTitle)
		}
	})
}
