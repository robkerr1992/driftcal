package handler

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"modernc.org/sqlite"

	"github.com/robkerr1992/driftcal/gen/sqlcdb"
	"github.com/robkerr1992/driftcal/internal/nylas"
	"github.com/robkerr1992/driftcal/internal/preferences"
)

const stateMaxAge = 10 * time.Minute

// NylasAccountService defines the Nylas operations needed by account handlers.
type NylasAccountService interface {
	AuthURL(redirectURI, provider, state string) string
	ExchangeCode(ctx context.Context, code, redirectURI string) (*nylas.TokenResponse, error)
	ListCalendars(ctx context.Context, grantID string) ([]nylas.Calendar, error)
	CreateCalendar(ctx context.Context, grantID string, req nylas.CreateCalendarRequest) (*nylas.Calendar, error)
}

const driftCalCalendarName = "DriftCal"

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func storeState(ctx context.Context, q *sqlcdb.Queries, state string) error {
	// Lazily clean expired states.
	q.DeleteExpiredOAuthStates(ctx, time.Now().UTC())
	return q.InsertOAuthState(ctx, sqlcdb.InsertOAuthStateParams{
		State:     state,
		ExpiresAt: time.Now().UTC().Add(stateMaxAge),
	})
}

func consumeState(ctx context.Context, q *sqlcdb.Queries, state string) bool {
	_, err := q.ConsumeOAuthState(ctx, sqlcdb.ConsumeOAuthStateParams{
		State:     state,
		ExpiresAt: time.Now().UTC(),
	})
	return err == nil
}

// ConnectAccount returns the Nylas OAuth URL for a given provider.
func ConnectAccount(nc NylasAccountService, baseURL string, q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", log)
			return
		}

		provider := strings.ToLower(body.Provider)
		switch provider {
		case "google", "microsoft", "icloud":
			// valid
		default:
			RespondError(w, http.StatusBadRequest, "bad_request", "provider must be google, microsoft, or icloud", log)
			return
		}

		state, err := generateState()
		if err != nil {
			log.Error().Err(err).Msg("failed to generate OAuth state")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to generate state", log)
			return
		}
		if err := storeState(r.Context(), q, state); err != nil {
			log.Error().Err(err).Msg("failed to store OAuth state")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to store state", log)
			return
		}

		redirectURI := baseURL + "/api/accounts/callback"
		authURL := nc.AuthURL(redirectURI, provider, state)

		RespondJSON(w, http.StatusOK, map[string]string{"auth_url": authURL}, log)
	}
}

// AccountCallback handles the OAuth redirect from Nylas.
// It exchanges the code, creates the account and calendars, and returns them.
// On the first connected account, it auto-creates a dedicated "DriftCal" calendar.
func AccountCallback(nc NylasAccountService, baseURL string, q *sqlcdb.Queries, prefs *preferences.Store, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			RespondError(w, http.StatusBadRequest, "bad_request", "missing code parameter", log)
			return
		}

		state := r.URL.Query().Get("state")
		if state == "" || !consumeState(r.Context(), q, state) {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid or expired state parameter", log)
			return
		}

		ctx := r.Context()
		redirectURI := baseURL + "/api/accounts/callback"

		token, err := nc.ExchangeCode(ctx, code, redirectURI)
		if err != nil {
			log.Error().Err(err).Msg("failed to exchange auth code")
			RespondError(w, http.StatusBadGateway, "exchange_failed", "failed to exchange authorization code", log)
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
				RespondError(w, http.StatusConflict, "duplicate_account", "this calendar account is already connected", log)
				return
			}
			log.Error().Err(err).Msg("failed to create account")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to save account", log)
			return
		}

		nylasCals, err := nc.ListCalendars(ctx, token.GrantID)
		if err != nil {
			log.Error().Err(err).Str("grant_id", token.GrantID).Msg("failed to fetch calendars from Nylas")
			// Account was created; return it even if calendar fetch fails
			RespondJSON(w, http.StatusCreated, map[string]any{
				"account":   account,
				"calendars": []any{},
			}, log)
			return
		}

		var calendars []sqlcdb.Calendar
		for _, nylCal := range nylasCals {
			cal, err := q.CreateCalendar(ctx, sqlcdb.CreateCalendarParams{
				AccountID:       account.ID,
				NylasCalendarID: nylCal.ID,
				Name:            nylCal.Name,
				Color:           sql.NullString{String: nylCal.HexColor, Valid: nylCal.HexColor != ""},
			})
			if err != nil {
				log.Warn().Err(err).Str("calendar_id", nylCal.ID).Msg("failed to create calendar")
				continue
			}
			calendars = append(calendars, cal)
		}

		// Auto-create a dedicated DriftCal calendar on the first connected account.
		// Best-effort: failures log warnings but don't break the OAuth callback.
		existingGrantID, _ := prefs.Get(ctx, "nylas_grant_id")
		if existingGrantID == "" {
			nylasCal, err := nc.CreateCalendar(ctx, token.GrantID, nylas.CreateCalendarRequest{
				Name:        driftCalCalendarName,
				Description: "Auto-created by DriftCal for scheduling suggestions and goals",
			})
			if err != nil {
				log.Warn().Err(err).Str("grant_id", token.GrantID).Msg("failed to create DriftCal calendar on Nylas")
			} else {
				dbCal, err := q.CreateCalendar(ctx, sqlcdb.CreateCalendarParams{
					AccountID:       account.ID,
					NylasCalendarID: nylasCal.ID,
					Name:            nylasCal.Name,
				})
				if err != nil {
					log.Warn().Err(err).Str("nylas_calendar_id", nylasCal.ID).Msg("failed to save DriftCal calendar to DB")
				} else {
					if err := q.UpdateCalendarFlags(ctx, sqlcdb.UpdateCalendarFlagsParams{
						ID:         dbCal.ID,
						IsBlocking: false,
						IsActive:   true,
					}); err != nil {
						log.Warn().Err(err).Int64("calendar_id", dbCal.ID).Msg("failed to set DriftCal calendar flags")
					} else {
						dbCal.IsBlocking = false
					}

					if err := prefs.SetMany(ctx, map[string]string{
						"nylas_grant_id":    token.GrantID,
						"nylas_calendar_id": nylasCal.ID,
					}); err != nil {
						log.Warn().Err(err).Msg("failed to set DriftCal calendar preferences")
					}

					calendars = append(calendars, dbCal)
				}
			}
		}

		RespondJSON(w, http.StatusCreated, map[string]any{
			"account":   account,
			"calendars": calendars,
		}, log)
	}
}

// ListAccounts returns all active calendar accounts.
func ListAccounts(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accounts, err := q.ListActiveAccounts(r.Context())
		if err != nil {
			log.Error().Err(err).Msg("failed to list accounts")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to list accounts", log)
			return
		}
		RespondJSON(w, http.StatusOK, accounts, log)
	}
}

// DisconnectAccount deactivates a calendar account by ID.
func DisconnectAccount(q *sqlcdb.Queries, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := chi.URLParam(r, "id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid account ID", log)
			return
		}

		// Verify the account exists
		if _, err := q.GetAccountByID(r.Context(), id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				RespondError(w, http.StatusNotFound, "not_found", "account not found", log)
				return
			}
			log.Error().Err(err).Int64("id", id).Msg("failed to get account")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to get account", log)
			return
		}

		if err := q.DeactivateAccount(r.Context(), id); err != nil {
			log.Error().Err(err).Int64("id", id).Msg("failed to deactivate account")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to deactivate account", log)
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

const sqliteConstraintUnique = 2067

// isUniqueViolation checks if a SQLite error is a UNIQUE constraint violation.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	if errors.As(err, &sqliteErr) {
		return sqliteErr.Code() == sqliteConstraintUnique
	}
	return false
}
