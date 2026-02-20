package database

import (
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"github.com/rs/zerolog"

	// Pure Go SQLite driver — registers as "sqlite"
	_ "modernc.org/sqlite"
)

// Open creates a SQLite connection with WAL mode and recommended pragmas.
func Open(dbPath string, log zerolog.Logger) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// WAL mode + performance pragmas for a single-user SQLite setup
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=20000",
		"PRAGMA foreign_keys=ON",
		"PRAGMA temp_store=MEMORY",
	}

	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("setting pragma %q: %w", p, err)
		}
	}

	log.Info().Str("path", dbPath).Msg("database opened")
	return db, nil
}

// Migrate runs all pending goose migrations from the embedded filesystem.
// The migrationsFS should contain .sql files at its root (use fs.Sub to
// strip the "migrations/" prefix from an embed.FS).
func Migrate(db *sql.DB, migrationsFS fs.FS, log zerolog.Logger) error {
	goose.SetBaseFS(migrationsFS)
	defer goose.SetBaseFS(nil)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("setting goose dialect: %w", err)
	}

	goose.SetLogger(goose.NopLogger())

	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	log.Info().Msg("migrations applied")
	return nil
}
