package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/internal/handler"
	authmw "github.com/robkerr1992/driftcal/internal/middleware"
	"github.com/robkerr1992/driftcal/internal/telegram"
)

// routes builds the chi router with middleware and all route registrations.
func (s *Server) routes() http.Handler {
	r := chi.NewRouter()

	// Core middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(s.log))
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(authmw.MaxBodySize(1 << 20)) // 1MB default

	// Health check (unauthenticated)
	r.Get("/health", handler.Health(s.db, s.startTime, s.log))

	// Unauthenticated API routes (browser redirect + webhook HMAC)
	r.Get("/api/accounts/callback", handler.AccountCallback(s.nylas, s.cfg.BaseURL, s.queries, s.prefs, s.log))
	if s.cfg.NylasWebhookSecret != "" {
		r.Post("/api/webhooks/nylas", handler.NylasWebhook(s.cfg.NylasWebhookSecret, s.nylas, s.queries, s.log))
	}
	if s.telegramBot != nil {
		r.Post("/api/webhooks/telegram", telegram.WebhookHandler(s.telegramBot))
	}

	// Authenticated API routes (Bearer token)
	r.Route("/api", func(r chi.Router) {
		r.Use(authmw.BearerAuth(s.cfg.APIKey, s.log))

		r.Post("/accounts/connect", handler.ConnectAccount(s.nylas, s.cfg.BaseURL, s.queries, s.log))
		r.Get("/accounts", handler.ListAccounts(s.queries, s.log))
		r.Delete("/accounts/{id}", handler.DisconnectAccount(s.queries, s.log))
		r.Get("/calendars", handler.ListCalendars(s.queries, s.log))
		r.Patch("/calendars/{id}", handler.UpdateCalendar(s.queries, s.log))

		r.Get("/gaps", handler.GapsHandler(s.queries, s.prefs, s.log))

		r.Get("/protected-blocks", handler.ListProtectedBlocks(s.queries, s.log))
		r.Post("/protected-blocks", handler.CreateProtectedBlock(s.queries, s.log))
		r.Delete("/protected-blocks/{id}", handler.DeleteProtectedBlock(s.queries, s.log))

		r.Get("/preferences", handler.GetPreferences(s.prefs, s.log))
		r.Patch("/preferences", handler.UpdatePreferences(s.prefs, s.log))

		r.Get("/goals", handler.ListGoals(s.queries, s.log))
		r.Post("/goals", handler.CreateGoal(s.queries, s.log))
		r.Patch("/goals/{id}", handler.UpdateGoal(s.queries, s.log))
		r.Delete("/goals/{id}", handler.DeleteGoal(s.queries, s.log))
		r.Get("/goals/{id}/instances", handler.ListGoalInstances(s.queries, s.log))
		r.Post("/goals/{id}/instances/{instance_id}/skip", handler.SkipGoalInstance(s.queries, s.prefs, s.nylas, s.log))
		r.Post("/goals/{id}/instances/{instance_id}/complete", handler.CompleteGoalInstance(s.queries, s.log))

		r.Get("/suggestions", handler.ListSuggestions(s.queries, s.prefs, s.log))
		r.Post("/suggestions/{id}/approve", handler.ApproveSuggestion(s.queries, s.nylas, s.log))
		r.Post("/suggestions/{id}/reject", handler.RejectSuggestion(s.queries, s.log))
		r.Post("/suggestions/generate", handler.TriggerPipeline(s.pipeline, s.log))
	})

	return r
}

// requestLogger returns middleware that logs each request using zerolog.
// Log level is escalated based on response status: 5xx→Error, 4xx→Warn.
func requestLogger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				status := ww.Status()
				var logEvent *zerolog.Event
				switch {
				case r.URL.Path == "/health":
					logEvent = log.Debug()
				case status >= 500:
					logEvent = log.Error()
				case status >= 400:
					logEvent = log.Warn()
				default:
					logEvent = log.Info()
				}
				logEvent.
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Int("status", ww.Status()).
					Int("bytes", ww.BytesWritten()).
					Dur("duration", time.Since(start)).
					Str("request_id", middleware.GetReqID(r.Context())).
					Msg("request")
			}()

			next.ServeHTTP(ww, r)
		})
	}
}
