package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/gap"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

func gapsSetup(t *testing.T) (*sqlcdb.Queries, *preferences.Store) {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	prefs := preferences.New(q, db)
	return q, prefs
}

// seedEvent creates a blocking event in the test DB.
func seedEvent(t *testing.T, q *sqlcdb.Queries, calID int64, title string, start, end time.Time) {
	t.Helper()
	_, err := q.UpsertEvent(context.Background(), sqlcdb.UpsertEventParams{
		CalendarID:   calID,
		NylasEventID: "nylas-" + title + "-" + start.Format(time.RFC3339),
		Title:        sql.NullString{String: title, Valid: true},
		StartTime:    start,
		EndTime:      end,
		Status:       "confirmed",
		Busy:         "busy",
	})
	if err != nil {
		t.Fatalf("seeding event %q: %v", title, err)
	}
}

func TestGaps_MissingParams(t *testing.T) {
	q, prefs := gapsSetup(t)
	log := zerolog.Nop()

	h := GapsHandler(q, prefs, log)

	// No start or end.
	req := httptest.NewRequest(http.MethodGet, "/api/gaps", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGaps_RangeTooLarge(t *testing.T) {
	q, prefs := gapsSetup(t)
	log := zerolog.Nop()

	h := GapsHandler(q, prefs, log)

	req := httptest.NewRequest(http.MethodGet, "/api/gaps?start=2026-01-01T00:00:00Z&end=2026-06-01T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestGaps_EmptyCalendar(t *testing.T) {
	q, prefs := gapsSetup(t)
	log := zerolog.Nop()

	h := GapsHandler(q, prefs, log)

	req := httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-21T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Gaps []gap.Gap `json:"gaps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	// Default active hours 07:00-22:00 UTC → one big gap.
	if len(resp.Gaps) != 1 {
		t.Fatalf("got %d gaps, want 1", len(resp.Gaps))
	}
	if resp.Gaps[0].DurationMinutes != 900 {
		t.Errorf("duration = %d, want 900 (15 hours)", resp.Gaps[0].DurationMinutes)
	}
}

func TestGaps_WithEvents(t *testing.T) {
	q, prefs := gapsSetup(t)
	log := zerolog.Nop()

	// Create account + calendar.
	account := createTestAccount(t, q)
	cal := createTestCalendar(t, q, account.ID)

	// Seed an event: 10:00-11:00 UTC (within default active hours 07:00-22:00 UTC).
	seedEvent(t, q, cal.ID, "Team Meeting",
		time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC))

	h := GapsHandler(q, prefs, log)

	req := httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-21T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Gaps []gap.Gap `json:"gaps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	// Event splits active hours into 2 gaps: 07:00-10:00 (180 min) and 11:00-22:00 (660 min).
	if len(resp.Gaps) != 2 {
		t.Fatalf("got %d gaps, want 2; gaps: %+v", len(resp.Gaps), resp.Gaps)
	}
	if resp.Gaps[0].DurationMinutes != 180 {
		t.Errorf("gap[0].duration = %d, want 180", resp.Gaps[0].DurationMinutes)
	}
	if resp.Gaps[1].DurationMinutes != 660 {
		t.Errorf("gap[1].duration = %d, want 660", resp.Gaps[1].DurationMinutes)
	}
}

func TestGaps_MinMinutesOverride(t *testing.T) {
	q, prefs := gapsSetup(t)
	log := zerolog.Nop()

	// Create account + calendar.
	account := createTestAccount(t, q)
	cal := createTestCalendar(t, q, account.ID)

	// Seed events that create a 20-minute gap.
	seedEvent(t, q, cal.ID, "A",
		time.Date(2026, 2, 20, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 9, 40, 0, 0, time.UTC))
	seedEvent(t, q, cal.ID, "B",
		time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC))

	h := GapsHandler(q, prefs, log)

	// With default min_minutes=45, the 20-min gap is filtered out.
	req := httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-21T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp1 struct {
		Gaps []gap.Gap `json:"gaps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp1); err != nil {
		t.Fatalf("decoding resp1: %v", err)
	}
	gapsDefault := len(resp1.Gaps)

	// With min_minutes=15, the 20-min gap should appear.
	req = httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-21T00:00:00Z&min_minutes=15", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var resp2 struct {
		Gaps []gap.Gap `json:"gaps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp2); err != nil {
		t.Fatalf("decoding resp2: %v", err)
	}

	if len(resp2.Gaps) <= gapsDefault {
		t.Errorf("min_minutes=15 returned %d gaps, expected more than default %d",
			len(resp2.Gaps), gapsDefault)
	}
}

func TestGaps_InvalidDateFormat(t *testing.T) {
	q, prefs := gapsSetup(t)
	h := GapsHandler(q, prefs, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet, "/api/gaps?start=2026-02-20&end=2026-02-21", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGaps_EndEqualsStart(t *testing.T) {
	q, prefs := gapsSetup(t)
	h := GapsHandler(q, prefs, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-20T00:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGaps_InvalidMinMinutes(t *testing.T) {
	q, prefs := gapsSetup(t)
	h := GapsHandler(q, prefs, zerolog.Nop())

	req := httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-21T00:00:00Z&min_minutes=abc", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}

func TestGaps_MinMinutesZero(t *testing.T) {
	q, prefs := gapsSetup(t)
	h := GapsHandler(q, prefs, zerolog.Nop())

	// Create account + calendar with events creating a 20-min gap.
	account := createTestAccount(t, q)
	cal := createTestCalendar(t, q, account.ID)
	seedEvent(t, q, cal.ID, "A",
		time.Date(2026, 2, 20, 7, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 9, 40, 0, 0, time.UTC))
	seedEvent(t, q, cal.ID, "B",
		time.Date(2026, 2, 20, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 11, 0, 0, 0, time.UTC))

	// Explicit min_minutes=0 should return ALL gaps, including the 20-min one.
	req := httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T00:00:00Z&end=2026-02-21T00:00:00Z&min_minutes=0", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Gaps []gap.Gap `json:"gaps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	// With min_minutes=0, the 20-min gap (09:40-10:00) should appear alongside
	// the 660-min gap (11:00-22:00). Event A starts at 07:00 (active start), so
	// there's no gap before it — total is exactly 2 gaps.
	if len(resp.Gaps) != 2 {
		t.Errorf("min_minutes=0 returned %d gaps, want 2; gaps: %+v", len(resp.Gaps), resp.Gaps)
	}
	// Verify the 20-min gap is present (it would be filtered at default min_minutes=45).
	found20 := false
	for _, g := range resp.Gaps {
		if g.DurationMinutes == 20 {
			found20 = true
		}
	}
	if !found20 {
		t.Errorf("expected a 20-minute gap with min_minutes=0, gaps: %+v", resp.Gaps)
	}
}

func TestGaps_MultiDayNonUTC(t *testing.T) {
	q, prefs := gapsSetup(t)

	// Set timezone to America/New_York.
	if err := prefs.Set(context.Background(), "timezone", "America/New_York"); err != nil {
		t.Fatalf("setting timezone: %v", err)
	}
	if err := prefs.Set(context.Background(), "active_hours_start", "09:00"); err != nil {
		t.Fatalf("setting active_hours_start: %v", err)
	}
	if err := prefs.Set(context.Background(), "active_hours_end", "17:00"); err != nil {
		t.Fatalf("setting active_hours_end: %v", err)
	}

	account := createTestAccount(t, q)
	cal := createTestCalendar(t, q, account.ID)

	// Seed events on two different days in New York time.
	// Feb 20 at 10:00 EST (15:00 UTC) to 11:00 EST (16:00 UTC)
	seedEvent(t, q, cal.ID, "Day1Meeting",
		time.Date(2026, 2, 20, 15, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 20, 16, 0, 0, 0, time.UTC))
	// Feb 21 at 14:00 EST (19:00 UTC) to 15:00 EST (20:00 UTC)
	seedEvent(t, q, cal.ID, "Day2Meeting",
		time.Date(2026, 2, 21, 19, 0, 0, 0, time.UTC),
		time.Date(2026, 2, 21, 20, 0, 0, 0, time.UTC))

	h := GapsHandler(q, prefs, zerolog.Nop())

	// Query a 3-day range. Use EST-aligned times (UTC-5) so the range covers
	// exactly Feb 20-22 in New York time.
	// Feb 20 00:00 EST = Feb 20 05:00 UTC, Feb 23 00:00 EST = Feb 23 05:00 UTC
	req := httptest.NewRequest(http.MethodGet,
		"/api/gaps?start=2026-02-20T05:00:00Z&end=2026-02-23T05:00:00Z", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp struct {
		Gaps []gap.Gap `json:"gaps"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	// Day 1 (Feb 20): 09:00-10:00 EST + 11:00-17:00 EST = 2 gaps
	// Day 2 (Feb 21): 09:00-14:00 EST + 15:00-17:00 EST = 2 gaps
	// Day 3 (Feb 22): 09:00-17:00 EST = 1 gap (no events)
	// Total: 5 gaps (all > default 45 min)
	if len(resp.Gaps) != 5 {
		t.Errorf("got %d gaps, want 5; gaps: %+v", len(resp.Gaps), resp.Gaps)
	}
}
