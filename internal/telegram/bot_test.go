package telegram

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"golang.org/x/time/rate"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

// telegramAPIMock records calls made to the Telegram Bot API.
type telegramAPIMock struct {
	mu       sync.Mutex
	calls    []mockCall
	response map[string]any // default success response
}

type mockCall struct {
	Method string
	Body   map[string]any
}

func newTelegramAPIMock() *telegramAPIMock {
	return &telegramAPIMock{
		response: map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		},
	}
}

func (m *telegramAPIMock) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Extract the method name from the path: /bot<token>/<method>
	parts := strings.Split(r.URL.Path, "/")
	method := ""
	if len(parts) > 0 {
		method = parts[len(parts)-1]
	}

	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body) //nolint:errcheck

	m.mu.Lock()
	m.calls = append(m.calls, mockCall{Method: method, Body: body})
	m.mu.Unlock()

	json.NewEncoder(w).Encode(m.response) //nolint:errcheck
}

func (m *telegramAPIMock) Methods() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	for i, c := range m.calls {
		out[i] = c.Method
	}
	return out
}

func (m *telegramAPIMock) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = nil
}

// newBotWithMockAPI creates a bot pointing at a mock Telegram API server.
// queries and prefs are nil — commands that need them will send an error message
// back through the mock, which is fine for routing tests.
func newBotWithMockAPI(t *testing.T, mock *telegramAPIMock, authorizedUser int64) (*Bot, *httptest.Server) {
	t.Helper()
	ts := httptest.NewServer(mock)
	t.Cleanup(ts.Close)

	client := NewWithBaseURL("test-token", ts.URL)
	log := zerolog.Nop()
	bot := NewBot(client, authorizedUser, "secret", nil, nil, nil, nil, log)
	return bot, ts
}

// newBotWithDB creates a bot with a real in-memory database and mock Telegram API.
func newBotWithDB(t *testing.T, mock *telegramAPIMock, authorizedUser int64) *Bot {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)

	ts := httptest.NewServer(mock)
	t.Cleanup(ts.Close)

	client := NewWithBaseURL("test-token", ts.URL)
	log := zerolog.Nop()
	return NewBot(client, authorizedUser, "secret", q, prefs, nil, nil, log)
}

// postWebhook sends an Update to the bot's WebhookHandler with the correct secret.
func postWebhook(t *testing.T, handler http.HandlerFunc, update Update) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("marshaling update: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "secret")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

// messageUpdate builds a webhook Update containing a text message from the given user.
func messageUpdate(userID int64, chatID int64, text string) Update {
	return Update{
		UpdateID: 1,
		Message: &Message{
			MessageID: 1,
			From:      &User{ID: userID, FirstName: "Test"},
			Chat:      Chat{ID: chatID, Type: "private"},
			Text:      text,
			Date:      time.Now().Unix(),
		},
	}
}

// callbackUpdate builds a webhook Update containing a callback query.
func callbackUpdate(userID int64, chatID int64, msgID int, data string) Update {
	return Update{
		UpdateID: 2,
		CallbackQuery: &CallbackQuery{
			ID:   "cq-1",
			From: User{ID: userID, FirstName: "Test"},
			Message: &Message{
				MessageID: msgID,
				Chat:      Chat{ID: chatID, Type: "private"},
			},
			Data: data,
		},
	}
}

// waitForGoroutines waits long enough for async webhook goroutines to complete.
func waitForGoroutines() {
	time.Sleep(100 * time.Millisecond)
}

// ---------------------------------------------------------------------------
// TestParseCommand
// ---------------------------------------------------------------------------

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
		wantArg string
	}{
		{"plain text", "hello world", "", "hello world"},
		{"simple command", "/start", "/start", ""},
		{"command with args", "/block Work 09:00-17:00", "/block", "Work 09:00-17:00"},
		{"command uppercased normalised", "/HELP", "/help", ""},
		{"command with bot suffix", "/start@mybot", "/start", ""},
		{"command with bot suffix and args", "/block@mybot Work 09:00-17:00", "/block", "Work 09:00-17:00"},
		{"empty string", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCmd, gotArg := parseCommand(tt.input)
			if gotCmd != tt.wantCmd {
				t.Errorf("cmd: got %q, want %q", gotCmd, tt.wantCmd)
			}
			if gotArg != tt.wantArg {
				t.Errorf("args: got %q, want %q", gotArg, tt.wantArg)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_ValidSecret
// ---------------------------------------------------------------------------

func TestWebhookHandler_ValidSecret(t *testing.T) {
	mock := newTelegramAPIMock()
	bot, _ := newBotWithMockAPI(t, mock, 0)
	handler := WebhookHandler(bot)

	update := messageUpdate(999, 999, "hello")
	w := postWebhook(t, handler, update)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_InvalidSecret
// ---------------------------------------------------------------------------

func TestWebhookHandler_InvalidSecret(t *testing.T) {
	mock := newTelegramAPIMock()
	bot, _ := newBotWithMockAPI(t, mock, 0)
	handler := WebhookHandler(bot)

	body, _ := json.Marshal(messageUpdate(999, 999, "/help"))
	req := httptest.NewRequest(http.MethodPost, "/webhook", bytes.NewReader(body))
	req.Header.Set("X-Telegram-Bot-Api-Secret-Token", "wrong-secret")
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (silent rejection), got %d", w.Code)
	}

	waitForGoroutines()

	// No API calls should have been made — the request was rejected before dispatch.
	if len(mock.Methods()) != 0 {
		t.Errorf("expected no API calls for invalid secret, got %v", mock.Methods())
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_UnauthorizedUser
// ---------------------------------------------------------------------------

func TestWebhookHandler_UnauthorizedUser(t *testing.T) {
	mock := newTelegramAPIMock()
	bot, _ := newBotWithMockAPI(t, mock, 12345) // only user 12345 is authorized
	handler := WebhookHandler(bot)

	// Send from a different user.
	update := messageUpdate(99999, 99999, "/help")
	w := postWebhook(t, handler, update)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (silent rejection), got %d", w.Code)
	}

	waitForGoroutines()

	if len(mock.Methods()) != 0 {
		t.Errorf("expected no API calls for unauthorized user, got %v", mock.Methods())
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_MessageDispatch
// ---------------------------------------------------------------------------

func TestWebhookHandler_MessageDispatch(t *testing.T) {
	const userID = int64(42)
	const chatID = int64(42)

	// Commands that only require sending a message (no DB queries or prefs).
	// /help and /regenerate (with nil pipeline) fall into this category.
	tests := []struct {
		name           string
		text           string
		wantAPIMethod  string
	}{
		{
			name:          "/help sends a message",
			text:          "/help",
			wantAPIMethod: "sendMessage",
		},
		{
			name:          "/regenerate with no pipeline sends a message",
			text:          "/regenerate",
			wantAPIMethod: "sendMessage",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newTelegramAPIMock()
			bot, _ := newBotWithMockAPI(t, mock, userID)
			handler := WebhookHandler(bot)

			update := messageUpdate(userID, chatID, tt.text)
			w := postWebhook(t, handler, update)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			waitForGoroutines()

			methods := mock.Methods()
			found := false
			for _, m := range methods {
				if m == tt.wantAPIMethod {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected %q to be called, got methods: %v", tt.wantAPIMethod, methods)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_CommandsWithDB
// ---------------------------------------------------------------------------

func TestWebhookHandler_CommandsWithDB(t *testing.T) {
	const userID = int64(42)
	const chatID = int64(42)

	// Commands that need a DB but no external services (Nylas, pipeline).
	// They should still respond (even with empty result sets) via sendMessage.
	tests := []struct {
		name string
		text string
	}{
		{"/today produces sendMessage", "/today"},
		{"/tomorrow produces sendMessage", "/tomorrow"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newTelegramAPIMock()
			bot := newBotWithDB(t, mock, userID)
			handler := WebhookHandler(bot)

			update := messageUpdate(userID, chatID, tt.text)
			w := postWebhook(t, handler, update)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			waitForGoroutines()

			methods := mock.Methods()
			found := false
			for _, m := range methods {
				if m == "sendMessage" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected sendMessage to be called for %q, got methods: %v", tt.text, methods)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_CallbackDispatch
// ---------------------------------------------------------------------------

func TestWebhookHandler_CallbackDispatch(t *testing.T) {
	const userID = int64(42)
	const chatID = int64(42)
	const msgID = 10

	tests := []struct {
		name         string
		callbackData string
	}{
		{
			name:         "approve suggestion callback answered",
			callbackData: "approve:s:123",
		},
		{
			name:         "reject suggestion callback answered",
			callbackData: "reject:s:456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := newTelegramAPIMock()
			// Use a bot with a real DB. The suggestion/goal IDs won't exist in the
			// DB, so the callback will log an error and send an error message, but
			// answerCallbackQuery is always called via defer regardless.
			bot := newBotWithDB(t, mock, userID)
			handler := WebhookHandler(bot)

			update := callbackUpdate(userID, chatID, msgID, tt.callbackData)
			w := postWebhook(t, handler, update)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			waitForGoroutines()

			methods := mock.Methods()
			found := false
			for _, m := range methods {
				if m == "answerCallbackQuery" {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("expected answerCallbackQuery to be called for %q, got methods: %v",
					tt.callbackData, methods)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_CallbackInvalidFormat
// ---------------------------------------------------------------------------

func TestWebhookHandler_CallbackInvalidFormat(t *testing.T) {
	const userID = int64(42)
	const chatID = int64(42)

	mock := newTelegramAPIMock()
	// Invalid callback data doesn't reach the DB, but use newBotWithDB for
	// consistency and to avoid any future nil-deref in dispatch paths.
	bot := newBotWithDB(t, mock, userID)
	handler := WebhookHandler(bot)

	// Callback data with wrong number of parts — handler should still answer the query.
	update := callbackUpdate(userID, chatID, 1, "invalid-data")
	w := postWebhook(t, handler, update)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	waitForGoroutines()

	methods := mock.Methods()
	found := false
	for _, m := range methods {
		if m == "answerCallbackQuery" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected answerCallbackQuery even for invalid callback data, got methods: %v", methods)
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_RateLimit
// ---------------------------------------------------------------------------

func TestWebhookHandler_RateLimit(t *testing.T) {
	const userID = int64(42)
	const chatID = int64(42)

	mock := newTelegramAPIMock()
	bot, _ := newBotWithMockAPI(t, mock, userID)

	// Replace the limiter with one that is already exhausted.
	// Burst=0 means no tokens are ever available.
	bot.cmdLimiter = rate.NewLimiter(rate.Every(time.Hour), 0)

	handler := WebhookHandler(bot)

	update := messageUpdate(userID, chatID, "/help")
	w := postWebhook(t, handler, update)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	waitForGoroutines()

	// The rate-limited path still calls sendMessage with a "slow down" message.
	methods := mock.Methods()
	found := false
	for _, m := range methods {
		if m == "sendMessage" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected sendMessage for rate-limit response, got methods: %v", methods)
	}
}

// ---------------------------------------------------------------------------
// TestWebhookHandler_EmptyMessageIgnored
// ---------------------------------------------------------------------------

func TestWebhookHandler_EmptyMessageIgnored(t *testing.T) {
	const userID = int64(42)

	mock := newTelegramAPIMock()
	bot, _ := newBotWithMockAPI(t, mock, userID)
	handler := WebhookHandler(bot)

	// A message with empty text should be silently ignored (no API calls).
	update := messageUpdate(userID, 42, "")
	w := postWebhook(t, handler, update)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	waitForGoroutines()

	if len(mock.Methods()) != 0 {
		t.Errorf("expected no API calls for empty message text, got %v", mock.Methods())
	}
}

// ---------------------------------------------------------------------------
// TestUpdateUserID — exported via unexported function tested within package
// (already covered in webhook_test.go; kept here for completeness with
// callback-only and message-without-from variants)
// ---------------------------------------------------------------------------

func TestUpdateUserID_Extended(t *testing.T) {
	tests := []struct {
		name string
		u    Update
		want int64
	}{
		{
			name: "message without From returns 0",
			u:    Update{Message: &Message{Chat: Chat{ID: 1}}},
			want: 0,
		},
		{
			name: "callback with zero ID returns 0",
			u:    Update{CallbackQuery: &CallbackQuery{From: User{ID: 0}}},
			want: 0,
		},
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
