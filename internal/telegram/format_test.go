package telegram

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

func TestEscapeMD2(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "hello world", "hello world"},
		{"special chars", "a_b*c[d]e(f)g", `a\_b\*c\[d\]e\(f\)g`},
		{"backslash", `a\b`, `a\\b`},
		{"all special", `_*[]()~` + "`>#+-.=|{}!", `\_\*\[\]\(\)\~` + "\\`" + `\>\#\+\-\.\=\|\{\}\!`},
		{"url-like", "https://example.com/path?q=1", `https://example\.com/path?q\=1`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := EscapeMD2(tt.in)
			if got != tt.want {
				t.Errorf("EscapeMD2(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatDailyDigest(t *testing.T) {
	tz := time.UTC
	date := time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC)

	t.Run("no goals no suggestions", func(t *testing.T) {
		text := FormatDailyDigest(date, nil, nil, tz)
		if !strings.Contains(text, "packed") {
			t.Errorf("expected packed message, got %q", text)
		}
	})

	t.Run("goals only", func(t *testing.T) {
		goals := []sqlcdb.ListScheduledGoalInstancesInRangeWithLabelRow{
			{
				ID:             1,
				GoalLabel:      "Run",
				ScheduledStart: time.Date(2026, 2, 24, 7, 0, 0, 0, time.UTC),
				ScheduledEnd:   time.Date(2026, 2, 24, 8, 0, 0, 0, time.UTC),
			},
		}
		text := FormatDailyDigest(date, goals, nil, tz)
		if !strings.Contains(text, "Goals") {
			t.Errorf("expected Goals section, got %q", text)
		}
		if !strings.Contains(text, "Run") {
			t.Errorf("expected goal label, got %q", text)
		}
		if !strings.Contains(text, "try again") {
			t.Errorf("expected no-suggestions fallback, got %q", text)
		}
	})

	t.Run("suggestions only", func(t *testing.T) {
		suggestions := []sqlcdb.ActivitySuggestion{
			{
				ID:        1,
				Title:     "Walk",
				StartTime: time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC),
				EndTime:   time.Date(2026, 2, 24, 11, 0, 0, 0, time.UTC),
			},
		}
		text := FormatDailyDigest(date, nil, suggestions, tz)
		if !strings.Contains(text, "Suggestions") {
			t.Errorf("expected Suggestions section, got %q", text)
		}
	})
}

func TestFormatSuggestionCard(t *testing.T) {
	tz := time.UTC
	s := sqlcdb.ActivitySuggestion{
		ID:          42,
		Title:       "Morning Yoga",
		Category:    "health",
		EnergyLevel: "low",
		Description: "A relaxing session",
		StartTime:   time.Date(2026, 2, 24, 7, 0, 0, 0, time.UTC),
		EndTime:     time.Date(2026, 2, 24, 7, 30, 0, 0, time.UTC),
	}

	text, kb := FormatSuggestionCard(s, tz)

	if !strings.Contains(text, "Morning Yoga") {
		t.Errorf("expected title in card, got %q", text)
	}
	if kb == nil || len(kb.InlineKeyboard) == 0 {
		t.Fatal("expected inline keyboard")
	}
	if len(kb.InlineKeyboard[0]) != 2 {
		t.Fatalf("expected 2 buttons, got %d", len(kb.InlineKeyboard[0]))
	}
	if kb.InlineKeyboard[0][0].CallbackData != "approve:s:42" {
		t.Errorf("expected approve callback data, got %q", kb.InlineKeyboard[0][0].CallbackData)
	}
	if kb.InlineKeyboard[0][1].CallbackData != "reject:s:42" {
		t.Errorf("expected reject callback data, got %q", kb.InlineKeyboard[0][1].CallbackData)
	}
}

func TestFormatGoalInstanceCard(t *testing.T) {
	tz := time.UTC
	gi := sqlcdb.ListScheduledGoalInstancesInRangeWithLabelRow{
		ID:             7,
		GoalLabel:      "Study Go",
		ScheduledStart: time.Date(2026, 2, 24, 14, 0, 0, 0, time.UTC),
		ScheduledEnd:   time.Date(2026, 2, 24, 15, 0, 0, 0, time.UTC),
	}

	text, kb := FormatGoalInstanceCard(gi, tz)

	if !strings.Contains(text, "Study Go") {
		t.Errorf("expected label in card, got %q", text)
	}
	if kb == nil || len(kb.InlineKeyboard) == 0 {
		t.Fatal("expected inline keyboard")
	}
	if kb.InlineKeyboard[0][0].CallbackData != "complete:g:7" {
		t.Errorf("expected complete callback data, got %q", kb.InlineKeyboard[0][0].CallbackData)
	}
	if kb.InlineKeyboard[0][1].CallbackData != "skip:g:7" {
		t.Errorf("expected skip callback data, got %q", kb.InlineKeyboard[0][1].CallbackData)
	}
}

func TestFormatGoalsList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		text := FormatGoalsList(nil, nil)
		if !strings.Contains(text, "No active goals") {
			t.Errorf("expected empty message, got %q", text)
		}
	})

	t.Run("with goals", func(t *testing.T) {
		goals := []sqlcdb.RecurringGoal{
			{ID: 1, Label: "Run", Category: "workout", DurationMinutes: 30, TimesPerWeek: 3},
		}
		weekCounts := map[int64][2]int{
			1: {1, 1}, // 1 scheduled, 1 completed
		}
		text := FormatGoalsList(goals, weekCounts)
		if !strings.Contains(text, "Run") {
			t.Errorf("expected goal label, got %q", text)
		}
		if !strings.Contains(text, "1 remaining") {
			t.Errorf("expected remaining count, got %q", text)
		}
	})
}

func TestFormatGapsList(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		text := FormatGapsList(nil, time.UTC)
		if !strings.Contains(text, "No gaps") {
			t.Errorf("expected empty message, got %q", text)
		}
	})

	t.Run("with gaps", func(t *testing.T) {
		gaps := []sqlcdb.DailyGap{
			{
				GapDate:         time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC),
				StartTime:       time.Date(2026, 2, 24, 9, 0, 0, 0, time.UTC),
				EndTime:         time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC),
				DurationMinutes: 60,
				TimeOfDay:       "morning",
			},
		}
		text := FormatGapsList(gaps, time.UTC)
		if !strings.Contains(text, "60min") {
			t.Errorf("expected duration, got %q", text)
		}
	})
}

func TestFormatPreferences(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		text := FormatPreferences(nil)
		if !strings.Contains(text, "No preferences") {
			t.Errorf("expected empty message, got %q", text)
		}
	})

	t.Run("with prefs", func(t *testing.T) {
		prefs := map[string]string{
			"timezone": "America/Chicago",
		}
		text := FormatPreferences(prefs)
		if !strings.Contains(text, "timezone") {
			t.Errorf("expected key, got %q", text)
		}
	})
}

func TestFormatStatus(t *testing.T) {
	t.Run("no pipeline run", func(t *testing.T) {
		text := FormatStatus(nil, 5*time.Minute)
		if !strings.Contains(text, "5m") {
			t.Errorf("expected uptime, got %q", text)
		}
		if !strings.Contains(text, "No pipeline") {
			t.Errorf("expected no pipeline message, got %q", text)
		}
	})

	t.Run("with pipeline run", func(t *testing.T) {
		run := &sqlcdb.PipelineRun{
			RunDate:           time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
			Status:            "completed",
			LastCompletedStep: sql.NullString{String: "suggest", Valid: true},
		}
		text := FormatStatus(run, 2*time.Hour+30*time.Minute)
		if !strings.Contains(text, "2h 30m") {
			t.Errorf("expected uptime, got %q", text)
		}
		if !strings.Contains(text, "completed") {
			t.Errorf("expected status, got %q", text)
		}
	})
}

func TestSplitMessage(t *testing.T) {
	t.Run("short message", func(t *testing.T) {
		chunks := SplitMessage("hello")
		if len(chunks) != 1 || chunks[0] != "hello" {
			t.Errorf("expected single chunk, got %v", chunks)
		}
	})

	t.Run("long message", func(t *testing.T) {
		long := strings.Repeat("a\n", 2000) // ~4000 chars
		chunks := SplitMessage(long)
		if len(chunks) < 2 {
			t.Errorf("expected multiple chunks, got %d", len(chunks))
		}
		// Verify we don't lose content.
		total := 0
		for _, c := range chunks {
			total += len(c)
		}
		if total != len(long) {
			t.Errorf("lost content: total %d vs original %d", total, len(long))
		}
	})
}
