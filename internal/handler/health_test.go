package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/internal/database"
)

func TestHealth_Healthy(t *testing.T) {
	db := database.TestDB(t)
	log := zerolog.Nop()
	h := Health(db, time.Now(), log)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.Status != "healthy" {
		t.Errorf("status = %q, want %q", resp.Status, "healthy")
	}
	if resp.Database != "ok" {
		t.Errorf("database = %q, want %q", resp.Database, "ok")
	}
}

func TestHealth_Degraded(t *testing.T) {
	db := database.TestDB(t)
	log := zerolog.Nop()
	// Close the DB to simulate unreachable database
	db.Close()

	h := Health(db, time.Now(), log)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.Status != "degraded" {
		t.Errorf("status = %q, want %q", resp.Status, "degraded")
	}
	if resp.Database != "unreachable" {
		t.Errorf("database = %q, want %q", resp.Database, "unreachable")
	}
}

func TestHealth_UptimePositive(t *testing.T) {
	db := database.TestDB(t)
	log := zerolog.Nop()
	startTime := time.Now().Add(-1 * time.Second)
	h := Health(db, startTime, log)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}

	if resp.UptimeSeconds <= 0 {
		t.Errorf("uptime_seconds = %f, want > 0", resp.UptimeSeconds)
	}
}
