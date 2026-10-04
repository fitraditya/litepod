package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fitraditya/litepod/internal/config"
)

func TestWebhookAuth(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	cfg := &config.Config{APIKey: "main", WebhookAPIKey: "hook"}
	h := WebhookAuth(cfg)(next)

	cases := []struct {
		name   string
		header map[string]string
		want   int
	}{
		{"missing", nil, http.StatusUnauthorized},
		{"wrong bearer", map[string]string{"Authorization": "Bearer nope"}, http.StatusUnauthorized},
		{"no scheme", map[string]string{"Authorization": "hook"}, http.StatusUnauthorized},
		{"empty bearer", map[string]string{"Authorization": "Bearer "}, http.StatusUnauthorized},
		{"main key via X-API-KEY", map[string]string{"X-API-KEY": "main"}, http.StatusUnauthorized},
		{"main key as bearer", map[string]string{"Authorization": "Bearer main"}, http.StatusUnauthorized},
		{"correct", map[string]string{"Authorization": "Bearer hook"}, http.StatusOK},
		{"scheme case-insensitive", map[string]string{"Authorization": "bearer hook"}, http.StatusOK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", nil)
			for k, v := range c.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			assert.Equal(t, c.want, rec.Code)
		})
	}

	t.Run("unset key never authenticates", func(t *testing.T) {
		h := WebhookAuth(&config.Config{})(next)
		req := httptest.NewRequest(http.MethodPost, "/", nil)
		req.Header.Set("Authorization", "Bearer ")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
}
