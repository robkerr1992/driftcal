package database

import (
	"database/sql"
	"io/fs"
	"testing"

	"github.com/rs/zerolog"

	dbpkg "github.com/robkerr1992/driftcal/db"
)

// TestDB opens an in-memory SQLite database with pragmas and migrations applied.
// It registers t.Cleanup to close the database when the test finishes.
func TestDB(t *testing.T) *sql.DB {
	t.Helper()

	log := zerolog.Nop()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	// Apply pragmas (skip WAL for in-memory)
	pragmas := []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA temp_store=MEMORY",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			t.Fatalf("setting pragma %q: %v", p, err)
		}
	}

	migrationsFS, err := fs.Sub(dbpkg.Migrations, "migrations")
	if err != nil {
		t.Fatalf("accessing embedded migrations: %v", err)
	}

	if err := Migrate(db, migrationsFS, log); err != nil {
		t.Fatalf("running migrations: %v", err)
	}

	return db
}
