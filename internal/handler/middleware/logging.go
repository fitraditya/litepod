package middleware

import (
	"net/http"
	"time"

	chimiddleware "github.com/go-chi/chi/v5/middleware"

	"github.com/fitraditya/litepod/pkg/logger"
)

// Logger replaces Chi's default request logger with structured logrus output.
// Log level reflects HTTP status: INFO for 2xx/3xx, WARN for 4xx, ERROR for 5xx.
func Logger(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()

			ctx := logger.WithRequestMeta(r.Context(), r.RemoteAddr, chimiddleware.GetReqID(r.Context()))
			r = r.WithContext(ctx)

			next.ServeHTTP(ww, r)

			status := ww.Status()
			entry := log.WithFields(logger.Fields{
				"method":      r.Method,
				"path":        r.RequestURI,
				"status":      status,
				"duration_ms": time.Since(start).Milliseconds(),
				"remote_ip":   r.RemoteAddr,
				"request_id":  chimiddleware.GetReqID(r.Context()),
			})

			switch {
			case status >= 500:
				entry.Error("Request completed")
			case status >= 400:
				entry.Warn("Request completed")
			default:
				entry.Info("Request completed")
			}
		})
	}
}
