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
)

// mockNylasAccountService is a test double for NylasAccountService.
type mockNylasAccountService struct {
	authURL      string
	tokenResp    *nylas.TokenResponse
	tokenErr     error
	calendars    []nylas.Calendar
	calendarsErr error
}

func (m *mockNylasAccountService) AuthURL(redirectURI, provider string) string {
	return m.authURL
}

func (m *mockNylasAccountService) ExchangeCode(ctx context.Context, code, redirectURI string) (*nylas.TokenResponse, error) {
	return m.tokenResp, m.tokenErr
}

func (m *mockNylasAccountService) ListCalendars(ctx context.Context, grantID string) ([]nylas.Calendar, error) {
	return m.calendars, m.calendarsErr
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
	json.NewDecoder(rec.Body).Decode(&resp)
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
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, log)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=auth-code-xyz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]json.RawMessage
	json.NewDecoder(rec.Body).Decode(&resp)
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

	h := AccountCallback(mock, "https://example.com", q, log)

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

	h := AccountCallback(mock, "https://example.com", q, log)

	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=bad", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadGateway)
	}
}

func TestAccountCallback_DuplicateGrant(t *testing.T) {
	mock := &mockNylasAccountService{
		tokenResp: &nylas.TokenResponse{
			GrantID:  "grant-dup",
			Email:    "user@example.com",
			Provider: "google",
		},
		calendars: []nylas.Calendar{},
	}
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := AccountCallback(mock, "https://example.com", q, log)

	// First call: creates account
	req := httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("first call: status = %d, want %d", rec.Code, http.StatusCreated)
	}

	// Second call: duplicate grant_id → 409
	req = httptest.NewRequest(http.MethodGet, "/api/accounts/callback?code=code2", nil)
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
	json.NewDecoder(rec.Body).Decode(&accounts)
	if len(accounts) != 0 {
		t.Errorf("got %d accounts, want 0", len(accounts))
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
