package suggest

import (
	"fmt"
	"strings"
	"time"

	"github.com/robkerr1992/driftcal/internal/weather"
)

// PromptInput holds all data needed to build the suggestion prompt.
type PromptInput struct {
	Date           time.Time
	Gaps           []GapContext
	Weather        weather.Forecast
	Preferences    map[string]string
	EnergyProfile  map[string]string
	ScheduledGoals []GoalContext
	RecentHistory  []HistoryItem
}

// GapContext describes a free gap for the prompt.
type GapContext struct {
	Number          int
	StartTime       string // "HH:MM"
	EndTime         string // "HH:MM"
	DurationMinutes int
	TimeOfDay       string
	BeforeEvent     string
	AfterEvent      string
}

// GoalContext describes a scheduled goal instance for the prompt.
type GoalContext struct {
	StartTime   string // "HH:MM"
	EndTime     string // "HH:MM"
	Label       string
	Category    string
	EnergyLevel string
}

// HistoryItem describes a recent suggestion for the prompt.
type HistoryItem struct {
	Date     string // "YYYY-MM-DD"
	Title    string
	Category string
	Status   string
}

const systemPrompt = `You are DriftCal, a personal lifestyle assistant that suggests activities to fill free time in someone's calendar. Your suggestions should be:

- **Specific and actionable** — not "go for a walk" but "Walk the Capital Crescent Trail from Bethesda to Georgetown (3.5 miles, mostly flat)"
- **Context-aware** — consider weather, time of day, energy level, what's before/after
- **Varied** — mix categories (outdoor, cultural, creative, social, culinary, fitness, relaxation) across the day and across days
- **Realistic** — account for travel time, prep time, and wind-down between activities
- **Encouraging but not pushy** — frame suggestions as invitations, not obligations

Never suggest the same activity twice within 14 days. Vary locations, categories, and energy levels. If the user has rejected a category recently, reduce its frequency but don't eliminate it entirely.

Respond with a JSON array of suggestions. Each suggestion must include all required fields. Do not include any text outside the JSON.`

// BuildPrompt generates the system and user prompts from the input data.
func BuildPrompt(input PromptInput) (system, user string) {
	var b strings.Builder

	b.WriteString("Generate activity suggestions for the free gaps in my schedule tomorrow.\n\n")

	// Profile section.
	b.WriteString("## My Profile\n")
	if city, ok := input.Preferences["city"]; ok {
		state := input.Preferences["state"]
		b.WriteString(fmt.Sprintf("- Location: %s, %s\n", city, state))
	}
	if interests, ok := input.Preferences["interests"]; ok && interests != "" {
		b.WriteString(fmt.Sprintf("- Interests: %s\n", interests))
	}
	if len(input.EnergyProfile) > 0 {
		b.WriteString(fmt.Sprintf("- Energy profile: Morning=%s, Afternoon=%s, Evening=%s\n",
			energyOrDefault(input.EnergyProfile, "morning"),
			energyOrDefault(input.EnergyProfile, "afternoon"),
			energyOrDefault(input.EnergyProfile, "evening")))
	}
	b.WriteString("\n")

	// Weather section (only if forecast is populated).
	if input.Weather.Conditions != "" {
		b.WriteString("## Tomorrow's Context\n")
		b.WriteString(fmt.Sprintf("- Date: %s (%s)\n", input.Date.Format("2006-01-02"), input.Date.Format("Monday")))
		b.WriteString(fmt.Sprintf("- Weather: %s\n", input.Weather.Conditions))
		b.WriteString(fmt.Sprintf("  - High: %.0f°F, Low: %.0f°F\n", input.Weather.TempHighF, input.Weather.TempLowF))
		b.WriteString(fmt.Sprintf("  - Precipitation: %d%%\n", input.Weather.PrecipChance))
		b.WriteString(fmt.Sprintf("  - Sunrise: %s, Sunset: %s\n", input.Weather.Sunrise, input.Weather.Sunset))
		b.WriteString("\n")
	}

	// Scheduled goals.
	if len(input.ScheduledGoals) > 0 {
		b.WriteString("## Recurring Goals Already Scheduled Tomorrow\n")
		for _, g := range input.ScheduledGoals {
			b.WriteString(fmt.Sprintf("- %s-%s: %s (%s, %s energy)\n",
				g.StartTime, g.EndTime, g.Label, g.Category, g.EnergyLevel))
		}
		b.WriteString("Note: These are already on the calendar. Suggest activities that complement them.\n\n")
	}

	// Free gaps.
	b.WriteString("## Free Gaps (suggest activities for these)\n")
	for _, g := range input.Gaps {
		b.WriteString(fmt.Sprintf("- Gap %d: %s-%s (%d min)\n", g.Number, g.StartTime, g.EndTime, g.DurationMinutes))
		b.WriteString(fmt.Sprintf("  - Time of day: %s\n", g.TimeOfDay))
		if g.BeforeEvent != "" {
			b.WriteString(fmt.Sprintf("  - Before: %s\n", g.BeforeEvent))
		}
		if g.AfterEvent != "" {
			b.WriteString(fmt.Sprintf("  - After: %s\n", g.AfterEvent))
		}
	}
	b.WriteString("\n")

	// Recent history (capped at 30).
	if len(input.RecentHistory) > 0 {
		b.WriteString("## Recent Suggestion History (last 14 days)\n")
		cap := len(input.RecentHistory)
		if cap > 30 {
			cap = 30
		}
		for _, h := range input.RecentHistory[:cap] {
			b.WriteString(fmt.Sprintf("- %s: \"%s\" (%s) — %s\n", h.Date, h.Title, h.Category, h.Status))
		}
		b.WriteString("\n")
	}

	b.WriteString("## Response Format\nUse the suggest_activities tool to return your suggestions. One suggestion per gap.\n")

	return systemPrompt, b.String()
}

func energyOrDefault(profile map[string]string, key string) string {
	if v, ok := profile[key]; ok {
		return v
	}
	return "medium"
}
