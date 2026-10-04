package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/fitraditya/litepod/internal/config"
)

// Auth validates the X-API-KEY header against the configured API key using a
// constant-time comparison, so response timing can't be used to brute-force
// the key byte-by-byte.
func Auth(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got := r.Header.Get("X-API-KEY")
			// An empty configured key must never authenticate a request, even
			// one sent with no X-API-KEY header at all — ConstantTimeCompare
			// treats two empty byte slices as equal, which would otherwise
			// let every request through when APIKey is unset.
			if cfg.APIKey == "" || subtle.ConstantTimeCompare([]byte(got), []byte(cfg.APIKey)) != 1 {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
