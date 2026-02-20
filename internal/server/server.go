package server

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/internal/config"
)

// Server holds the application dependencies and runs the HTTP server.
type Server struct {
	cfg       *config.Config
	db        *sql.DB
	log       zerolog.Logger
	startTime time.Time
}

// New creates a Server with all required dependencies.
func New(cfg *config.Config, db *sql.DB, log zerolog.Logger) *Server {
	return &Server{
		cfg:       cfg,
		db:        db,
		log:       log,
		startTime: time.Now(),
	}
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

	select {
	case err := <-serverErr:
		return err
	case sig := <-shutdown:
		s.log.Info().Str("signal", sig.String()).Msg("shutdown signal received")

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
