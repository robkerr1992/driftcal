package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/internal/handler"
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

	// Health check (unauthenticated)
	r.Get("/health", handler.Health(s.db, s.startTime))

	// API routes (authentication added in later milestones)
	r.Route("/api", func(r chi.Router) {
		// TODO: add auth middleware (Bearer token check against cfg.APIKey)
	})

	return r
}

// requestLogger returns middleware that logs each request using zerolog.
func requestLogger(log zerolog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

			defer func() {
				log.Info().
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
