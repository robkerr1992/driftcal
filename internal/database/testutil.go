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
// MaxOpenConns is set to 1 because in-memory SQLite is per-connection —
// a second connection would see an empty database.
func TestDB(t *testing.T) *sql.DB {
	t.Helper()

	log := zerolog.Nop()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("opening test database: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })

	if err := applyPragmas(db, true); err != nil {
		t.Fatalf("applying pragmas: %v", err)
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
