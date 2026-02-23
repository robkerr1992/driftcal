package telegram

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/internal/action"
)

func newTestBot(secret string, authorizedUser int64) *Bot {
	log := zerolog.Nop()
	client := NewWithBaseURL("test", "http://localhost")
	return NewBot(client, authorizedUser, secret, nil, nil, (action.NylasEventCreator)(nil), nil, log)
}

func TestWebhookHandler_SecretValidation(t *testing.T) {
	bot := newTestBot("my-secret", 0)
	handler := WebhookHandler(bot)

	t.Run("missing secret returns 200 silently", func(t *testing.T) {
		body, _ := json.Marshal(Update{UpdateID: 1})
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("wrong secret returns 200 silently", func(t *testing.T) {
		body, _ := json.Marshal(Update{UpdateID: 1})
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("correct secret passes", func(t *testing.T) {
		body, _ := json.Marshal(Update{
			UpdateID: 1,
			Message: &Message{
				From: &User{ID: 999},
				Chat: Chat{ID: 999},
				Text: "hello",
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
		req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "my-secret")
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestWebhookHandler_UserAuthorization(t *testing.T) {
	bot := newTestBot("", 12345)
	handler := WebhookHandler(bot)

	t.Run("unauthorized user silently ignored", func(t *testing.T) {
		body, _ := json.Marshal(Update{
			UpdateID: 1,
			Message: &Message{
				From: &User{ID: 99999},
				Chat: Chat{ID: 99999},
				Text: "/help",
			},
		})
		req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
		w := httptest.NewRecorder()
		handler(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})
}

func TestWebhookHandler_InvalidJSON(t *testing.T) {
	bot := newTestBot("", 0)
	handler := WebhookHandler(bot)

	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()
	handler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 even for bad JSON, got %d", w.Code)
	}
}

func TestUpdateUserID(t *testing.T) {
	tests := []struct {
		name string
		u    Update
		want int64
	}{
		{"message", Update{Message: &Message{From: &User{ID: 42}}}, 42},
		{"callback", Update{CallbackQuery: &CallbackQuery{From: User{ID: 99}}}, 99},
		{"empty", Update{}, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateUserID(tt.u)
			if got != tt.want {
				t.Errorf("updateUserID() = %d, want %d", got, tt.want)
			}
		})
	}
}
