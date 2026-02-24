package telegram

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
)

// ConversationState tracks the /addgoal multi-step flow.
type ConversationState struct {
	ChatID    int64
	Step      string
	Label     string
	Category  string
	Duration  int64
	TimesWeek int64
	TimeOfDay string
	Days      string // "weekdays", "weekends", or "any"
	ExpiresAt time.Time
}

const convTimeout = 10 * time.Minute

// startConversation begins the /addgoal guided flow.
func (b *Bot) startConversation(ctx context.Context, chatID int64) {
	b.convMu.Lock()
	b.conversation = &ConversationState{
		ChatID:    chatID,
		Step:      "label",
		ExpiresAt: time.Now().Add(convTimeout),
	}
	b.convMu.Unlock()

	b.sendText(ctx, chatID, EscapeMD2("Let's create a new goal! What should we call it?"))
}

// handleConversationText processes free-text input during a conversation.
func (b *Bot) handleConversationText(ctx context.Context, msg *Message, text string) {
	b.convMu.Lock()
	conv := b.conversation
	if conv == nil || time.Now().After(conv.ExpiresAt) {
		b.conversation = nil
		b.convMu.Unlock()
		return
	}
	step := conv.Step
	b.convMu.Unlock()

	chatID := msg.Chat.ID

	switch step {
	case "label":
		if len(text) > 100 {
			b.sendText(ctx, chatID, EscapeMD2("Label too long (max 100 characters). Try again:"))
			return
		}

		b.convMu.Lock()
		conv.Label = text
		conv.Step = "category"
		conv.ExpiresAt = time.Now().Add(convTimeout)
		b.convMu.Unlock()

		kb := NewInlineKeyboard(
			[]InlineButton{
				{Text: "Study", CallbackData: "conv:cat:study"},
				{Text: "Workout", CallbackData: "conv:cat:workout"},
				{Text: "Creative", CallbackData: "conv:cat:creative"},
				{Text: "Social", CallbackData: "conv:cat:social"},
			},
			[]InlineButton{
				{Text: "Errand", CallbackData: "conv:cat:errand"},
				{Text: "Health", CallbackData: "conv:cat:health"},
				{Text: "Hobby", CallbackData: "conv:cat:hobby"},
				{Text: "Other", CallbackData: "conv:cat:other"},
			},
		)
		b.sendWithKeyboard(ctx, chatID, EscapeMD2("What category?"), kb)
	}
}

// handleConversationCallback processes inline keyboard presses during conversation.
func (b *Bot) handleConversationCallback(ctx context.Context, cq *CallbackQuery) {
	b.convMu.Lock()
	conv := b.conversation
	if conv == nil || time.Now().After(conv.ExpiresAt) {
		b.conversation = nil
		b.convMu.Unlock()
		return
	}
	b.convMu.Unlock()

	chatID := int64(0)
	if cq.Message != nil {
		chatID = cq.Message.Chat.ID
	}
	if chatID == 0 {
		return
	}

	data := strings.TrimPrefix(cq.Data, "conv:")
	parts := strings.SplitN(data, ":", 2)
	if len(parts) != 2 {
		return
	}

	field := parts[0]
	value := parts[1]

	b.convMu.Lock()
	conv.ExpiresAt = time.Now().Add(convTimeout)

	switch field {
	case "cat":
		conv.Category = value
		conv.Step = "duration"
		b.convMu.Unlock()

		kb := NewInlineKeyboard([]InlineButton{
			{Text: "30 min", CallbackData: "conv:dur:30"},
			{Text: "45 min", CallbackData: "conv:dur:45"},
			{Text: "60 min", CallbackData: "conv:dur:60"},
			{Text: "90 min", CallbackData: "conv:dur:90"},
		})
		b.sendWithKeyboard(ctx, chatID, EscapeMD2("How long per session?"), kb)

	case "dur":
		dur, _ := strconv.ParseInt(value, 10, 64)
		conv.Duration = dur
		conv.Step = "frequency"
		b.convMu.Unlock()

		kb := NewInlineKeyboard([]InlineButton{
			{Text: "1x/wk", CallbackData: "conv:freq:1"},
			{Text: "2x/wk", CallbackData: "conv:freq:2"},
			{Text: "3x/wk", CallbackData: "conv:freq:3"},
			{Text: "4x/wk", CallbackData: "conv:freq:4"},
			{Text: "5x/wk", CallbackData: "conv:freq:5"},
		})
		b.sendWithKeyboard(ctx, chatID, EscapeMD2("How many times per week?"), kb)

	case "freq":
		freq, _ := strconv.ParseInt(value, 10, 64)
		conv.TimesWeek = freq
		conv.Step = "time_of_day"
		b.convMu.Unlock()

		kb := NewInlineKeyboard([]InlineButton{
			{Text: "Any", CallbackData: "conv:tod:any"},
			{Text: "Morning", CallbackData: "conv:tod:morning"},
			{Text: "Afternoon", CallbackData: "conv:tod:afternoon"},
			{Text: "Evening", CallbackData: "conv:tod:evening"},
		})
		b.sendWithKeyboard(ctx, chatID, EscapeMD2("Preferred time of day?"), kb)

	case "tod":
		conv.TimeOfDay = value
		conv.Step = "days"
		b.convMu.Unlock()

		kb := NewInlineKeyboard([]InlineButton{
			{Text: "Weekdays", CallbackData: "conv:days:weekdays"},
			{Text: "Weekends", CallbackData: "conv:days:weekends"},
			{Text: "Any day", CallbackData: "conv:days:any"},
		})
		b.sendWithKeyboard(ctx, chatID, EscapeMD2("Which days?"), kb)

	case "days":
		conv.Days = value
		conv.Step = "confirm"
		// Snapshot fields while holding the lock for the summary message.
		label := conv.Label
		category := conv.Category
		duration := conv.Duration
		timesWeek := conv.TimesWeek
		timeOfDay := conv.TimeOfDay
		days := conv.Days
		b.convMu.Unlock()

		summary := fmt.Sprintf("*New Goal*\n\n"+
			"Label: %s\n"+
			"Category: %s\n"+
			"Duration: %d min\n"+
			"Frequency: %dx/week\n"+
			"Time: %s\n"+
			"Days: %s",
			EscapeMD2(label),
			EscapeMD2(category),
			duration,
			timesWeek,
			EscapeMD2(timeOfDay),
			EscapeMD2(days))

		kb := NewInlineKeyboard([]InlineButton{
			{Text: "Create", CallbackData: "conv:confirm:yes"},
			{Text: "Cancel", CallbackData: "conv:confirm:no"},
		})
		b.sendWithKeyboard(ctx, chatID, summary, kb)

	case "confirm":
		// Snapshot the entire conversation state before unlocking.
		snapshot := *conv
		b.convMu.Unlock()

		if value == "yes" {
			b.createGoalFromConversation(ctx, chatID, &snapshot)
		} else {
			b.sendText(ctx, chatID, EscapeMD2("Goal creation cancelled."))
		}

		b.convMu.Lock()
		b.conversation = nil
		b.convMu.Unlock()

	default:
		b.convMu.Unlock()
	}
}

func (b *Bot) createGoalFromConversation(ctx context.Context, chatID int64, conv *ConversationState) {
	allowedDays := expandDays(conv.Days)

	var allowedDaysJSON sql.NullString
	if len(allowedDays) > 0 && len(allowedDays) < 7 {
		daysJSON, _ := json.Marshal(allowedDays)
		allowedDaysJSON = sql.NullString{String: string(daysJSON), Valid: true}
	}

	created, err := b.queries.CreateGoal(ctx, sqlcdb.CreateGoalParams{
		Label:              conv.Label,
		Category:           conv.Category,
		DurationMinutes:    conv.Duration,
		TimesPerWeek:       conv.TimesWeek,
		PreferredTimeOfDay: sql.NullString{String: conv.TimeOfDay, Valid: true},
		EnergyLevel:        "medium",
		EarliestHour:       "07:00",
		LatestHour:         "22:00",
		AllowedDays:        allowedDaysJSON,
		MinGapBetweenHours: 24,
		Priority:           3,
	})
	if err != nil {
		b.log.Error().Err(err).Msg("failed to create goal from conversation")
		b.sendText(ctx, chatID, EscapeMD2("Failed to create goal. Please try again."))
		return
	}

	b.sendText(ctx, chatID, EscapeMD2(fmt.Sprintf(
		"Goal created! #%d: %s (%dx/week, %d min)",
		created.ID, created.Label, created.TimesPerWeek, created.DurationMinutes)))
}

func expandDays(choice string) []string {
	switch choice {
	case "weekdays":
		return []string{"mon", "tue", "wed", "thu", "fri"}
	case "weekends":
		return []string{"sat", "sun"}
	default: // "any"
		return nil // nil means all days
	}
}
