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
	"github.com/robkerr1992/driftcal/internal/action"
	"github.com/robkerr1992/driftcal/internal/config"
	"github.com/robkerr1992/driftcal/internal/nylas"
	"github.com/robkerr1992/driftcal/internal/pipeline"
	"github.com/robkerr1992/driftcal/internal/preferences"
	"github.com/robkerr1992/driftcal/internal/scheduler"
	"github.com/robkerr1992/driftcal/internal/suggest"
	syncpkg "github.com/robkerr1992/driftcal/internal/sync"
	"github.com/robkerr1992/driftcal/internal/telegram"
	"github.com/robkerr1992/driftcal/internal/weather"
)

// Server holds the application dependencies and runs the HTTP server.
type Server struct {
	cfg       *config.Config
	db        *sql.DB
	queries   *sqlcdb.Queries
	prefs     *preferences.Store
	log       zerolog.Logger
	startTime time.Time
	nylas          *nylas.Client
	syncer         *syncpkg.Syncer
	weatherClient  *weather.Client
	suggestClient  *suggest.Client
	pipeline       *pipeline.Runner
	telegramBot    *telegram.Bot
	scheduler      *scheduler.Scheduler
}

// New creates a Server with all required dependencies.
func New(cfg *config.Config, db *sql.DB, log zerolog.Logger) *Server {
	q := sqlcdb.New(db)

	s := &Server{
		cfg:       cfg,
		db:        db,
		queries:   q,
		prefs:     preferences.New(q, db),
		log:       log,
		startTime: time.Now(),
	}

	// Initialize Nylas client and syncer only if configured.
	if cfg.NylasAPIKey != "" {
		s.nylas = nylas.New(cfg.NylasClientID, cfg.NylasAPIKey)
		s.syncer = syncpkg.New(s.nylas, q, log, 15*time.Minute)
	}

	// Initialize weather and suggestion clients if API keys present.
	s.weatherClient = weather.New(cfg.OpenWeatherAPIKey)
	s.suggestClient = suggest.New(cfg.AnthropicAPIKey, cfg.AnthropicModel)

	// Initialize pipeline runner if suggestion client is available.
	if s.suggestClient != nil {
		var weatherFetcher pipeline.WeatherFetcher
		if s.weatherClient != nil {
			weatherFetcher = s.weatherClient
		}
		var nylasCreator pipeline.NylasEventCreator
		if s.nylas != nil {
			nylasCreator = s.nylas
		}
		s.pipeline = pipeline.New(db, q, s.syncer, s.prefs, weatherFetcher, s.suggestClient, nylasCreator, log)
	}

	// Initialize Telegram bot if configured.
	if cfg.TelegramBotToken != "" && cfg.TelegramAuthorizedUser != 0 {
		tgClient := telegram.New(cfg.TelegramBotToken)
		var pipelineRunner telegram.PipelineRunner
		if s.pipeline != nil {
			pipelineRunner = s.pipeline
		}
		var nylasEvt action.NylasEventCreator
		if s.nylas != nil {
			nylasEvt = s.nylas
		}
		s.telegramBot = telegram.NewBot(
			tgClient,
			cfg.TelegramAuthorizedUser,
			cfg.TelegramWebhookSecret,
			q,
			s.prefs,
			nylasEvt,
			pipelineRunner,
			log,
		)
	}

	// Initialize scheduler when both pipeline and telegram bot are available.
	if s.pipeline != nil && s.telegramBot != nil {
		s.scheduler = scheduler.New(s.prefs, db, q, s.pipeline, s.telegramBot, log)
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

	// Start scheduler after syncer (non-fatal on error).
	if s.scheduler != nil {
		if err := s.scheduler.Start(context.Background()); err != nil {
			s.log.Warn().Err(err).Msg("failed to start scheduler (non-fatal)")
		}
	}

	// Register Telegram webhook (non-fatal on error).
	if s.telegramBot != nil && s.cfg.BaseURL != "" {
		webhookURL := s.cfg.BaseURL + "/api/webhooks/telegram"
		if err := s.telegramBot.RegisterWebhook(context.Background(), webhookURL, s.cfg.TelegramWebhookSecret); err != nil {
			s.log.Warn().Err(err).Msg("failed to register Telegram webhook (non-fatal)")
		} else {
			s.log.Info().Str("url", webhookURL).Msg("Telegram webhook registered")
		}
	}

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case sig := <-shutdown:
		s.log.Info().Str("signal", sig.String()).Msg("shutdown signal received")

		// Stop scheduler first (no new pipeline runs start).
		if s.scheduler != nil {
			s.scheduler.Stop()
		}

		// Then stop syncer.
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
