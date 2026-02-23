package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robkerr1992/driftcal/internal/action"
)

// handleCallbackQuery processes inline keyboard button presses.
func (b *Bot) handleCallbackQuery(ctx context.Context, cq *CallbackQuery) {
	// Always answer the callback to dismiss the loading spinner.
	defer func() {
		if err := b.client.AnswerCallbackQuery(ctx, cq.ID, ""); err != nil {
			b.log.Warn().Err(err).Msg("failed to answer callback query")
		}
	}()

	data := cq.Data

	// Route conversation callbacks.
	if strings.HasPrefix(data, "conv:") {
		b.handleConversationCallback(ctx, cq)
		return
	}

	// Parse callback data: {action}:{type}:{id}
	parts := strings.SplitN(data, ":", 3)
	if len(parts) != 3 {
		b.log.Warn().Str("data", data).Msg("invalid callback data format")
		return
	}

	cbAction := parts[0]
	cbType := parts[1]
	id, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		b.log.Warn().Str("data", data).Msg("invalid callback ID")
		return
	}

	chatID := int64(0)
	msgID := 0
	if cq.Message != nil {
		chatID = cq.Message.Chat.ID
		msgID = cq.Message.MessageID
	}
	if chatID == 0 {
		return
	}

	switch {
	case cbAction == "approve" && cbType == "s":
		b.callbackApproveSuggestion(ctx, chatID, msgID, id)
	case cbAction == "reject" && cbType == "s":
		b.callbackRejectSuggestion(ctx, chatID, msgID, id)
	case cbAction == "complete" && cbType == "g":
		b.callbackCompleteGoal(ctx, chatID, msgID, id)
	case cbAction == "skip" && cbType == "g":
		b.callbackSkipGoal(ctx, chatID, msgID, id)
	default:
		b.log.Warn().Str("data", data).Msg("unknown callback action")
	}
}

func (b *Bot) callbackApproveSuggestion(ctx context.Context, chatID int64, msgID int, id int64) {
	_, err := action.ApproveSuggestion(ctx, b.queries, b.nylasClient, id, b.log)
	if err != nil {
		b.editResult(ctx, chatID, msgID, fmt.Sprintf("Failed to approve: %s", err.Error()))
		return
	}
	b.editResult(ctx, chatID, msgID, "Added to calendar!")
}

func (b *Bot) callbackRejectSuggestion(ctx context.Context, chatID int64, msgID int, id int64) {
	_, err := action.RejectSuggestion(ctx, b.queries, id, "", b.log)
	if err != nil {
		b.editResult(ctx, chatID, msgID, fmt.Sprintf("Failed to reject: %s", err.Error()))
		return
	}
	b.editResult(ctx, chatID, msgID, "Skipped")
}

func (b *Bot) callbackCompleteGoal(ctx context.Context, chatID int64, msgID int, instanceID int64) {
	// Look up the goal instance to get the goal_id.
	instance, err := b.queries.GetGoalInstance(ctx, instanceID)
	if err != nil {
		b.editResult(ctx, chatID, msgID, fmt.Sprintf("Failed: %s", err.Error()))
		return
	}

	_, err = action.CompleteGoalInstance(ctx, b.queries, instance.GoalID, instanceID, b.log)
	if err != nil {
		b.editResult(ctx, chatID, msgID, fmt.Sprintf("Failed to complete: %s", err.Error()))
		return
	}
	b.editResult(ctx, chatID, msgID, "Completed!")
}

func (b *Bot) callbackSkipGoal(ctx context.Context, chatID int64, msgID int, instanceID int64) {
	instance, err := b.queries.GetGoalInstance(ctx, instanceID)
	if err != nil {
		b.editResult(ctx, chatID, msgID, fmt.Sprintf("Failed: %s", err.Error()))
		return
	}

	_, rescheduled, err := action.SkipGoalInstance(ctx, b.queries, b.prefs, b.nylasClient, instance.GoalID, instanceID, b.log)
	if err != nil {
		b.editResult(ctx, chatID, msgID, fmt.Sprintf("Failed to skip: %s", err.Error()))
		return
	}

	if rescheduled != nil {
		tz, _ := b.prefs.Timezone(ctx)
		if tz == nil {
			tz = time.UTC
		}
		newTime := rescheduled.ScheduledStart.In(tz).Format("15:04")
		b.editResult(ctx, chatID, msgID, fmt.Sprintf("Skipped! Rescheduled to %s", newTime))
	} else {
		b.editResult(ctx, chatID, msgID, "Skipped (no available slot to reschedule)")
	}
}

// editResult replaces a message with a result string and removes the keyboard.
func (b *Bot) editResult(ctx context.Context, chatID int64, msgID int, result string) {
	text := EscapeMD2(result)
	if err := b.client.EditMessageText(ctx, chatID, msgID, text, &SendOpts{
		ParseMode:   "MarkdownV2",
		ReplyMarkup: &InlineKeyboard{InlineKeyboard: [][]InlineButton{}}, // Remove keyboard
	}); err != nil {
		b.log.Error().Err(err).Msg("failed to edit callback result")
	}
}
