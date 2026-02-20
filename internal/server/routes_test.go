package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/config"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/nylas"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	db := database.TestDB(t)
	log := zerolog.Nop()

	cfg := &config.Config{
		APIKey:             "test-api-key",
		NylasWebhookSecret: "test-webhook-secret",
		BaseURL:            "http://localhost:8080",
		NylasClientID:      "test-client",
		NylasAPIKey:        "test-nylas-key",
	}

	q := sqlcdb.New(db)
	return &Server{
		cfg:     cfg,
		db:      db,
		queries: q,
		log:     log,
		nylas:   nylas.New(cfg.NylasClientID, cfg.NylasAPIKey),
	}
}

func TestRoutes_AuthRequired(t *testing.T) {
	s := testServer(t)
	router := s.routes()

	// Unauthenticated request to protected endpoint → 401
	req := httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no auth: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	// Authenticated request → 200
	req = httptest.NewRequest(http.MethodGet, "/api/accounts", nil)
	req.Header.Set("Authorization", "Bearer test-api-key")
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("with auth: status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestRoutes_HealthNoAuth(t *testing.T) {
	s := testServer(t)
	router := s.routes()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("health: status = %d, want %d", rec.Code, http.StatusOK)
	}
}
