package preferences

import (
	"context"
	"testing"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/database"
)

func setup(t *testing.T) *Store {
	t.Helper()
	db := database.TestDB(t)
	q := sqlcdb.New(db)
	return New(q, db)
}

func TestStore_DefaultTimezone(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	loc, err := store.Timezone(ctx)
	if err != nil {
		t.Fatalf("Timezone: %v", err)
	}
	if loc.String() != "UTC" {
		t.Errorf("default timezone = %q, want UTC", loc.String())
	}
}

func TestStore_SetAndGetTimezone(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	if err := store.Set(ctx, "timezone", "America/New_York"); err != nil {
		t.Fatalf("Set timezone: %v", err)
	}

	loc, err := store.Timezone(ctx)
	if err != nil {
		t.Fatalf("Timezone: %v", err)
	}
	if loc.String() != "America/New_York" {
		t.Errorf("timezone = %q, want America/New_York", loc.String())
	}
}

func TestStore_MinGapMinutes(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	// Default
	mins, err := store.MinGapMinutes(ctx)
	if err != nil {
		t.Fatalf("MinGapMinutes: %v", err)
	}
	if mins != 45 {
		t.Errorf("default MinGapMinutes = %d, want 45", mins)
	}

	// Set custom
	if err := store.Set(ctx, "min_gap_minutes", "30"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	mins, err = store.MinGapMinutes(ctx)
	if err != nil {
		t.Fatalf("MinGapMinutes: %v", err)
	}
	if mins != 30 {
		t.Errorf("MinGapMinutes = %d, want 30", mins)
	}
}

func TestStore_FormatForAPI(t *testing.T) {
	store := setup(t)
	ctx := context.Background()

	// Set some values
	if err := store.Set(ctx, "timezone", "America/New_York"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := store.Set(ctx, "active_hours_start", "08:00"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	result, err := store.FormatForAPI(ctx)
	if err != nil {
		t.Fatalf("FormatForAPI: %v", err)
	}

	// Check timezone
	if tz, ok := result["timezone"].(string); !ok || tz != "America/New_York" {
		t.Errorf("timezone = %v, want America/New_York", result["timezone"])
	}

	// Check nested active_hours
	ah, ok := result["active_hours"].(map[string]string)
	if !ok {
		t.Fatalf("active_hours is not map[string]string: %T", result["active_hours"])
	}
	if ah["start"] != "08:00" {
		t.Errorf("active_hours.start = %q, want 08:00", ah["start"])
	}
	if ah["end"] != "22:00" {
		t.Errorf("active_hours.end = %q, want 22:00 (default)", ah["end"])
	}

	// Check default min_gap_minutes
	if mgm, ok := result["min_gap_minutes"].(int); !ok || mgm != 45 {
		t.Errorf("min_gap_minutes = %v, want 45", result["min_gap_minutes"])
	}

	// Check interests default is empty slice
	interests, ok := result["interests"].([]any)
	if !ok {
		t.Fatalf("interests is not []any: %T", result["interests"])
	}
	if len(interests) != 0 {
		t.Errorf("interests length = %d, want 0", len(interests))
	}
}
