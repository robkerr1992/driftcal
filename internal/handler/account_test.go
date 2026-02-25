package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/nylas"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

// mockNylasAccountService is a test double for NylasAccountService.
type mockNylasAccountService struct {
	authURL      string
	tokenResp    *nylas.TokenResponse
	tokenErr     error
	calendars    []nylas.Calendar
	calendarsErr error

	createdCalendar  *nylas.Calendar
	createCalErr     error
	createCalCalled  bool
}

func (m *mockNylasAccountService) AuthURL(redirectURI, provider, state string) string {
	return m.authURL
}

func (m *mockNylasAccountService) ExchangeCode(ctx context.Context, code, redirectURI string) (*nylas.TokenResponse, error) {
	return m.tokenResp, m.tokenErr
}

func (m *mockNylasAccountService) ListCalendars(ctx context.Context, grantID string) ([]nylas.Calendar, error) {
	return m.calendars, m.calendarsErr
}

func (m *mockNylasAccountService) CreateCalendar(ctx context.Context, grantID string, req nylas.CreateCalendarRequest) (*nylas.Calendar, error) {
	m.createCalCalled = true
	return m.createdCalendar, m.createCalErr
}

func TestConnectAccount_Success(t *testing.T) {
	mock := &mockNylasAccountService{authURL: "https://nylas.com/auth?foo=bar"}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := ConnectAccount(mock, "https://example.com", q, log)

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/connect",
		strings.NewReader(`{"provider":"google"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp["auth_url"] != "https://nylas.com/auth?foo=bar" {
		t.Errorf("auth_url = %q, want %q", resp["auth_url"], "https://nylas.com/auth?foo=bar")
	}
}

func TestConnectAccount_InvalidProvider(t *testing.T) {
	mock := &mockNylasAccountService{}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := ConnectAccount(mock, "https://example.com", q, log)

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/connect",
		strings.NewReader(`{"provider":"yahoo"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestConnectAccount_InvalidBody(t *testing.T) {
	mock := &mockNylasAccountService{}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := ConnectAccount(mock, "https://example.com", q, log)

	req := httptest.NewRequest(http.MethodPost, "/api/accounts/connect",
		strings.NewReader("not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAccountCallback_Success(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-123",
			Email:    "user@example.com",
			Provider: "google",
		},
		calendars: []nylas.Calendar{
			{ID: "cal-1", Name: "Work", HexColor: "#0000ff"},
		},
		createdCalendar: &nylas.Calendar{ID: "cal-driftcal", Name: "DriftCal"},
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, preferences.New(q, db), log)

	state := "test-state-success"
	storeState(t.Context(), q, state)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=auth-code-xyz&state="+state, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp["account"] == nil {
		t.Error("missing account in response")
	}
	if resp["calendars"] == nil {
		t.Error("missing calendars in response")
	}
}

func TestAccountCallback_MissingCode(t *testing.T) {
	mock := &mockNylasAccountService{}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, preferences.New(q, db), log)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAccountCallback_ExchangeError(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenErr: errors.New("nylas error"),
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, preferences.New(q, db), log)

	state := "test-state-exchange-err"
	storeState(t.Context(), q, state)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=bad&state="+state, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestAccountCallback_CalendarFetchFails(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-cal-fail",
			Email:    "calfail@example.com",
			Provider: "google",
		},
		calendarsErr: errors.New("calendar fetch failed"),
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, preferences.New(q, db), log)

	state := "test-state-cal-fail"
	storeState(t.Context(), q, state)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code1&state="+state, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if resp["account"] == nil {
		t.Error("missing account in response")
	}

	var calendars []json.RawMessage
	if err := json.Unmarshal(resp["calendars"], &calendars); err != nil {
		t.Fatalf("decoding calendars: %v", err)
	}
	if len(calendars) != 0 {
		t.Errorf("expected empty calendars array, got %d", len(calendars))
	}
}

func TestAccountCallback_DuplicateGrant(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-dup",
			Email:    "user@example.com",
			Provider: "google",
		},
		calendars:       []nylas.Calendar{},
		createdCalendar: &nylas.Calendar{ID: "cal-driftcal", Name: "DriftCal"},
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, preferences.New(q, db), log)

	// First call: creates account
	state1 := "test-state-dup-1"
	storeState(t.Context(), q, state1)
	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code1&state="+state1, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first call: status = %d, want %d", rec.Code, http.StatusCreated)
	}

	// Second call: duplicate grant_id → 409
	state2 := "test-state-dup-2"
	storeState(t.Context(), q, state2)
	req = httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code2&state="+state2, nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("second call: status = %d, want %d", rec.Code, http.StatusConflict)
	}
}

func TestListAccounts_Empty(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := ListAccounts(q, log)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var accounts []json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&accounts); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("got %d accounts, want 0", len(accounts))
	}
}

func TestListAccounts_WithData(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	// Create some accounts
	for _, email := range []string{"a@test.com", "b@test.com"} {
		if _, err := q.CreateAccount(context.Background(), sqlcdb.CreateAccountParams{
			NylasGrantID: "grant-" + email,
			Provider:     "google",
			Email:        email,
		}); err != nil {
			t.Fatalf("creating account %s: %v", email, err)
		}
	}

	h := ListAccounts(q, log)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var accounts []json.RawMessage
	if err := json.NewDecoder(rec.Body).Decode(&accounts); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(accounts) != 2 {
		t.Errorf("got %d accounts, want 2", len(accounts))
	}
}

func TestDisconnectAccount_Success(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	// Create an account first
	account, err := q.CreateAccount(context.Background(), sqlcdb.CreateAccountParams{
		NylasGrantID: "grant-disc",
		Provider:     "google",
		Email:        "disc@example.com",
	})
	if err != nil {
		t.Fatalf("creating test account: %v", err)
	}

	h := DisconnectAccount(q, log)

	// Use chi router to properly parse URL params
	r := chi.NewRouter()
	r.Delete("/api/accounts/{id}", h)

	req := httptest.NewRequest(http.MethodDelete, "/api/accounts/"+strconv.FormatInt(account.ID, 10), nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestDisconnectAccount_NotFound(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := DisconnectAccount(q, log)

	r := chi.NewRouter()
	r.Delete("/api/accounts/{id}", h)

	req := httptest.NewRequest(http.MethodDelete, "/api/accounts/99999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestAccountCallback_InvalidState(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-state",
			Email:    "state@example.com",
			Provider: "google",
		},
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, preferences.New(q, db), log)

	// No state parameter
	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing state: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}

	// Wrong state parameter
	req = httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code1&state=bogus", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid state: status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestAccountCallback_PartialCalendarFailure(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-partial-cal",
			Email:    "partial@example.com",
			Provider: "google",
		},
		calendars: []nylas.Calendar{
			{ID: "cal-unique", Name: "First Cal", HexColor: "#00ff00"},
			{ID: "cal-unique", Name: "Duplicate Cal", HexColor: "#ff0000"}, // same nylas_calendar_id → unique constraint
		},
		createdCalendar: &nylas.Calendar{ID: "cal-driftcal", Name: "DriftCal"},
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, preferences.New(q, db), log)

	state := "test-state-partial"
	storeState(t.Context(), q, state)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code1&state="+state, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp struct {
		Calendars []json.RawMessage `json:"calendars"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Calendars) != 2 {
		t.Errorf("expected 2 calendars (1 original + DriftCal; second original should fail unique constraint), got %d", len(resp.Calendars))
	}
}

func TestAccountCallback_FirstAccount_CreatesDriftCal(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-first",
			Email:    "first@example.com",
			Provider: "google",
		},
		calendars:       []nylas.Calendar{{ID: "cal-work", Name: "Work"}},
		createdCalendar: &nylas.Calendar{ID: "cal-driftcal", Name: "DriftCal"},
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, prefs, log)

	state := "test-state-first-acct"
	storeState(t.Context(), q, state)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code1&state="+state, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if !mock.createCalCalled {
		t.Fatal("expected CreateCalendar to be called for first account")
	}

	// Verify prefs were set
	grantID, err := prefs.Get(t.Context(), "nylas_grant_id")
	if err != nil {
		t.Fatalf("getting nylas_grant_id pref: %v", err)
	}
	if grantID != "grant-first" {
		t.Errorf("nylas_grant_id = %q, want %q", grantID, "grant-first")
	}

	calID, err := prefs.Get(t.Context(), "nylas_calendar_id")
	if err != nil {
		t.Fatalf("getting nylas_calendar_id pref: %v", err)
	}
	if calID != "cal-driftcal" {
		t.Errorf("nylas_calendar_id = %q, want %q", calID, "cal-driftcal")
	}

	// Verify the DriftCal calendar is in the response
	var resp struct {
		Calendars []struct {
			Name       string `json:"name"`
			IsBlocking bool   `json:"is_blocking"`
		} `json:"calendars"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	found := false
	for _, cal := range resp.Calendars {
		if cal.Name == "DriftCal" {
			found = true
			if cal.IsBlocking {
				t.Error("DriftCal calendar should have is_blocking=false")
			}
		}
	}
	if !found {
		t.Error("DriftCal calendar not found in response")
	}
}

func TestAccountCallback_SecondAccount_NoDriftCal(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-second",
			Email:    "second@example.com",
			Provider: "microsoft",
		},
		calendars:       []nylas.Calendar{{ID: "cal-outlook", Name: "Outlook"}},
		createdCalendar: &nylas.Calendar{ID: "cal-should-not-create", Name: "DriftCal"},
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	log := zerolog.Nop()

	// Pre-set prefs to simulate first account already connected
	if err := prefs.SetMany(t.Context(), map[string]string{
		"nylas_grant_id":    "grant-existing",
		"nylas_calendar_id": "cal-existing",
	}); err != nil {
		t.Fatalf("setting initial prefs: %v", err)
	}

	h := AccountCallback(mock, "https://example.com", q, prefs, log)

	state := "test-state-second-acct"
	storeState(t.Context(), q, state)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code2&state="+state, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	if mock.createCalCalled {
		t.Error("CreateCalendar should NOT be called for second account")
	}

	// Verify prefs are unchanged
	grantID, _ := prefs.Get(t.Context(), "nylas_grant_id")
	if grantID != "grant-existing" {
		t.Errorf("nylas_grant_id should be unchanged, got %q", grantID)
	}
}

func TestAccountCallback_DriftCalCreationFails_AccountStillCreated(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-icloud",
			Email:    "icloud@example.com",
			Provider: "icloud",
		},
		calendars:    []nylas.Calendar{{ID: "cal-icloud", Name: "iCloud Calendar"}},
		createCalErr: errors.New("provider does not support calendar creation"),
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, prefs, log)

	state := "test-state-icloud"
	storeState(t.Context(), q, state)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code3&state="+state, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Account should still be created (201)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	// Prefs should NOT be set since DriftCal creation failed
	grantID, _ := prefs.Get(t.Context(), "nylas_grant_id")
	if grantID != "" {
		t.Errorf("nylas_grant_id should be empty after DriftCal failure, got %q", grantID)
	}
	calID, _ := prefs.Get(t.Context(), "nylas_calendar_id")
	if calID != "" {
		t.Errorf("nylas_calendar_id should be empty after DriftCal failure, got %q", calID)
	}
}
