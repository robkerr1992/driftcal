package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

func prefsSetup(t *testing.T) *preferences.Store {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	return preferences.New(q, db)
}

func TestGetPreferences_Defaults(t *testing.T) {
	prefs := prefsSetup(t)
	log := zerolog.Nop()

	h := GetPreferences(prefs, log)

	req := httptest.NewRequest(http.MethodGet, "/api/preferences", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp struct {
		Preferences map[string]any `json:"preferences"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if resp.Preferences["timezone"] != "UTC" {
		t.Errorf("timezone = %v, want UTC", resp.Preferences["timezone"])
	}
}

func TestGetPreferences_NestedFormat(t *testing.T) {
	prefs := prefsSetup(t)
	log := zerolog.Nop()

	h := GetPreferences(prefs, log)

	req := httptest.NewRequest(http.MethodGet, "/api/preferences", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp struct {
		Preferences map[string]any `json:"preferences"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	// active_hours should be a nested object.
	ah, ok := resp.Preferences["active_hours"].(map[string]any)
	if !ok {
		t.Fatalf("active_hours is not a nested object: %T", resp.Preferences["active_hours"])
	}
	if ah["start"] != "07:00" {
		t.Errorf("active_hours.start = %v, want 07:00", ah["start"])
	}
	if ah["end"] != "22:00" {
		t.Errorf("active_hours.end = %v, want 22:00", ah["end"])
	}
}

func TestUpdatePreferences(t *testing.T) {
	prefs := prefsSetup(t)
	log := zerolog.Nop()

	h := UpdatePreferences(prefs, log)

	body := `{"timezone":"America/New_York"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Preferences map[string]any `json:"preferences"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if resp.Preferences["timezone"] != "America/New_York" {
		t.Errorf("timezone = %v, want America/New_York", resp.Preferences["timezone"])
	}
}

func TestUpdatePreferences_InvalidTimezone(t *testing.T) {
	prefs := prefsSetup(t)
	log := zerolog.Nop()

	h := UpdatePreferences(prefs, log)

	body := `{"timezone":"Fake/Timezone"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdatePreferences_ActiveHoursInverted(t *testing.T) {
	prefs := prefsSetup(t)
	log := zerolog.Nop()

	h := UpdatePreferences(prefs, log)

	body := `{"active_hours_start":"22:00","active_hours_end":"07:00"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestUpdatePreferences_UnknownKey(t *testing.T) {
	prefs := prefsSetup(t)
	log := zerolog.Nop()

	h := UpdatePreferences(prefs, log)

	body := `{"nonexistent_key":"value"}`
	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdatePreferences_EmptyBody(t *testing.T) {
	prefs := prefsSetup(t)
	h := UpdatePreferences(prefs, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdatePreferences_MalformedJSON(t *testing.T) {
	prefs := prefsSetup(t)
	h := UpdatePreferences(prefs, zerolog.Nop())

	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader("{not json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestUpdatePreferences_MinGapTooSmall(t *testing.T) {
	prefs := prefsSetup(t)
	h := UpdatePreferences(prefs, zerolog.Nop())

	body := `{"min_gap_minutes":5}`
	req := httptest.NewRequest(http.MethodPatch, "/api/preferences", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}
