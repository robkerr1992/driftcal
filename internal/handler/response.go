package handler

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// RespondJSON writes data as a JSON response with the given HTTP status code.
func RespondJSON(w http.ResponseWriter, status int, data any, log zerolog.Logger) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Error().Err(err).Msg("failed to encode JSON response")
	}
}

// RespondError writes a standard error envelope as JSON.
func RespondError(w http.ResponseWriter, status int, code, message string, log zerolog.Logger) {
	RespondJSON(w, status, errorBody{
		Error: errorDetail{Code: code, Message: message},
	}, log)
}
