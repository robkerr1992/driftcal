package telegram

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

// maxMessageLen is the safe limit for a single Telegram message (under the 4096 hard limit).
const maxMessageLen = 3800

// md2Replacer escapes all 18 MarkdownV2 special characters.
// Allocated once at package init; safe for concurrent use.
var md2Replacer = strings.NewReplacer(
	`\`, `\\`,
	`_`, `\_`,
	`*`, `\*`,
	`[`, `\[`,
	`]`, `\]`,
	`(`, `\(`,
	`)`, `\)`,
	`~`, `\~`,
	"`", "\\`",
	`>`, `\>`,
	`#`, `\#`,
	`+`, `\+`,
	`-`, `\-`,
	`=`, `\=`,
	`|`, `\|`,
	`{`, `\{`,
	`}`, `\}`,
	`.`, `\.`,
	`!`, `\!`,
)

// EscapeMD2 escapes all 18 MarkdownV2 special characters.
func EscapeMD2(s string) string {
	return md2Replacer.Replace(s)
}

// FormatDailyDigest builds the digest header and section labels.
// Individual items are sent as separate messages (each with their own keyboard).
func FormatDailyDigest(date time.Time, goals []sqlcdb.ListScheduledGoalInstancesInRangeWithLabelRow, suggestions []sqlcdb.ActivitySuggestion, tz *time.Location) string {
	dayStr := date.In(tz).Format("Monday, January 2")
	var b strings.Builder

	b.WriteString(fmt.Sprintf("*%s*\n\n", EscapeMD2(dayStr)))

	goalCount := len(goals)
	suggCount := len(suggestions)

	if goalCount == 0 && suggCount == 0 {
		b.WriteString(EscapeMD2("Tomorrow's packed! No gaps to fill."))
		return b.String()
	}

	if goalCount > 0 {
		b.WriteString(fmt.Sprintf("*Goals* \\(%d\\)\n", goalCount))
		for _, gi := range goals {
			start := gi.ScheduledStart.In(tz).Format("15:04")
			end := gi.ScheduledEnd.In(tz).Format("15:04")
			b.WriteString(fmt.Sprintf("  %s %s\\-%s\n",
				EscapeMD2(gi.GoalLabel), EscapeMD2(start), EscapeMD2(end)))
		}
	}

	if suggCount > 0 {
		if goalCount > 0 {
			b.WriteString("\n")
		}
		b.WriteString(fmt.Sprintf("*Suggestions* \\(%d\\)\n", suggCount))
		for _, s := range suggestions {
			start := s.StartTime.In(tz).Format("15:04")
			end := s.EndTime.In(tz).Format("15:04")
			b.WriteString(fmt.Sprintf("  %s %s\\-%s\n",
				EscapeMD2(s.Title), EscapeMD2(start), EscapeMD2(end)))
		}
	} else if goalCount > 0 {
		b.WriteString(fmt.Sprintf("\n%s", EscapeMD2("Couldn't generate suggestions today -- I'll try again tomorrow.")))
	}

	return b.String()
}

// FormatSuggestionCard formats a single suggestion with its inline keyboard.
func FormatSuggestionCard(s sqlcdb.ActivitySuggestion, tz *time.Location) (string, *InlineKeyboard) {
	start := s.StartTime.In(tz).Format("15:04")
	end := s.EndTime.In(tz).Format("15:04")

	var b strings.Builder
	b.WriteString(fmt.Sprintf("*%s*\n", EscapeMD2(s.Title)))
	b.WriteString(fmt.Sprintf("%s\\-%s  %s  %s\n",
		EscapeMD2(start), EscapeMD2(end),
		EscapeMD2(s.Category), EscapeMD2(s.EnergyLevel)))

	if s.Description != "" {
		b.WriteString(fmt.Sprintf("\n%s", EscapeMD2(s.Description)))
	}

	kb := NewInlineKeyboard([]InlineButton{
		{Text: "Approve", CallbackData: fmt.Sprintf("approve:s:%d", s.ID)},
		{Text: "Reject", CallbackData: fmt.Sprintf("reject:s:%d", s.ID)},
	})

	return b.String(), kb
}

// FormatGoalInstanceCard formats a goal instance with action buttons.
func FormatGoalInstanceCard(gi sqlcdb.ListScheduledGoalInstancesInRangeWithLabelRow, tz *time.Location) (string, *InlineKeyboard) {
	start := gi.ScheduledStart.In(tz).Format("15:04")
	end := gi.ScheduledEnd.In(tz).Format("15:04")

	text := fmt.Sprintf("*%s*\n%s\\-%s",
		EscapeMD2(gi.GoalLabel),
		EscapeMD2(start), EscapeMD2(end))

	kb := NewInlineKeyboard([]InlineButton{
		{Text: "Complete", CallbackData: fmt.Sprintf("complete:g:%d", gi.ID)},
		{Text: "Skip", CallbackData: fmt.Sprintf("skip:g:%d", gi.ID)},
	})

	return text, kb
}

// FormatGoalsList formats active goals with weekly progress.
func FormatGoalsList(goals []sqlcdb.RecurringGoal, weekCounts map[int64][2]int) string {
	if len(goals) == 0 {
		return EscapeMD2("No active goals. Use /addgoal to create one!")
	}

	var b strings.Builder
	b.WriteString("*Active Goals*\n\n")

	for _, g := range goals {
		scheduled, completed := 0, 0
		if counts, ok := weekCounts[g.ID]; ok {
			scheduled = counts[0]
			completed = counts[1]
		}
		fulfilled := scheduled + completed
		remaining := int(g.TimesPerWeek) - fulfilled
		if remaining < 0 {
			remaining = 0
		}

		progress := progressBar(completed, int(g.TimesPerWeek))

		b.WriteString(fmt.Sprintf("*%s* %s\n", EscapeMD2(g.Label), progress))
		b.WriteString(fmt.Sprintf("  %s  %dmin  %dx/wk\n",
			EscapeMD2(g.Category),
			g.DurationMinutes,
			g.TimesPerWeek))
		b.WriteString(fmt.Sprintf("  This week: %d/%d done, %d remaining\n\n",
			completed, g.TimesPerWeek, remaining))
	}

	return b.String()
}

// FormatGapsList formats gaps grouped by day.
func FormatGapsList(gaps []sqlcdb.DailyGap, tz *time.Location) string {
	if len(gaps) == 0 {
		return EscapeMD2("No gaps found for the next 3 days.")
	}

	var b strings.Builder
	b.WriteString("*Upcoming Gaps*\n\n")

	currentDate := ""
	for _, g := range gaps {
		dayStr := g.GapDate.In(tz).Format("Mon Jan 2")
		if dayStr != currentDate {
			if currentDate != "" {
				b.WriteString("\n")
			}
			b.WriteString(fmt.Sprintf("*%s*\n", EscapeMD2(dayStr)))
			currentDate = dayStr
		}

		start := g.StartTime.In(tz).Format("15:04")
		end := g.EndTime.In(tz).Format("15:04")
		b.WriteString(fmt.Sprintf("  %s\\-%s  \\(%dmin, %s\\)\n",
			EscapeMD2(start), EscapeMD2(end),
			g.DurationMinutes,
			EscapeMD2(g.TimeOfDay)))
	}

	return b.String()
}

// FormatPreferences formats all preferences as a key-value display.
// Keys are sorted for deterministic output.
func FormatPreferences(prefs map[string]string) string {
	if len(prefs) == 0 {
		return EscapeMD2("No preferences set.")
	}

	keys := make([]string, 0, len(prefs))
	for k := range prefs {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("*Preferences*\n\n")

	for _, k := range keys {
		b.WriteString(fmt.Sprintf("*%s:* %s\n", EscapeMD2(k), EscapeMD2(prefs[k])))
	}

	return b.String()
}

// FormatStatus formats the bot/pipeline status.
func FormatStatus(run *sqlcdb.PipelineRun, uptime time.Duration) string {
	var b strings.Builder
	b.WriteString("*Status*\n\n")
	b.WriteString(fmt.Sprintf("Uptime: %s\n", EscapeMD2(formatDuration(uptime))))

	if run != nil {
		b.WriteString(fmt.Sprintf("\nLast pipeline run: %s\n", EscapeMD2(run.RunDate.Format("2006-01-02"))))
		b.WriteString(fmt.Sprintf("Status: %s\n", EscapeMD2(run.Status)))
		if run.LastCompletedStep.Valid {
			b.WriteString(fmt.Sprintf("Last step: %s\n", EscapeMD2(run.LastCompletedStep.String)))
		}
		if run.ErrorMessage.Valid {
			b.WriteString(fmt.Sprintf("Error: %s\n", EscapeMD2(run.ErrorMessage.String)))
		}
	} else {
		b.WriteString(fmt.Sprintf("\n%s", EscapeMD2("No pipeline runs yet.")))
	}

	return b.String()
}

// SplitMessage splits long text into chunks that fit within Telegram's message limit.
func SplitMessage(text string) []string {
	if len(text) <= maxMessageLen {
		return []string{text}
	}

	var chunks []string
	for len(text) > 0 {
		if len(text) <= maxMessageLen {
			chunks = append(chunks, text)
			break
		}

		// Find a good split point (newline before maxMessageLen).
		splitAt := maxMessageLen
		idx := strings.LastIndex(text[:splitAt], "\n")
		if idx > 0 {
			splitAt = idx + 1
		}

		chunks = append(chunks, text[:splitAt])
		text = text[splitAt:]
	}
	return chunks
}

func progressBar(completed, total int) string {
	if total == 0 {
		return ""
	}
	filled := completed
	if filled > total {
		filled = total
	}
	empty := total - filled
	return strings.Repeat("\\|", filled) + strings.Repeat("\\.", empty)
}

func formatDuration(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}
