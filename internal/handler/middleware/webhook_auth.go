package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/fitraditya/litepod/internal/config"
)

// WebhookAuth validates "Authorization: Bearer <key>" against the configured
// webhook key (constant-time). It is independent of Auth: the main X-API-KEY
// does not open webhook routes, and the webhook key does not open anything else.
func WebhookAuth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			const prefix = "Bearer "
			h := r.Header.Get("Authorization")
			got := ""
			if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
				got = h[len(prefix):]
			}
			// Empty configured key must never authenticate (see Auth).
			if cfg.WebhookAPIKey == "" || got == "" ||
				subtle.ConstantTimeCompare([]byte(got), []byte(cfg.WebhookAPIKey)) != 1 {
				w.Header().Set("WWW-Authenticate", "Bearer")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
