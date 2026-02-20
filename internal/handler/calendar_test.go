package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
)

func createTestAccount(t *testing.T, q *sqlcdb.Queries) sqlcdb.CalendarAccount {
	t.Helper()
	account, err := q.CreateAccount(context.Background(), sqlcdb.CreateAccountParams{
		NylasGrantID: "grant-cal-test",
		Provider:     "google",
		Email:        "cal@example.com",
	})
	if err != nil {
		t.Fatalf("creating test account: %v", err)
	}
	return account
}

func createTestCalendar(t *testing.T, q *sqlcdb.Queries, accountID int64) sqlcdb.Calendar {
	t.Helper()
	cal, err := q.CreateCalendar(context.Background(), sqlcdb.CreateCalendarParams{
		AccountID:       accountID,
		NylasCalendarID: "nylas-cal-1",
		Name:            "Work",
		Color:           sql.NullString{String: "#0000ff", Valid: true},
	})
	if err != nil {
		t.Fatalf("creating test calendar: %v", err)
	}
	return cal
}

func TestListCalendars(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	account := createTestAccount(t, q)
	createTestCalendar(t, q, account.ID)

	h := ListCalendars(q, log)

	req := httptest.NewRequest(http.MethodGet, "/api/calendars", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var calendars []json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&calendars); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(calendars) != 1 {
		t.Errorf("got %d calendars, want 1", len(calendars))
	}
}

func TestUpdateCalendar_Success(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	account := createTestAccount(t, q)
	cal := createTestCalendar(t, q, account.ID)

	h := UpdateCalendar(q, log)

	r := chi.NewRouter()
	r.Patch("/api/calendars/{id}", h)

	body := `{"is_blocking": false, "is_active": true}`
	req := httptest.NewRequest(http.MethodPatch, "/api/calendars/"+strconv.FormatInt(cal.ID, 10), strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated sqlcdb.Calendar
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if updated.IsBlocking {
		t.Error("IsBlocking should be false after update")
	}
}

func TestUpdateCalendar_NotFound(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := UpdateCalendar(q, log)

	r := chi.NewRouter()
	r.Patch("/api/calendars/{id}", h)

	body := `{"is_blocking": false}`
	req := httptest.NewRequest(http.MethodPatch, "/api/calendars/99999", strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateCalendar_NoChanges(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	account := createTestAccount(t, q)
	cal := createTestCalendar(t, q, account.ID)

	h := UpdateCalendar(q, log)

	r := chi.NewRouter()
	r.Patch("/api/calendars/{id}", h)

	// Send empty JSON body
	req := httptest.NewRequest(http.MethodPatch, "/api/calendars/"+strconv.FormatInt(cal.ID, 10), strings.NewReader(`{}`))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var updated sqlcdb.Calendar
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	// Default values should be preserved
	if updated.IsBlocking != cal.IsBlocking {
		t.Errorf("IsBlocking changed: got %v, want %v", updated.IsBlocking, cal.IsBlocking)
	}
	if updated.IsActive != cal.IsActive {
		t.Errorf("IsActive changed: got %v, want %v", updated.IsActive, cal.IsActive)
	}
}

func TestUpdateCalendar_InvalidBody(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := UpdateCalendar(q, log)

	r := chi.NewRouter()
	r.Patch("/api/calendars/{id}", h)

	req := httptest.NewRequest(http.MethodPatch, "/api/calendars/1", strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
