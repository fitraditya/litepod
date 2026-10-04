package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// staleVisitorAge and sweepInterval bound how long a per-IP limiter is kept
// around after its last request, so a long-running process doesn't leak
// memory for clients that stop sending traffic.
const (
	staleVisitorAge = 3 * time.Minute
	sweepInterval   = time.Minute

	// Fallback limits used when rps/burst aren't positive (e.g. a caller,
	// such as a test, constructs config.Config directly and skips
	// config.Load's defaulting) — a limiter with a zero burst would reject
	// every single request, which is never the intent of an unset value.
	fallbackRPS   = 20.0
	fallbackBurst = 40
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimit throttles requests per client IP using a token-bucket limiter, so
// a single misbehaving or compromised caller can't exhaust node resources
// (CPU, Docker daemon connections, open fds) by hammering the API. Run after
// chi's RealIP middleware so r.RemoteAddr already reflects the real client
// when behind a trusted proxy.
func RateLimit(rps float64, burst int) func(http.Handler) http.Handler {
	if rps <= 0 {
		rps = fallbackRPS
	}
	if burst <= 0 {
		burst = fallbackBurst
	}

	var mu sync.Mutex
	visitors := make(map[string]*visitor)

	go func() {
		for range time.Tick(sweepInterval) {
			mu.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > staleVisitorAge {
					delete(visitors, ip)
				}
			}
			mu.Unlock()
		}
	}()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = r.RemoteAddr
			}

			mu.Lock()
			v, ok := visitors[ip]
			if !ok {
				v = &visitor{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
				visitors[ip] = v
			}
			v.lastSeen = time.Now()
			allowed := v.limiter.Allow()
			mu.Unlock()

			if !allowed {
				http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
