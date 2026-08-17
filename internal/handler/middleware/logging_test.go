package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fitraditya/litepod/pkg/logger"
)

func TestLogger(t *testing.T) {
	log := logger.NewSilent()

	cases := []struct {
		name   string
		status int
	}{
		{"2xx", http.StatusOK},
		{"4xx", http.StatusBadRequest},
		{"5xx", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			})
			handler := Logger(log)(next)
			req := httptest.NewRequest(http.MethodGet, "/x", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, tc.status, rec.Code)
		})
	}
}
