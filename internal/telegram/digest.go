package telegram

import (
	"context"
	"fmt"
	"strconv"
	"time"
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

	b.sendDigestForDate(ctx, chatID, date)
	return nil
}
