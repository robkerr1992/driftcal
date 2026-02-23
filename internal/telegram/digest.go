package telegram

import (
	"context"
	"fmt"
	"strconv"
	"time"
)

// SendDailyDigest sends the daily digest for a given date to the configured chat.
func (b *Bot) SendDailyDigest(ctx context.Context, date time.Time) error {
	chatID, err := b.resolvedChatID(ctx)
	if err != nil {
		return err
	}

	b.sendDigestForDate(ctx, chatID, date)
	return nil
}

// SendError sends an error notification to the configured Telegram chat.
func (b *Bot) SendError(ctx context.Context, msg string) error {
	chatID, err := b.resolvedChatID(ctx)
	if err != nil {
		return err
	}

	text := "⚠ *DriftCal error:*\n" + EscapeMD2(msg)
	b.sendText(ctx, chatID, text)
	return nil
}

// resolvedChatID loads and parses the telegram_chat_id preference.
func (b *Bot) resolvedChatID(ctx context.Context) (int64, error) {
	chatIDStr, err := b.prefs.Get(ctx, "telegram_chat_id")
	if err != nil || chatIDStr == "" {
		return 0, fmt.Errorf("telegram_chat_id not configured (send /start first)")
	}

	chatID, err := strconv.ParseInt(chatIDStr, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid telegram_chat_id: %w", err)
	}
	return chatID, nil
}
