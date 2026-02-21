package handler

import (
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

func TestCreateProtectedBlock(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := CreateProtectedBlock(q, log)

	body := `{"label":"Family dinner","day_of_week":0,"start_time":"18:00","end_time":"20:00"}`
	req := httptest.NewRequest(http.MethodPost, "/api/protected-blocks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var block sqlcdb.ProtectedBlock
	if err := json.NewDecoder(rec.Body).Decode(&block); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if block.Label != "Family dinner" {
		t.Errorf("Label = %q, want Family dinner", block.Label)
	}
	if !block.DayOfWeek.Valid || block.DayOfWeek.Int64 != 0 {
		t.Errorf("DayOfWeek = %v, want 0", block.DayOfWeek)
	}
}

func TestCreateProtectedBlock_InvalidTime(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := CreateProtectedBlock(q, log)

	body := `{"label":"Test","start_time":"25:00","end_time":"20:00"}`
	req := httptest.NewRequest(http.MethodPost, "/api/protected-blocks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateProtectedBlock_OvernightRejected(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	h := CreateProtectedBlock(q, log)

	body := `{"label":"Sleep","start_time":"22:00","end_time":"07:00"}`
	req := httptest.NewRequest(http.MethodPost, "/api/protected-blocks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestListProtectedBlocks(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	// Create two blocks.
	createH := CreateProtectedBlock(q, log)
	for _, body := range []string{
		`{"label":"Morning","start_time":"07:00","end_time":"08:00"}`,
		`{"label":"Evening","start_time":"18:00","end_time":"20:00"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/protected-blocks", strings.NewReader(body))
		rec := httptest.NewRecorder()
		createH.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("setup: status = %d", rec.Code)
		}
	}

	listH := ListProtectedBlocks(q, log)
	req := httptest.NewRequest(http.MethodGet, "/api/protected-blocks", nil)
	rec := httptest.NewRecorder()
	listH.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Blocks []sqlcdb.ProtectedBlock `json:"blocks"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if len(resp.Blocks) != 2 {
		t.Errorf("got %d blocks, want 2", len(resp.Blocks))
	}
}

func TestDeleteProtectedBlock(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	// Create a block first.
	createH := CreateProtectedBlock(q, log)
	body := `{"label":"To Delete","start_time":"10:00","end_time":"11:00"}`
	req := httptest.NewRequest(http.MethodPost, "/api/protected-blocks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	createH.ServeHTTP(rec, req)

	var created sqlcdb.ProtectedBlock
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decoding created block: %v", err)
	}

	// Delete it.
	deleteH := DeleteProtectedBlock(q, log)
	r := chi.NewRouter()
	r.Delete("/api/protected-blocks/{id}", deleteH)

	req = httptest.NewRequest(http.MethodDelete, "/api/protected-blocks/"+strconv.FormatInt(created.ID, 10), nil)
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}
}

func TestDeleteProtectedBlock_NotFound(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	log := zerolog.Nop()

	deleteH := DeleteProtectedBlock(q, log)
	r := chi.NewRouter()
	r.Delete("/api/protected-blocks/{id}", deleteH)

	req := httptest.NewRequest(http.MethodDelete, "/api/protected-blocks/99999", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestCreateProtectedBlock_MissingLabel(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	h := CreateProtectedBlock(q, zerolog.Nop())

	body := `{"start_time":"09:00","end_time":"10:00"}`
	req := httptest.NewRequest(http.MethodPost, "/api/protected-blocks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestCreateProtectedBlock_InvalidDayOfWeek(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	h := CreateProtectedBlock(q, zerolog.Nop())

	body := `{"label":"Test","day_of_week":7,"start_time":"09:00","end_time":"10:00"}`
	req := httptest.NewRequest(http.MethodPost, "/api/protected-blocks", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestDeleteProtectedBlock_InvalidID(t *testing.T) {
	db := database.TestDB(t)
	q := sqlcdb.New(db)

	deleteH := DeleteProtectedBlock(q, zerolog.Nop())
	r := chi.NewRouter()
	r.Delete("/api/protected-blocks/{id}", deleteH)

	req := httptest.NewRequest(http.MethodDelete, "/api/protected-blocks/not-a-number", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
