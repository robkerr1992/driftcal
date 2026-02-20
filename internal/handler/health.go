package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

type healthResponse struct {
	Status        string  `json:"status"`
	UptimeSeconds float64 `json:"uptime_seconds"`
	Database      string  `json:"database"`
}

// Health returns a handler that reports server health and database connectivity.
// Returns 200 if healthy, 503 if the database is unreachable.
func Health(db *sql.DB, startTime time.Time, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dbStatus := "ok"
		httpStatus := http.StatusOK

		if err := db.PingContext(r.Context()); err != nil {
			dbStatus = "unreachable"
			httpStatus = http.StatusServiceUnavailable
		}

		resp := healthResponse{
			Status:        "healthy",
			UptimeSeconds: time.Since(startTime).Seconds(),
			Database:      dbStatus,
		}

		if httpStatus != http.StatusOK {
			resp.Status = "degraded"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpStatus)
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Error().Err(err).Msg("failed to encode health response")
		}
	}
}
