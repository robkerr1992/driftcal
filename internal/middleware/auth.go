package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/robkerr1992/driftcal/internal/handler"
)

// BearerAuth returns middleware that validates requests carry a Bearer token
// matching the provided API key. Comparison uses constant-time comparison
// to prevent timing attacks.
func BearerAuth(apiKey string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				handler.RespondError(w, http.StatusUnauthorized, "unauthorized", "missing Authorization header")
				return
			}

			if !strings.HasPrefix(auth, "Bearer ") {
				handler.RespondError(w, http.StatusUnauthorized, "unauthorized", "invalid Authorization header format")
				return
			}

			token := strings.TrimPrefix(auth, "Bearer ")
			if subtle.ConstantTimeCompare([]byte(token), []byte(apiKey)) != 1 {
				handler.RespondError(w, http.StatusUnauthorized, "unauthorized", "invalid API key")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
