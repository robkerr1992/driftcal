package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/nylas"
)

// NylasAccountService defines the Nylas operations needed by account handlers.
type NylasAccountService interface {
	AuthURL(redirectURI, provider string) string
	ExchangeCode(ctx context.Context, code, redirectURI string) (*nylas.TokenResponse, error)
	ListCalendars(ctx context.Context, grantID string) ([]nylas.Calendar, error)
}

// ConnectAccount returns the Nylas OAuth URL for a given provider.
func ConnectAccount(nc NylasAccountService, baseURL string, q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
			return
		}

		provider := strings.ToLower(body.Provider)
		switch provider {
		case "google", "microsoft", "icloud":
			// valid
		default:
			RespondError(w, http.StatusBadRequest, "bad_request", "provider must be google, microsoft, or icloud")
			return
		}

		redirectURI := baseURL + "/api/accounts/callback"
		authURL := nc.AuthURL(redirectURI, provider)

		RespondJSON(w, http.StatusOK, map[string]string{"auth_url": authURL})
	}
}

// AccountCallback handles the OAuth redirect from Nylas.
// It exchanges the code, creates the account and calendars, and returns them.
func AccountCallback(nc NylasAccountService, baseURL string, q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			RespondError(w, http.StatusBadRequest, "bad_request", "missing code parameter")
			return
		}

		ctx := r.Context()
		redirectURI := baseURL + "/api/accounts/callback"

		token, err := nc.ExchangeCode(ctx, code, redirectURI)
		if err != nil {
			log.Error().Err(err).Msg("failed to exchange auth code")
			RespondError(w, http.StatusBadGateway, "exchange_failed", "failed to exchange authorization code")
			return
		}

		account, err := q.CreateAccount(ctx, sqlcdb.CreateAccountParams{
			NylasGrantID: token.GrantID,
			Provider:     token.Provider,
			Email:        token.Email,
			DisplayName:  sql.NullString{String: token.Email, Valid: true},
		})
		if err != nil {
			if isUniqueViolation(err) {
				RespondError(w, http.StatusConflict, "duplicate_account", "this calendar account is already connected")
				return
			}
			log.Error().Err(err).Msg("failed to create account")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to save account")
			return
		}

		nylasCals, err := nc.ListCalendars(ctx, token.GrantID)
		if err != nil {
			log.Error().Err(err).Str("grant_id", token.GrantID).Msg("failed to fetch calendars from Nylas")
			// Account was created; return it even if calendar fetch fails
			RespondJSON(w, http.StatusCreated, map[string]any{
				"account":   account,
				"calendars": []any{},
			})
			return
		}

		var calendars []sqlcdb.Calendar
		for _, nc := range nylasCals {
			cal, err := q.CreateCalendar(ctx, sqlcdb.CreateCalendarParams{
				AccountID:       account.ID,
				NylasCalendarID: nc.ID,
				Name:            nc.Name,
				Color:           sql.NullString{String: nc.HexColor, Valid: nc.HexColor != ""},
			})
			if err != nil {
				log.Warn().Err(err).Str("calendar_id", nc.ID).Msg("failed to create calendar")
				continue
			}
			calendars = append(calendars, cal)
		}

		RespondJSON(w, http.StatusCreated, map[string]any{
			"account":   account,
			"calendars": calendars,
		})
	}
}

// ListAccounts returns all active calendar accounts.
func ListAccounts(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accounts, err := q.ListActiveAccounts(r.Context())
		if err != nil {
			log.Error().Err(err).Msg("failed to list accounts")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to list accounts")
			return
		}
		RespondJSON(w, http.StatusOK, accounts)
	}
}

// DisconnectAccount deactivates a calendar account by ID.
func DisconnectAccount(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid account ID")
			return
		}

		// Verify the account exists
		if _, err := q.GetAccountByID(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "account not found")
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get account")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get account")
			return
		}

		if err := q.DeactivateAccount(r.Context(), id); err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to deactivate account")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to deactivate account")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// isUniqueViolation checks if a SQLite error is a UNIQUE constraint violation.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
