package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/config"
	"github.com/robkerr1992/driftcal/internal/nylas"
	syncpkg "github.com/robkerr1992/driftcal/internal/sync"
)

// Server holds the application dependencies and runs the HTTP server.
type Server struct {
	cfg       *config.Config
	db        *sql.DB
	queries   *sqlcdb.Queries
	log       zerolog.Logger
	startTime time.Time
	nylas     *nylas.Client
	syncer    *syncpkg.Syncer
}

// New creates a Server with all required dependencies.
func New(cfg *config.Config, db *sql.DB, log zerolog.Logger) *Server {
	q := sqlcdb.New(db)

	s := &Server{
		cfg:       cfg,
		db:        db,
		queries:   q,
		log:       log,
		startTime: time.Now(),
	}

	// Initialize Nylas client and syncer only if configured
	if cfg.NylasAPIKey != "" {
		s.nylas = nylas.New(cfg.NylasClientID, cfg.NylasAPIKey)
		s.syncer = syncpkg.New(s.nylas, q, log, 15*time.Minute)
	}

	return s
}

// Run starts the HTTP server and blocks until a shutdown signal is received.
// It drains in-flight requests for up to 30 seconds before forcing shutdown.
func (s *Server) Run() error {
	srv := &http.Server{
		Addr:         s.cfg.Addr(),
		Handler:      s.routes(),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Shutdown signal handling
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	serverErr := make(chan error, 1)
	go func() {
		s.log.Info().Str("addr", srv.Addr).Msg("server starting")
		serverErr <- srv.ListenAndServe()
	}()

	// Start syncer after server is up
	if s.syncer != nil {
		s.syncer.Start()
	}

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-shutdown:
		s.log.Info().Str("signal", sig.String()).Msg("shutdown signal received")

		// Stop syncer first
		if s.syncer != nil {
			s.syncer.Stop()
		}

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			s.log.Error().Err(err).Msg("graceful shutdown failed, forcing close")
			srv.Close()
			return err
		}

		s.log.Info().Msg("server stopped gracefully")
	}

	return nil
}
