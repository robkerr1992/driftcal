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
	"github.com/robkerr1992/driftcal/internal/preferences"
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
		prefs:   preferences.New(q, db),
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

func TestRoutes_NewEndpoints(t *testing.T) {
	s := testServer(t)
	router := s.routes()

	unauthTests := []struct {
		name   string
		method string
		path   string
	}{
		{"GET /api/gaps", http.MethodGet, "/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-21T00:00:00Z"},
		{"GET /api/protected-blocks", http.MethodGet, "/api/protected-blocks"},
		{"POST /api/protected-blocks", http.MethodPost, "/api/protected-blocks"},
		{"DELETE /api/protected-blocks/1", http.MethodDelete, "/api/protected-blocks/1"},
		{"GET /api/preferences", http.MethodGet, "/api/preferences"},
		{"PATCH /api/preferences", http.MethodPatch, "/api/preferences"},
	}

	for _, tt := range unauthTests {
		t.Run(tt.name+"_no_auth", func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
			}
		})
	}

	// Authenticated requests that should succeed (GET endpoints).
	authGetTests := []struct {
		name string
		path string
	}{
		{"GET /api/protected-blocks", "/api/protected-blocks"},
		{"GET /api/preferences", "/api/preferences"},
	}

	for _, tt := range authGetTests {
		t.Run(tt.name+"_with_auth", func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			req.Header.Set("Authorization", "Bearer test-api-key")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
			}
		})
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
