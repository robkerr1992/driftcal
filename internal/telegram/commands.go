package telegram

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/goal"
)

var hhmmRegex = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func (b *Bot) cmdStart(ctx context.Context, msg *Message) {
	chatID := msg.Chat.ID

	// Store the telegram chat ID in preferences for digest delivery.
	if err := b.prefs.Set(ctx, "telegram_chat_id", strconv.FormatInt(chatID, 10)); err != nil {
		b.log.Error().Err(err).Msg("failed to store telegram_chat_id")
	}

	welcome := "*Welcome to DriftCal\\!*\n\n" +
		"I'll send you daily digests with activity suggestions and goal updates\\.\n\n" +
		"Use /help to see available commands\\."
	b.sendText(ctx, chatID, welcome)
}

func (b *Bot) cmdToday(ctx context.Context, msg *Message) {
	b.sendDigestForDate(ctx, msg.Chat.ID, todayUTC())
}

func (b *Bot) cmdTomorrow(ctx context.Context, msg *Message) {
	b.sendDigestForDate(ctx, msg.Chat.ID, tomorrowUTC())
}

func (b *Bot) cmdRegenerate(ctx context.Context, msg *Message) {
	chatID := msg.Chat.ID

	if b.pipeline == nil {
		b.sendText(ctx, chatID, EscapeMD2("Pipeline not configured."))
		return
	}

	// Rate limit: 1 per hour.
	b.regenMu.Lock()
	elapsed := time.Since(b.lastRegenerate)
	if elapsed < time.Hour {
		b.regenMu.Unlock()
		remaining := time.Hour - elapsed
		b.sendText(ctx, chatID, EscapeMD2(fmt.Sprintf(
			"Rate limited. Try again in %d minutes.", int(remaining.Minutes())+1)))
		return
	}
	b.lastRegenerate = time.Now()
	b.regenMu.Unlock()

	// Send "regenerating" message, then edit it when done.
	msgID, err := b.client.SendMessage(ctx, chatID, EscapeMD2("Regenerating..."), &SendOpts{ParseMode: "MarkdownV2"})
	if err != nil {
		b.log.Error().Err(err).Msg("failed to send regenerating message")
		return
	}

	tomorrow := tomorrowUTC()

	// Run pipeline (this can take a while).
	pipeErr := b.pipeline.RunDailyPipeline(ctx, tomorrow)

	var resultText string
	if pipeErr != nil {
		b.log.Error().Err(pipeErr).Msg("pipeline failed during /regenerate")
		resultText = EscapeMD2("Pipeline failed. Check server logs for details.")
	} else {
		resultText = EscapeMD2("Pipeline completed! Use /tomorrow to see results.")
	}

	if err := b.client.EditMessageText(ctx, chatID, msgID, resultText, &SendOpts{ParseMode: "MarkdownV2"}); err != nil {
		b.log.Error().Err(err).Msg("failed to edit regenerate result message")
	}
}

func (b *Bot) cmdGaps(ctx context.Context, msg *Message) {
	chatID := msg.Chat.ID

	tz, err := b.prefs.Timezone(ctx)
	if err != nil {
		b.sendText(ctx, chatID, EscapeMD2("Failed to load timezone."))
		return
	}

	today := todayUTC()
	endDate := today.AddDate(0, 0, 2)

	gaps, err := b.queries.ListDailyGapsForDateRange(ctx, sqlcdb.ListDailyGapsForDateRangeParams{
		StartDate: today,
		EndDate:   endDate,
	})
	if err != nil {
		b.log.Error().Err(err).Msg("failed to list gaps")
		b.sendText(ctx, chatID, EscapeMD2("Failed to load gaps."))
		return
	}

	text := FormatGapsList(gaps, tz)
	for _, chunk := range SplitMessage(text) {
		b.sendText(ctx, chatID, chunk)
	}
}

func (b *Bot) cmdGoals(ctx context.Context, msg *Message) {
	chatID := msg.Chat.ID

	goals, err := b.queries.ListActiveGoals(ctx)
	if err != nil {
		b.log.Error().Err(err).Msg("failed to list goals")
		b.sendText(ctx, chatID, EscapeMD2("Failed to load goals."))
		return
	}

	weekStart := goal.MondayOf(time.Now())
	counts, err := b.queries.ListGoalInstanceCountsForWeek(ctx, weekStart)
	if err != nil {
		b.log.Error().Err(err).Msg("failed to list instance counts")
	}

	// Build counts map: goalID → [scheduled, completed].
	weekCounts := make(map[int64][2]int)
	for _, c := range counts {
		wc := weekCounts[c.GoalID]
		switch c.Status {
		case "scheduled":
			wc[0] = int(c.Count)
		case "completed":
			wc[1] = int(c.Count)
		}
		weekCounts[c.GoalID] = wc
	}

	text := FormatGoalsList(goals, weekCounts)
	for _, chunk := range SplitMessage(text) {
		b.sendText(ctx, chatID, chunk)
	}
}

func (b *Bot) cmdAddGoal(ctx context.Context, msg *Message) {
	b.startConversation(ctx, msg.Chat.ID)
}

func (b *Bot) cmdPreferences(ctx context.Context, msg *Message) {
	chatID := msg.Chat.ID

	prefs, err := b.prefs.All(ctx)
	if err != nil {
		b.log.Error().Err(err).Msg("failed to load preferences")
		b.sendText(ctx, chatID, EscapeMD2("Failed to load preferences."))
		return
	}

	text := FormatPreferences(prefs)
	for _, chunk := range SplitMessage(text) {
		b.sendText(ctx, chatID, chunk)
	}
}

func (b *Bot) cmdBlock(ctx context.Context, msg *Message, args string) {
	chatID := msg.Chat.ID

	// Parse: /block Label HH:MM-HH:MM [day_of_week]
	parts := strings.Fields(args)
	if len(parts) < 2 {
		b.sendText(ctx, chatID, EscapeMD2("Usage: /block Label HH:MM-HH:MM [day_of_week 0-6]"))
		return
	}

	label := parts[0]
	timeRange := parts[1]

	times := strings.SplitN(timeRange, "-", 2)
	if len(times) != 2 {
		b.sendText(ctx, chatID, EscapeMD2("Time range must be HH:MM-HH:MM format."))
		return
	}

	startTime := times[0]
	endTime := times[1]

	if !hhmmRegex.MatchString(startTime) || !hhmmRegex.MatchString(endTime) {
		b.sendText(ctx, chatID, EscapeMD2("Times must be HH:MM format (e.g. 09:00-17:00)."))
		return
	}
	if endTime <= startTime {
		b.sendText(ctx, chatID, EscapeMD2("End time must be after start time."))
		return
	}

	var dow sql.NullInt64
	if len(parts) >= 3 {
		day, err := strconv.ParseInt(parts[2], 10, 64)
		if err != nil || day < 0 || day > 6 {
			b.sendText(ctx, chatID, EscapeMD2("day_of_week must be 0-6."))
			return
		}
		dow = sql.NullInt64{Int64: day, Valid: true}
	}

	block, err := b.queries.CreateProtectedBlock(ctx, sqlcdb.CreateProtectedBlockParams{
		Label:     label,
		DayOfWeek: dow,
		StartTime: startTime,
		EndTime:   endTime,
	})
	if err != nil {
		b.log.Error().Err(err).Msg("failed to create protected block")
		b.sendText(ctx, chatID, EscapeMD2("Failed to create block."))
		return
	}

	b.sendText(ctx, chatID, EscapeMD2(fmt.Sprintf(
		"Created block #%d: %s %s-%s", block.ID, block.Label, block.StartTime, block.EndTime)))
}

func (b *Bot) cmdUnblock(ctx context.Context, msg *Message, args string) {
	chatID := msg.Chat.ID

	id, err := strconv.ParseInt(strings.TrimSpace(args), 10, 64)
	if err != nil {
		b.sendText(ctx, chatID, EscapeMD2("Usage: /unblock <block_id>"))
		return
	}

	if _, err := b.queries.GetProtectedBlock(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			b.sendText(ctx, chatID, EscapeMD2("Block not found."))
			return
		}
		b.sendText(ctx, chatID, EscapeMD2("Failed to look up block."))
		return
	}

	if err := b.queries.DeleteProtectedBlock(ctx, id); err != nil {
		b.log.Error().Err(err).Int64("block_id", id).Msg("failed to delete block")
		b.sendText(ctx, chatID, EscapeMD2("Failed to delete block."))
		return
	}

	b.sendText(ctx, chatID, EscapeMD2(fmt.Sprintf("Deleted block #%d.", id)))
}

func (b *Bot) cmdStatus(ctx context.Context, msg *Message) {
	chatID := msg.Chat.ID

	run, err := b.queries.GetLatestPipelineRun(ctx)
	var runPtr *sqlcdb.PipelineRun
	if err == nil {
		runPtr = &run
	}

	uptime := time.Since(b.startTime)
	text := FormatStatus(runPtr, uptime)
	b.sendText(ctx, chatID, text)
}

func (b *Bot) cmdHelp(ctx context.Context, msg *Message) {
	help := `*DriftCal Bot Commands*

/today \- Today's schedule and suggestions
/tomorrow \- Tomorrow's schedule and suggestions
/regenerate \- Re\-run the daily pipeline
/gaps \- Show free time gaps \(3 days\)
/goals \- Active goals with weekly progress
/addgoal \- Create a new goal \(guided\)
/preferences \- View your preferences
/block \- Create a protected time block
/unblock \- Delete a protected time block
/status \- System status
/help \- This help message`

	b.sendText(ctx, chatID(msg), help)
}

// sendDigestForDate loads goals and suggestions for a date and sends them.
func (b *Bot) sendDigestForDate(ctx context.Context, chatID int64, date time.Time) {
	tz, err := b.prefs.Timezone(ctx)
	if err != nil {
		b.sendText(ctx, chatID, EscapeMD2("Failed to load timezone."))
		return
	}

	dayLocal := date.In(tz)
	dayStart := time.Date(dayLocal.Year(), dayLocal.Month(), dayLocal.Day(), 0, 0, 0, 0, tz).UTC()
	dayEnd := dayStart.Add(24 * time.Hour)

	goals, err := b.queries.ListScheduledGoalInstancesInRangeWithLabel(ctx, sqlcdb.ListScheduledGoalInstancesInRangeWithLabelParams{
		RangeEnd:   dayEnd,
		RangeStart: dayStart,
	})
	if err != nil {
		b.log.Error().Err(err).Msg("failed to list goal instances")
		b.sendText(ctx, chatID, EscapeMD2("Failed to load goals."))
		return
	}

	suggestions, err := b.queries.ListPendingSuggestionsForDate(ctx, date)
	if err != nil {
		b.log.Error().Err(err).Msg("failed to list suggestions")
		b.sendText(ctx, chatID, EscapeMD2("Failed to load suggestions."))
		return
	}

	// Send digest header.
	header := FormatDailyDigest(date, goals, suggestions, tz)
	b.sendText(ctx, chatID, header)

	// Send individual goal cards with action buttons.
	for _, gi := range goals {
		text, kb := FormatGoalInstanceCard(gi, tz)
		if _, err := b.sendWithKeyboard(ctx, chatID, text, kb); err != nil {
			b.log.Error().Err(err).Msg("failed to send goal card")
		}
	}

	// Send individual suggestion cards with action buttons.
	for _, s := range suggestions {
		text, kb := FormatSuggestionCard(s, tz)
		if _, err := b.sendWithKeyboard(ctx, chatID, text, kb); err != nil {
			b.log.Error().Err(err).Msg("failed to send suggestion card")
		}
	}
}

func todayUTC() time.Time {
	now := time.Now().UTC()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

func tomorrowUTC() time.Time {
	return todayUTC().AddDate(0, 0, 1)
}

func chatID(msg *Message) int64 {
	return msg.Chat.ID
}
