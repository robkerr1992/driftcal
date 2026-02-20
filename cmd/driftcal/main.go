package main

import (
	"io/fs"
	"log"

	"github.com/robkerr1992/driftcal/internal/config"
	"github.com/robkerr1992/driftcal/internal/database"
	"github.com/robkerr1992/driftcal/internal/logging"
	"github.com/robkerr1992/driftcal/internal/server"

	dbpkg "github.com/robkerr1992/driftcal/db"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	logger := logging.Setup(cfg.LogLevel, cfg.LogFormat)

	db, err := database.Open(cfg.DBPath, logger)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to open database")
	}
	defer db.Close()

	// embed.FS has files at migrations/*.sql — goose needs the FS root
	// to contain .sql files directly, so we extract the subdirectory.
	migrationsFS, err := fs.Sub(dbpkg.Migrations, "migrations")
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to access embedded migrations")
	}

	if err := database.Migrate(db, migrationsFS, logger); err != nil {
		logger.Fatal().Err(err).Msg("failed to run migrations")
	}

	srv := server.New(cfg, db, logger)
	if err := srv.Run(); err != nil {
		logger.Fatal().Err(err).Msg("server error")
	}
}
