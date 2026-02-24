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

// applyPragmas sets shared SQLite pragmas on a connection. When skipWAL is
// true, journal_mode and cache_size are omitted (in-memory DBs don't support WAL).
func applyPragmas(db *sql.DB, skipWAL bool) error {
	pragmas := []string{
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
		"PRAGMA temp_store=MEMORY",
	}

	if !skipWAL {
		pragmas = append([]string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA cache_size=20000",
		}, pragmas...)
	}

	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("setting pragma %q: %w", p, err)
		}
	}
	return nil
}

// Open creates a SQLite connection with WAL mode and recommended pragmas.
func Open(dbPath string, log zerolog.Logger) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	// SQLite pragmas are per-connection. Limiting the pool to a single
	// connection ensures every query inherits WAL mode, foreign keys, etc.
	// WAL still allows concurrent reads from other processes (Litestream).
	db.SetMaxOpenConns(1)

	if err := applyPragmas(db, false); err != nil {
		db.Close()
		return nil, err
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

	version, err := goose.GetDBVersion(db)
	if err != nil {
		return fmt.Errorf("getting migration version: %w", err)
	}

	log.Info().Int64("version", version).Msg("migrations applied")
	return nil
}
