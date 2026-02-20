package nylas

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestAuthURL(t *testing.T) {
	c := New("client-123", "api-key")
	got := c.AuthURL("https://example.com/callback", "google")

	if !strings.Contains(got, "client_id=client-123") {
		t.Errorf("AuthURL missing client_id, got %s", got)
	}
	if !strings.Contains(got, "provider=google") {
		t.Errorf("AuthURL missing provider, got %s", got)
	}
	if !strings.Contains(got, "redirect_uri=") {
		t.Errorf("AuthURL missing redirect_uri, got %s", got)
	}
	if !strings.HasPrefix(got, defaultBaseURL) {
		t.Errorf("AuthURL should start with base URL, got %s", got)
	}
}

func TestExchangeCode_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/v3/connect/token" {
			t.Errorf("path = %s, want /v3/connect/token", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer api-key" {
			t.Errorf("Authorization = %q, want %q", auth, "Bearer api-key")
		}

		json.NewEncoder(w).Encode(TokenResponse{
			GrantID:  "grant-abc",
			Email:    "user@example.com",
			Provider: "google",
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	resp, err := c.ExchangeCode(context.Background(), "auth-code", "https://example.com/callback")
	if err != nil {
		t.Fatalf("ExchangeCode failed: %v", err)
	}

	if resp.GrantID != "grant-abc" {
		t.Errorf("GrantID = %q, want %q", resp.GrantID, "grant-abc")
	}
	if resp.Email != "user@example.com" {
		t.Errorf("Email = %q, want %q", resp.Email, "user@example.com")
	}
}

func TestExchangeCode_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	_, err := c.ExchangeCode(context.Background(), "bad-code", "https://example.com/callback")
	if err == nil {
		t.Fatal("expected error for bad auth code")
	}
	if !strings.Contains(err.Error(), "400") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestListCalendars(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/grants/grant-abc/calendars" {
			t.Errorf("path = %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(listResponse[Calendar]{
			Data: []Calendar{
				{ID: "cal-1", Name: "Work", HexColor: "#0000ff", IsPrimary: true},
				{ID: "cal-2", Name: "Personal", HexColor: "#ff0000"},
			},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	cals, err := c.ListCalendars(context.Background(), "grant-abc")
	if err != nil {
		t.Fatalf("ListCalendars failed: %v", err)
	}
	if len(cals) != 2 {
		t.Fatalf("got %d calendars, want 2", len(cals))
	}
	if cals[0].Name != "Work" {
		t.Errorf("first calendar name = %q, want %q", cals[0].Name, "Work")
	}
}

func TestListEvents_WithPagination(t *testing.T) {
	page := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/grants/grant-abc/events" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if page == 0 {
			json.NewEncoder(w).Encode(listResponse[Event]{
				Data:       []Event{{ID: "evt-1", Title: "Meeting"}},
				NextCursor: "cursor-2",
			})
			page++
		} else {
			json.NewEncoder(w).Encode(listResponse[Event]{
				Data: []Event{{ID: "evt-2", Title: "Lunch"}},
			})
		}
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2025, 1, 31, 23, 59, 59, 0, time.UTC)

	events, err := c.ListEvents(context.Background(), "grant-abc", "cal-1", start, end)
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("got %d events, want 2", len(events))
	}
	if events[1].Title != "Lunch" {
		t.Errorf("second event title = %q, want %q", events[1].Title, "Lunch")
	}
}

func TestListEvents_Empty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(listResponse[Event]{Data: []Event{}})
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	events, err := c.ListEvents(context.Background(), "grant-abc", "cal-1", time.Now(), time.Now())
	if err != nil {
		t.Fatalf("ListEvents failed: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("got %d events, want 0", len(events))
	}
}

func TestGetEvent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v3/grants/grant-abc/events/evt-1" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.URL.Query().Get("calendar_id") != "cal-1" {
			t.Errorf("calendar_id param = %q", r.URL.Query().Get("calendar_id"))
		}
		json.NewEncoder(w).Encode(struct {
			Data Event `json:"data"`
		}{
			Data: Event{ID: "evt-1", Title: "Team Standup"},
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	evt, err := c.GetEvent(context.Background(), "grant-abc", "evt-1", "cal-1")
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}
	if evt.Title != "Team Standup" {
		t.Errorf("Title = %q, want %q", evt.Title, "Team Standup")
	}
}

func TestListCalendars_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server_error"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	_, err := c.ListCalendars(context.Background(), "grant-abc")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestListEvents_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"server_error"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	_, err := c.ListEvents(context.Background(), "grant-abc", "cal-1", time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestGetEvent_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not_found"}`))
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	_, err := c.GetEvent(context.Background(), "grant-abc", "evt-missing", "cal-1")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention status code, got: %v", err)
	}
}

func TestListEvents_PaginationLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Always return a next cursor to trigger pagination limit
		json.NewEncoder(w).Encode(listResponse[Event]{
			Data:       []Event{{ID: "evt-page", Title: "Page Event"}},
			NextCursor: "always-more",
		})
	}))
	defer srv.Close()

	c := NewWithBaseURL("client-123", "api-key", srv.URL)
	events, err := c.ListEvents(context.Background(), "grant-abc", "cal-1", time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected error when pagination limit reached")
	}
	if !strings.Contains(err.Error(), "pagination limit") {
		t.Errorf("error should mention pagination limit, got: %v", err)
	}
	// Should still return the events collected so far
	if len(events) != 50 {
		t.Errorf("got %d events, want 50 (one per page before limit)", len(events))
	}
}
