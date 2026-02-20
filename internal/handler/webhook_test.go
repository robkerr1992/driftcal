package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/nylas"
)

type mockNylasEventService struct {
	event    *nylas.Event
	eventErr error
}

func (m *mockNylasEventService) GetEvent(ctx context.Context, grantID, eventID, calendarID string) (*nylas.Event, error) {
	return m.event, m.eventErr
}

const webhookSecret = "test-webhook-secret"

func signPayload(body []byte) string {
	mac := hmac.New(sha256.New, []byte(webhookSecret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func setupWebhookTestDB(t *testing.T) (*sqlcdb.Queries, sqlcdb.Calendar) {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	account, err := q.CreateAccount(context.Background(), sqlcdb.CreateAccountParams{
		NylasGrantID: "grant-wh",
		Provider:     "google",
		Email:        "wh@example.com",
	})
	if err != nil {
		t.Fatalf("creating test account: %v", err)
	}

	cal, err := q.CreateCalendar(context.Background(), sqlcdb.CreateCalendarParams{
		AccountID:       account.ID,
		NylasCalendarID: "nylas-cal-wh",
		Name:            "Webhook Test Cal",
	})
	if err != nil {
		t.Fatalf("creating test calendar: %v", err)
	}

	return q, cal
}

func TestWebhook_ChallengeResponse(t *testing.T) {
	q, _ := setupWebhookTestDB(t)
	mock := &mockNylasEventService{}
	log := zerolog.Nop()

	h := NylasWebhook(webhookSecret, mock, q, log)

	req := httptest.NewRequest(http.MethodGet, "/api/webhooks/nylas?challenge=abc123", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Body.String(); got != "abc123" {
		t.Errorf("body = %q, want %q", got, "abc123")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/plain" {
		t.Errorf("Content-Type = %q, want %q", ct, "text/plain")
	}
}

func TestWebhook_InvalidSignature(t *testing.T) {
	q, _ := setupWebhookTestDB(t)
	mock := &mockNylasEventService{}
	log := zerolog.Nop()

	h := NylasWebhook(webhookSecret, mock, q, log)

	body := `{"type":"event.created","data":{"object":{}}}`
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/nylas", strings.NewReader(body))
	req.Header.Set("X-Nylas-Signature", "invalid-signature")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestWebhook_EventCreated(t *testing.T) {
	q, cal := setupWebhookTestDB(t)
	mock := &mockNylasEventService{}
	log := zerolog.Nop()

	h := NylasWebhook(webhookSecret, mock, q, log)

	eventObj := nylas.Event{
		ID:         "evt-wh-1",
		CalendarID: cal.NylasCalendarID,
		Title:      "Webhook Meeting",
		Status:     "confirmed",
		Busy:       true,
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}
	eventJSON, _ := json.Marshal(eventObj)

	payload := map[string]any{
		"type": "event.created",
		"data": map[string]any{
			"object": json.RawMessage(eventJSON),
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/nylas", strings.NewReader(string(body)))
	req.Header.Set("X-Nylas-Signature", signPayload(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	// Verify event was inserted
	evt, err := q.GetEventByNylasID(context.Background(), "evt-wh-1")
	if err != nil {
		t.Fatalf("event not found after webhook: %v", err)
	}
	if evt.Title.String != "Webhook Meeting" {
		t.Errorf("title = %q, want %q", evt.Title.String, "Webhook Meeting")
	}
}

func TestWebhook_EventUpdated(t *testing.T) {
	q, cal := setupWebhookTestDB(t)
	mock := &mockNylasEventService{}
	log := zerolog.Nop()

	// Insert initial event
	q.UpsertEvent(context.Background(), sqlcdb.UpsertEventParams{
		CalendarID:   cal.ID,
		NylasEventID: "evt-wh-upd",
		Title:        sql.NullString{String: "Original Title", Valid: true},
		Status:       "confirmed",
		Busy:         "busy",
		Category:     sql.NullString{String: "other", Valid: true},
		StartTime:    mustParseTime("2024-01-01T00:00:00Z"),
		EndTime:      mustParseTime("2024-01-01T01:00:00Z"),
	})

	h := NylasWebhook(webhookSecret, mock, q, log)

	eventObj := nylas.Event{
		ID:         "evt-wh-upd",
		CalendarID: cal.NylasCalendarID,
		Title:      "Updated Title",
		Status:     "confirmed",
		Busy:       true,
		When: nylas.EventWhen{
			Object:    "timespan",
			StartTime: 1704067200,
			EndTime:   1704070800,
		},
	}
	eventJSON, _ := json.Marshal(eventObj)

	payload := map[string]any{
		"type": "event.updated",
		"data": map[string]any{
			"object": json.RawMessage(eventJSON),
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/nylas", strings.NewReader(string(body)))
	req.Header.Set("X-Nylas-Signature", signPayload(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	evt, err := q.GetEventByNylasID(context.Background(), "evt-wh-upd")
	if err != nil {
		t.Fatalf("event not found: %v", err)
	}
	if evt.Title.String != "Updated Title" {
		t.Errorf("title = %q, want %q", evt.Title.String, "Updated Title")
	}
}

func TestWebhook_EventDeleted(t *testing.T) {
	q, cal := setupWebhookTestDB(t)
	mock := &mockNylasEventService{}
	log := zerolog.Nop()

	// Insert an event to delete
	q.UpsertEvent(context.Background(), sqlcdb.UpsertEventParams{
		CalendarID:   cal.ID,
		NylasEventID: "evt-wh-del",
		Title:        sql.NullString{String: "To Delete", Valid: true},
		Status:       "confirmed",
		Busy:         "busy",
		Category:     sql.NullString{String: "other", Valid: true},
		StartTime:    mustParseTime("2024-01-01T00:00:00Z"),
		EndTime:      mustParseTime("2024-01-01T01:00:00Z"),
	})

	h := NylasWebhook(webhookSecret, mock, q, log)

	payload := map[string]any{
		"type": "event.deleted",
		"data": map[string]any{
			"object": json.RawMessage(`{"id":"evt-wh-del"}`),
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/nylas", strings.NewReader(string(body)))
	req.Header.Set("X-Nylas-Signature", signPayload(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	evt, err := q.GetEventByNylasID(context.Background(), "evt-wh-del")
	if err != nil {
		t.Fatalf("event not found: %v", err)
	}
	if evt.Status != "cancelled" {
		t.Errorf("status = %q, want %q", evt.Status, "cancelled")
	}
}

func TestWebhook_OversizedBody(t *testing.T) {
	q, _ := setupWebhookTestDB(t)
	mock := &mockNylasEventService{}
	log := zerolog.Nop()

	h := NylasWebhook(webhookSecret, mock, q, log)

	// Create a body larger than 1MB
	bigBody := strings.Repeat("x", 2<<20)
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/nylas", strings.NewReader(bigBody))
	req.Header.Set("X-Nylas-Signature", "doesnt-matter")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWebhook_MalformedJSON(t *testing.T) {
	q, _ := setupWebhookTestDB(t)
	mock := &mockNylasEventService{}
	log := zerolog.Nop()

	h := NylasWebhook(webhookSecret, mock, q, log)

	body := []byte("not valid json")
	req := httptest.NewRequest(http.MethodPost, "/api/webhooks/nylas", strings.NewReader(string(body)))
	req.Header.Set("X-Nylas-Signature", signPayload(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Should return 200 to avoid Nylas retries
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d (malformed JSON should still return 200)", rec.Code, http.StatusOK)
	}
}

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}
