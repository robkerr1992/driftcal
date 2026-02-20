package database

import (
	"io/fs"
	"path/filepath"
	"testing"

	"github.com/rs/zerolog"

	dbpkg "github.com/robkerr1992/driftcal/db"
)

func TestOpen_SetsPragmas(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.Nop()

	db, err := Open(dbPath, log)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	checks := []struct {
		pragma string
		want   string
	}{
		{"PRAGMA journal_mode", "wal"},
		{"PRAGMA foreign_keys", "1"},
		{"PRAGMA synchronous", "1"},    // NORMAL = 1
		{"PRAGMA temp_store", "2"},     // MEMORY = 2
		{"PRAGMA busy_timeout", "5000"},
		{"PRAGMA cache_size", "20000"},
	}

	for _, c := range checks {
		var got string
		if err := db.QueryRow(c.pragma).Scan(&got); err != nil {
			t.Fatalf("querying %s: %v", c.pragma, err)
		}
		if got != c.want {
			t.Errorf("%s = %q, want %q", c.pragma, got, c.want)
		}
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	log := zerolog.Nop()
	_, err := Open("/nonexistent/path/to/db.sqlite", log)
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
}

func TestMigrate_CreatesAllTables(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.Nop()

	db, err := Open(dbPath, log)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	migrationsFS, err := fs.Sub(dbpkg.Migrations, "migrations")
	if err != nil {
		t.Fatalf("accessing embedded migrations: %v", err)
	}

	if err := Migrate(db, migrationsFS, log); err != nil {
		t.Fatalf("Migrate failed: %v", err)
	}

	expectedTables := []string{
		"calendar_accounts",
		"calendars",
		"events",
		"pipeline_runs",
		"daily_gaps",
		"activity_suggestions",
		"suggestion_feedback",
		"user_preferences",
		"recurring_goals",
		"goal_instances",
		"protected_blocks",
	}

	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'goose_%' AND name NOT LIKE 'sqlite_%'")
	if err != nil {
		t.Fatalf("querying sqlite_master: %v", err)
	}
	defer rows.Close()

	tables := make(map[string]bool)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scanning table name: %v", err)
		}
		tables[name] = true
	}

	for _, want := range expectedTables {
		if !tables[want] {
			t.Errorf("expected table %q not found", want)
		}
	}
}

func TestMigrate_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	log := zerolog.Nop()

	db, err := Open(dbPath, log)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer db.Close()

	migrationsFS, err := fs.Sub(dbpkg.Migrations, "migrations")
	if err != nil {
		t.Fatalf("accessing embedded migrations: %v", err)
	}

	if err := Migrate(db, migrationsFS, log); err != nil {
		t.Fatalf("first Migrate failed: %v", err)
	}

	if err := Migrate(db, migrationsFS, log); err != nil {
		t.Fatalf("second Migrate failed (not idempotent): %v", err)
	}
}
