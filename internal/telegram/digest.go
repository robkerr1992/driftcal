package telegram

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

// SendDailyDigest sends the daily digest for a given date to the configured chat.
// This is exported so the future cron scheduler (Milestone 1.7) can call it.
func (b *Bot) SendDailyDigest(ctx context.Context, date time.Time) error {
	// Load telegram chat ID from preferences.
	chatIDStr, err := b.prefs.Get(ctx, "telegram_chat_id")
	if err != nil || chatIDStr == "" {
		return fmt.Errorf("telegram_chat_id not configured (send /start first)")
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram_chat_id: %w", err)
	}

	tz, err := b.prefs.Timezone(ctx)
	if err != nil {
		return fmt.Errorf("loading timezone: %w", err)
	}

	dayLocal := date.In(tz)
	dayStart := time.Date(dayLocal.Year(), dayLocal.Month(), dayLocal.Day(), 0, 0, 0, 0, tz).UTC()
	dayEnd := dayStart.Add(24 * time.Hour)

	goals, err := b.queries.ListScheduledGoalInstancesInRangeWithLabel(ctx, sqlcdb.ListScheduledGoalInstancesInRangeWithLabelParams{
		RangeEnd:   dayEnd,
		RangeStart: dayStart,
	})
	if err != nil {
		return fmt.Errorf("loading goal instances: %w", err)
	}

	suggestions, err := b.queries.ListPendingSuggestionsForDate(ctx, date)
	if err != nil {
		return fmt.Errorf("loading suggestions: %w", err)
	}

	// Send digest header.
	header := FormatDailyDigest(date, goals, suggestions, tz)
	b.sendText(ctx, chatID, header)

	// Send individual goal cards with action buttons.
	for _, gi := range goals {
		text, kb := FormatGoalInstanceCard(gi, tz)
		if _, err := b.sendWithKeyboard(ctx, chatID, text, kb); err != nil {
			b.log.Error().Err(err).Int64("instance_id", gi.ID).Msg("failed to send goal card")
		}
	}

	// Send individual suggestion cards with action buttons.
	for _, s := range suggestions {
		text, kb := FormatSuggestionCard(s, tz)
		if _, err := b.sendWithKeyboard(ctx, chatID, text, kb); err != nil {
			b.log.Error().Err(err).Int64("suggestion_id", s.ID).Msg("failed to send suggestion card")
		}
	}

	return nil
}
