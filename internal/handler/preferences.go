package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/rs/zerolog"

	"github.com/robkerr1992/driftcal/internal/preferences"
)

// knownPrefKeys are the keys accepted by PATCH /api/preferences.
// Validated keys are checked strictly; deferred keys are stored as-is.
var validatedPrefKeys = map[string]bool{
	"timezone":           true,
	"active_hours_start": true,
	"active_hours_end":   true,
	"min_gap_minutes":    true,
}

var deferredPrefKeys = map[string]bool{
	"interests":      true,
	"energy_profile": true,
	"digest_time":    true,
	"latitude":       true,
	"longitude":      true,
	"city":           true,
	"state":          true,
}

// GetPreferences returns all preferences in the nested API format.
func GetPreferences(prefs *preferences.Store, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		formatted, err := prefs.FormatForAPI(r.Context())
		if err != nil {
			log.Error().Err(err).Msg("failed to format preferences")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load preferences", log)
			return
		}
		RespondJSON(w, http.StatusOK, map[string]any{"preferences": formatted}, log)
	}
}

// UpdatePreferences handles PATCH updates to user preferences.
func UpdatePreferences(prefs *preferences.Store, log zerolog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			RespondError(w, http.StatusBadRequest, "bad_request", "invalid JSON body", log)
			return
		}

		if len(body) == 0 {
			RespondError(w, http.StatusBadRequest, "bad_request", "no preferences provided", log)
			return
		}

		// Reject unknown keys.
		for key := range body {
			if !validatedPrefKeys[key] && !deferredPrefKeys[key] {
				safeKey := key
				if len(safeKey) > 50 {
					safeKey = safeKey[:50]
				}
				RespondError(w, http.StatusBadRequest, "bad_request", "unknown preference key: "+safeKey, log)
				return
			}
		}

		ctx := r.Context()

		// Validate and collect updates.
		updates := make(map[string]string)

		for key, raw := range body {
			if validatedPrefKeys[key] {
				var strVal string
				switch key {
				case "timezone":
					if err := json.Unmarshal(raw, &strVal); err != nil {
						RespondError(w, http.StatusBadRequest, "bad_request", "timezone must be a string", log)
						return
					}
					if _, err := time.LoadLocation(strVal); err != nil {
						RespondError(w, http.StatusBadRequest, "bad_request", "invalid timezone: "+strVal, log)
						return
					}
					updates[key] = strVal

				case "active_hours_start", "active_hours_end":
					if err := json.Unmarshal(raw, &strVal); err != nil {
						RespondError(w, http.StatusBadRequest, "bad_request", key+" must be a string", log)
						return
					}
					if !hhmmRegex.MatchString(strVal) {
						RespondError(w, http.StatusBadRequest, "bad_request", key+" must be HH:MM format", log)
						return
					}
					updates[key] = strVal

				case "min_gap_minutes":
					var intVal int
					if err := json.Unmarshal(raw, &intVal); err != nil {
						RespondError(w, http.StatusBadRequest, "bad_request", "min_gap_minutes must be an integer", log)
						return
					}
					if intVal < 15 {
						RespondError(w, http.StatusBadRequest, "bad_request", "min_gap_minutes must be >= 15", log)
						return
					}
					updates[key] = strconv.Itoa(intVal)
				}
			} else {
				// Deferred keys: store the raw JSON as-is.
				updates[key] = string(raw)
			}
		}

		// Cross-field validation: active_hours_end must be after active_hours_start.
		startVal := updates["active_hours_start"]
		endVal := updates["active_hours_end"]

		// If only one is provided, load the other from DB.
		if startVal != "" && endVal == "" {
			var err error
			endVal, err = prefs.ActiveHoursEnd(ctx)
			if err != nil {
				log.Error().Err(err).Msg("failed to load active_hours_end")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to validate active hours", log)
				return
			}
		} else if startVal == "" && endVal != "" {
			var err error
			startVal, err = prefs.ActiveHoursStart(ctx)
			if err != nil {
				log.Error().Err(err).Msg("failed to load active_hours_start")
				RespondError(w, http.StatusInternalServerError, "internal_error", "failed to validate active hours", log)
				return
			}
		}

		// HH:MM strings sort lexicographically = chronologically, so string comparison is correct.
		if startVal != "" && endVal != "" && endVal <= startVal {
			RespondError(w, http.StatusBadRequest, "bad_request", "active_hours_end must be after active_hours_start", log)
			return
		}

		// Persist all updates atomically.
		if err := prefs.SetMany(ctx, updates); err != nil {
			log.Error().Err(err).Msg("failed to save preferences")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to save preferences", log)
			return
		}

		// Return updated preferences.
		formatted, err := prefs.FormatForAPI(ctx)
		if err != nil {
			log.Error().Err(err).Msg("failed to format preferences after update")
			RespondError(w, http.StatusInternalServerError, "internal_error", "failed to load preferences", log)
			return
		}
		RespondJSON(w, http.StatusOK, map[string]any{"preferences": formatted}, log)
	}
}
