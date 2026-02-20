package handler

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/nylas"
	syncpkg "github.com/robkerr1992/driftcal/internal/sync"
)

// NylasEventService defines the Nylas operations needed by the webhook handler.
type NylasEventService interface {
	GetEvent(ctx context.Context, grantID, eventID, calendarID string) (*nylas.Event, error)
}

// NylasWebhook handles incoming Nylas webhook notifications.
// It validates HMAC-SHA256 signatures, handles the challenge handshake,
// and dispatches event.created/updated/deleted notifications.
func NylasWebhook(secret string, nc NylasEventService, q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Handle Nylas challenge handshake (GET with ?challenge=xxx)
		if challenge := r.URL.Query().Get("challenge"); challenge != "" {
			if len(challenge) > 256 {
				http.Error(w, "invalid challenge", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(challenge))
			return
		}

		// Limit body size to 1MB
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

		body, err := io.ReadAll(r.Body)
		if err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				RespondError(w, http.StatusRequestEntityTooLarge, "body_too_large", "request body too large", log)
			} else {
				log.Error().Err(err).Msg("failed to read webhook body")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to read request body", log)
			}
			return
		}

		// Verify HMAC-SHA256 signature
		sig := r.Header.Get("X-Nylas-Signature")
		if !verifyHMAC(secret, body, sig) {
			log.Warn().Msg("invalid webhook signature")
			RespondError(w, http.StatusUnauthorized, "unauthorized", "invalid webhook signature", log)
			return
		}

		var payload nylas.WebhookPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Warn().Err(err).Msg("failed to parse webhook payload")
			// Still return 200 — Nylas requires it to avoid retries
			w.WriteHeader(http.StatusOK)
			return
		}

		ctx := r.Context()

		switch payload.Type {
		case "event.created", "event.updated":
			handleEventUpsert(ctx, payload, nc, q, log)
		case "event.deleted":
			handleEventDelete(ctx, payload, q, log)
		default:
			log.Debug().Str("type", payload.Type).Msg("ignoring unhandled webhook type")
		}

		// Always return 200 to Nylas
		w.WriteHeader(http.StatusOK)
	}
}

func handleEventUpsert(ctx context.Context, payload nylas.WebhookPayload, nc NylasEventService, q *sqlcdb.Queries, log zerolog.Logger) {
	// Parse the event from the webhook object
	var ev nylas.Event
	if err := json.Unmarshal(payload.Data.Object, &ev); err != nil {
		log.Warn().Err(err).Msg("failed to parse webhook event object")
		return
	}

	// Look up the calendar by Nylas calendar ID
	cal, err := q.GetCalendarByNylasID(ctx, ev.CalendarID)
	if err != nil {
		log.Warn().Err(err).Str("nylas_calendar_id", ev.CalendarID).Msg("calendar not found for webhook event")
		return
	}

	params, err := syncpkg.NormalizeEvent(cal.ID, &ev)
	if err != nil {
		log.Warn().Err(err).Str("event_id", ev.ID).Msg("failed to normalize webhook event")
		return
	}

	if _, err := q.UpsertEvent(ctx, params); err != nil {
		log.Error().Err(err).Str("event_id", ev.ID).Msg("failed to upsert webhook event")
		return
	}

	log.Info().Str("type", payload.Type).Str("event_id", ev.ID).Msg("webhook event processed")
}

func handleEventDelete(ctx context.Context, payload nylas.WebhookPayload, q *sqlcdb.Queries, log zerolog.Logger) {
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload.Data.Object, &obj); err != nil {
		log.Warn().Err(err).Msg("failed to parse deleted event ID")
		return
	}

	if err := q.SoftDeleteEventByNylasID(ctx, obj.ID); err != nil {
		log.Error().Err(err).Str("event_id", obj.ID).Msg("failed to soft-delete event")
		return
	}

	log.Info().Str("event_id", obj.ID).Msg("webhook event soft-deleted")
}

func verifyHMAC(secret string, body []byte, signature string) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
