package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/pipeline"
	"github.com/robkerr1992/driftcal/internal/preferences"
	"github.com/robkerr1992/driftcal/internal/suggest"
)

func createTestSuggestion(t *testing.T, q *sqlcdb.Queries, date time.Time) sqlcdb.ActivitySuggestion {
	t.Helper()
	s, err := q.CreateActivitySuggestion(t.Context(), sqlcdb.CreateActivitySuggestionParams{
		SuggestedDate: date,
		StartTime:     date.Add(10 * time.Hour),
		EndTime:       date.Add(12 * time.Hour),
		Title:         "Walk in the park",
		Description:   "A nice stroll through the local park",
		Category:      "outdoor",
		EnergyLevel:   "medium",
		EstimatedCost: "free",
		Location:      sql.NullString{String: "Central Park", Valid: true},
		Reasoning:     sql.NullString{String: "Good weather", Valid: true},
	})
	if err != nil {
		t.Fatalf("createTestSuggestion: %v", err)
	}
	return s
}

func TestListSuggestions_Empty(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := ListSuggestions(q, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions?date=2026-03-01", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Suggestions []sqlcdb.ActivitySuggestion `json:"suggestions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Suggestions) != 0 {
		t.Errorf("got %d suggestions, want 0", len(resp.Suggestions))
	}
}

func TestListSuggestions_WithData(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	date := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	createTestSuggestion(t, q, date)

	h := ListSuggestions(q, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions?date=2026-03-01", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Suggestions []sqlcdb.ActivitySuggestion `json:"suggestions"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Suggestions) != 1 {
		t.Errorf("got %d suggestions, want 1", len(resp.Suggestions))
	}
}

func TestListSuggestions_DefaultDate(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := ListSuggestions(q, zerolog.Nop())
	// No ?date param — should default to tomorrow and return 200.
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestListSuggestions_InvalidDate(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	h := ListSuggestions(q, zerolog.Nop())
	req := httptest.NewRequest(http.MethodGet, "/api/suggestions?date=bad", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestApproveSuggestion_Success(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	date := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s := createTestSuggestion(t, q, date)

	r := chi.NewRouter()
	r.Post("/api/suggestions/{id}/approve", ApproveSuggestion(q, nil, zerolog.Nop()))

	url := "/api/suggestions/" + strconv.FormatInt(s.ID, 10) + "/approve"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Suggestion sqlcdb.ActivitySuggestion `json:"suggestion"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Suggestion.Status != "approved" {
		t.Errorf("Status = %q, want approved", resp.Suggestion.Status)
	}

	// Verify feedback was created.
	feedback, err := q.ListFeedbackForSuggestion(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("listing feedback: %v", err)
	}
	if len(feedback) != 1 {
		t.Fatalf("got %d feedback records, want 1", len(feedback))
	}
	if feedback[0].Action != "approved" {
		t.Errorf("feedback action = %q, want approved", feedback[0].Action)
	}
}

func TestRejectSuggestion_WithNotes(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	date := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s := createTestSuggestion(t, q, date)

	r := chi.NewRouter()
	r.Post("/api/suggestions/{id}/reject", RejectSuggestion(q, zerolog.Nop()))

	body := `{"notes":"Not in the mood for outdoor activities"}`
	url := "/api/suggestions/" + strconv.FormatInt(s.ID, 10) + "/reject"
	req := httptest.NewRequest(http.MethodPost, url, strings.NewReader(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Suggestion sqlcdb.ActivitySuggestion `json:"suggestion"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp.Suggestion.Status != "rejected" {
		t.Errorf("Status = %q, want rejected", resp.Suggestion.Status)
	}

	// Verify feedback with notes.
	feedback, err := q.ListFeedbackForSuggestion(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("listing feedback: %v", err)
	}
	if len(feedback) != 1 {
		t.Fatalf("got %d feedback records, want 1", len(feedback))
	}
	if !feedback[0].EditNotes.Valid || feedback[0].EditNotes.String != "Not in the mood for outdoor activities" {
		t.Errorf("feedback notes = %v, want notes", feedback[0].EditNotes)
	}
}

func TestRejectSuggestion_NoNotes(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	date := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s := createTestSuggestion(t, q, date)

	r := chi.NewRouter()
	r.Post("/api/suggestions/{id}/reject", RejectSuggestion(q, zerolog.Nop()))

	// Empty body — no notes.
	url := "/api/suggestions/" + strconv.FormatInt(s.ID, 10) + "/reject"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify feedback has no notes.
	feedback, err := q.ListFeedbackForSuggestion(t.Context(), s.ID)
	if err != nil {
		t.Fatalf("listing feedback: %v", err)
	}
	if len(feedback) != 1 {
		t.Fatalf("got %d feedback records, want 1", len(feedback))
	}
	if feedback[0].EditNotes.Valid {
		t.Errorf("feedback notes should be NULL, got %q", feedback[0].EditNotes.String)
	}
}

func TestRejectSuggestion_NotFound(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	r := chi.NewRouter()
	r.Post("/api/suggestions/{id}/reject", RejectSuggestion(q, zerolog.Nop()))

	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/99999/reject", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestApproveSuggestion_AlreadyRejected(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	date := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s := createTestSuggestion(t, q, date)

	// Reject it first.
	if _, err := q.UpdateSuggestionStatus(t.Context(), sqlcdb.UpdateSuggestionStatusParams{
		Status: "rejected", ID: s.ID,
	}); err != nil {
		t.Fatalf("setup: rejecting suggestion: %v", err)
	}

	r := chi.NewRouter()
	r.Post("/api/suggestions/{id}/approve", ApproveSuggestion(q, nil, zerolog.Nop()))

	url := "/api/suggestions/" + strconv.FormatInt(s.ID, 10) + "/approve"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestApproveSuggestion_NotFound(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	r := chi.NewRouter()
	r.Post("/api/suggestions/{id}/approve", ApproveSuggestion(q, nil, zerolog.Nop()))

	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/99999/approve", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestRejectSuggestion_AlreadyApproved(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	date := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	s := createTestSuggestion(t, q, date)

	if _, err := q.UpdateSuggestionStatus(t.Context(), sqlcdb.UpdateSuggestionStatusParams{
		Status: "approved", ID: s.ID,
	}); err != nil {
		t.Fatalf("setup: approving suggestion: %v", err)
	}

	r := chi.NewRouter()
	r.Post("/api/suggestions/{id}/reject", RejectSuggestion(q, zerolog.Nop()))

	url := "/api/suggestions/" + strconv.FormatInt(s.ID, 10) + "/reject"
	req := httptest.NewRequest(http.MethodPost, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestTriggerPipeline_NotConfigured(t *testing.T) {
	h := TriggerPipeline(nil, zerolog.Nop())
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/generate", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
}

// --- Local mocks for pipeline interfaces ---

type failingSyncer struct {
	err error
}

func (f *failingSyncer) SyncAll(ctx context.Context) error {
	return f.err
}

type failingSuggest struct {
	err error
}

func (f *failingSuggest) Generate(ctx context.Context, system, user string) (*suggest.GenerateResult, error) {
	return nil, f.err
}

func TestTriggerPipeline_ErrorDoesNotLeakDetails(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	prefs.Set(t.Context(), "timezone", "UTC")
	prefs.Set(t.Context(), "active_hours_start", "07:00")
	prefs.Set(t.Context(), "active_hours_end", "22:00")

	runner := pipeline.New(q,
		&failingSyncer{err: errors.New("secret internal detail: API key invalid")},
		prefs, nil, &failingSuggest{}, nil, zerolog.Nop())

	h := TriggerPipeline(runner, zerolog.Nop())
	req := httptest.NewRequest(http.MethodPost, "/api/suggestions/generate", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "secret internal detail") {
		t.Error("response body leaks internal error details")
	}
	if !strings.Contains(body, "pipeline execution failed") {
		t.Error("response should contain generic error message")
	}
}
