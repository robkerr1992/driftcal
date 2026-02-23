package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendMessage(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/bottest-token/sendMessage" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if body["chat_id"].(float64) != 123 {
			t.Errorf("expected chat_id 123, got %v", body["chat_id"])
		}
		if body["text"] != "hello" {
			t.Errorf("expected text 'hello', got %v", body["text"])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 42},
		})
	}))
	defer ts.Close()

	client := NewWithBaseURL("test-token", ts.URL)
	msgID, err := client.SendMessage(context.Background(), 123, "hello", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgID != 42 {
		t.Errorf("expected message_id 42, got %d", msgID)
	}
}

func TestSendMessageWithOpts(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if body["parse_mode"] != "MarkdownV2" {
			t.Errorf("expected MarkdownV2 parse mode, got %v", body["parse_mode"])
		}
		if body["reply_markup"] == nil {
			t.Error("expected reply_markup")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	}))
	defer ts.Close()

	client := NewWithBaseURL("test-token", ts.URL)
	kb := NewInlineKeyboard([]InlineButton{
		{Text: "OK", CallbackData: "ok"},
	})
	_, err := client.SendMessage(context.Background(), 123, "test", &SendOpts{
		ParseMode:   "MarkdownV2",
		ReplyMarkup: kb,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEditMessageText(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/editMessageText" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	client := NewWithBaseURL("test-token", ts.URL)
	err := client.EditMessageText(context.Background(), 123, 42, "updated", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnswerCallbackQuery(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/answerCallbackQuery" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	client := NewWithBaseURL("test-token", ts.URL)
	err := client.AnswerCallbackQuery(context.Background(), "query-123", "done")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSetWebhook(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/bottest-token/setWebhook" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if body["url"] != "https://example.com/webhook" {
			t.Errorf("unexpected url: %v", body["url"])
		}
		if body["secret_token"] != "my-secret" {
			t.Errorf("unexpected secret: %v", body["secret_token"])
		}

		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer ts.Close()

	client := NewWithBaseURL("test-token", ts.URL)
	err := client.SetWebhook(context.Background(), "https://example.com/webhook", "my-secret")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSendMessageError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"description":"Too Many Requests"}`))
	}))
	defer ts.Close()

	client := NewWithBaseURL("test-token", ts.URL)
	_, err := client.SendMessage(context.Background(), 123, "hello", nil)
	if err == nil {
		t.Fatal("expected error")
	}
}
