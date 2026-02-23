package telegram

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
)

// Update represents a Telegram update delivered via webhook.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message,omitempty"`
	CallbackQuery *CallbackQuery `json:"callback_query,omitempty"`
}

// Message represents a Telegram message.
type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from,omitempty"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
	Date      int64  `json:"date"`
}

// CallbackQuery represents a callback from an inline keyboard button.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    User     `json:"from"`
	Message *Message `json:"message,omitempty"`
	Data    string   `json:"data"`
}

// User represents a Telegram user.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	Username  string `json:"username"`
	IsBot     bool   `json:"is_bot"`
}

// Chat represents a Telegram chat.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// WebhookHandler returns an http.HandlerFunc that processes Telegram webhook updates.
// It validates the secret token header and silently ignores unauthorized users.
// Always returns 200 to Telegram to prevent redelivery.
func WebhookHandler(bot *Bot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Validate webhook secret via constant-time comparison.
		if bot.webhookSecret != "" {
			headerSecret := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
			if subtle.ConstantTimeCompare([]byte(headerSecret), []byte(bot.webhookSecret)) != 1 {
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		var update Update
		if err := json.NewDecoder(r.Body).Decode(&update); err != nil {
			bot.log.Warn().Err(err).Msg("telegram: failed to decode update")
			w.WriteHeader(http.StatusOK)
			return
		}

		// Check authorization: only handle updates from the authorized user.
		userID := updateUserID(update)
		if userID == 0 || (bot.authorizedUser != 0 && userID != bot.authorizedUser) {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Handle update asynchronously to not block the webhook response.
		// We intentionally use context.Background() (inside handleUpdate) rather
		// than r.Context(), because the request context is cancelled when this
		// handler returns 200.
		go bot.handleUpdate(update)

		w.WriteHeader(http.StatusOK)
	}
}

// updateUserID extracts the user ID from an update.
func updateUserID(u Update) int64 {
	if u.Message != nil && u.Message.From != nil {
		return u.Message.From.ID
	}
	if u.CallbackQuery != nil {
		return u.CallbackQuery.From.ID
	}
	return 0
}
