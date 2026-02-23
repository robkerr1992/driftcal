package suggest

import (
	"strings"
	"testing"
	"time"

	"github.com/robkerr1992/driftcal/internal/weather"
)

func TestBuildPrompt_WithWeather(t *testing.T) {
	input := PromptInput{
		Date: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		Gaps: []GapContext{
			{Number: 1, StartTime: "10:00", EndTime: "12:00", DurationMinutes: 120, TimeOfDay: "morning"},
		},
		Weather: weather.Forecast{
			TempHighF: 65, TempLowF: 42, Conditions: "Clear",
			PrecipChance: 10, Sunrise: "06:45", Sunset: "17:30",
		},
		Preferences:   map[string]string{"city": "Washington", "state": "DC"},
		EnergyProfile: map[string]string{"morning": "high", "afternoon": "medium", "evening": "low"},
	}

	system, user := BuildPrompt(input)

	if system == "" {
		t.Error("system prompt should not be empty")
	}
	if !strings.Contains(user, "Clear") {
		t.Error("user prompt should contain weather conditions")
	}
	if !strings.Contains(user, "65°F") {
		t.Error("user prompt should contain high temp")
	}
	if !strings.Contains(user, "Washington") {
		t.Error("user prompt should contain city")
	}
	if !strings.Contains(user, "Gap 1") {
		t.Error("user prompt should contain gap info")
	}
}

func TestBuildPrompt_WithoutWeather(t *testing.T) {
	input := PromptInput{
		Date: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		Gaps: []GapContext{
			{Number: 1, StartTime: "10:00", EndTime: "12:00", DurationMinutes: 120, TimeOfDay: "morning"},
		},
		Weather: weather.Forecast{}, // zero value
	}

	_, user := BuildPrompt(input)

	if strings.Contains(user, "Tomorrow's Context") {
		t.Error("user prompt should NOT contain weather section when forecast is empty")
	}
}

func TestBuildPrompt_GapFormatting(t *testing.T) {
	input := PromptInput{
		Date: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		Gaps: []GapContext{
			{Number: 1, StartTime: "09:00", EndTime: "10:30", DurationMinutes: 90, TimeOfDay: "morning", BeforeEvent: "Team standup", AfterEvent: "Lunch"},
			{Number: 2, StartTime: "14:00", EndTime: "16:00", DurationMinutes: 120, TimeOfDay: "afternoon"},
		},
	}

	_, user := BuildPrompt(input)

	if !strings.Contains(user, "Gap 1: 09:00-10:30 (90 min)") {
		t.Error("gap 1 not formatted correctly")
	}
	if !strings.Contains(user, "Before: Team standup") {
		t.Error("before event not included")
	}
	if !strings.Contains(user, "After: Lunch") {
		t.Error("after event not included")
	}
	if !strings.Contains(user, "Gap 2") {
		t.Error("gap 2 not included")
	}
}

func TestBuildPrompt_HistoryCappedAt30(t *testing.T) {
	history := make([]HistoryItem, 40)
	for i := range history {
		history[i] = HistoryItem{Date: "2026-02-01", Title: "Item", Category: "outdoor", Status: "approved"}
	}

	input := PromptInput{
		Date:          time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		Gaps:          []GapContext{{Number: 1, StartTime: "10:00", EndTime: "12:00", DurationMinutes: 120, TimeOfDay: "morning"}},
		RecentHistory: history,
	}

	_, user := BuildPrompt(input)

	count := strings.Count(user, "\"Item\"")
	if count > 30 {
		t.Errorf("history items = %d, want max 30", count)
	}
}

func TestBuildPrompt_ScheduledGoals(t *testing.T) {
	input := PromptInput{
		Date: time.Date(2026, 2, 23, 0, 0, 0, 0, time.UTC),
		Gaps: []GapContext{{Number: 1, StartTime: "14:00", EndTime: "16:00", DurationMinutes: 120, TimeOfDay: "afternoon"}},
		ScheduledGoals: []GoalContext{
			{StartTime: "09:00", EndTime: "10:00", Label: "Study Go", Category: "study", EnergyLevel: "high"},
		},
	}

	_, user := BuildPrompt(input)

	if !strings.Contains(user, "Study Go") {
		t.Error("scheduled goal not included in prompt")
	}
	if !strings.Contains(user, "complement them") {
		t.Error("complement note not included")
	}
}
