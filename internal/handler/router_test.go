package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/fitraditya/litepod/internal/config"
)

func TestRouter_HealthIsPublic(t *testing.T) {
	cfg := &config.Config{NodeID: "node-1", APIKey: "secret", MaxMemoryMB: 4096, MaxCPUUnits: 4}
	ch, hh := newTestHandler(nil, nil, cfg)
	router := NewRouter(cfg, ch, hh, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestRouter_ContainersRequiresAuth(t *testing.T) {
	cfg := &config.Config{NodeID: "node-1", APIKey: "secret", MaxMemoryMB: 4096, MaxCPUUnits: 4}
	ch, hh := newTestHandler(nil, nil, cfg)
	router := NewRouter(cfg, ch, hh, testLogger())

	t.Run("missing key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/containers", nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("wrong key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/containers", nil)
		req.Header.Set("X-API-KEY", "nope")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("correct key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/containers", nil)
		req.Header.Set("X-API-KEY", "secret")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestRouter_SwaggerRedirect(t *testing.T) {
	cfg := &config.Config{NodeID: "node-1", APIKey: "secret"}
	ch, hh := newTestHandler(nil, nil, cfg)
	router := NewRouter(cfg, ch, hh, testLogger())

	req := httptest.NewRequest(http.MethodGet, "/swagger", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusMovedPermanently, rec.Code)
}

// TestRouter_AllRoutesResolve exercises every documented method+path through
// the real chi mux (not by calling handlers directly) so a routing
// misconfiguration - like a method handler and a Route() sub-router
// registered on the same pattern, which silently produces 405 - gets caught
// here instead of only in production.
func TestRouter_AllRoutesResolve(t *testing.T) {
	cfg := &config.Config{NodeID: "node-1", APIKey: "secret", MaxMemoryMB: 4096, MaxCPUUnits: 4}
	ch, hh := newTestHandler(nil, nil, cfg)
	router := NewRouter(cfg, ch, hh, testLogger())

	cases := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/containers"},
		{http.MethodPost, "/containers"},
		{http.MethodPut, "/containers/app1"},
		{http.MethodDelete, "/containers/app1"},
		{http.MethodPost, "/containers/app1/start"},
		{http.MethodPost, "/containers/app1/stop"},
		{http.MethodPost, "/containers/app1/restart"},
		{http.MethodPost, "/containers/app1/reset"},
		{http.MethodPost, "/containers/app1/pause"},
		{http.MethodPost, "/containers/app1/unpause"},
		{http.MethodPost, "/containers/app1/kill"},
		{http.MethodPost, "/containers/app1/suspend"},
		{http.MethodPost, "/containers/app1/unsuspend"},
		{http.MethodGet, "/containers/app1/stats"},
		{http.MethodGet, "/containers/app1/ip"},
		{http.MethodGet, "/containers/app1/state"},
		{http.MethodGet, "/containers/app1/logs"},
		{http.MethodGet, "/images"},
		{http.MethodHead, "/images"},
		{http.MethodPost, "/images/pull"},
		{http.MethodGet, "/networks"},
		{http.MethodPost, "/networks"},
		{http.MethodDelete, "/networks/net1"},
		{http.MethodGet, "/volumes"},
		{http.MethodPost, "/volumes"},
		{http.MethodDelete, "/volumes/vol1"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set("X-API-KEY", "secret")
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assert.NotEqual(t, http.StatusMethodNotAllowed, rec.Code, "route did not resolve: %s %s", tc.method, tc.path)
			assert.NotEqual(t, http.StatusNotFound, rec.Code, "route did not resolve: %s %s", tc.method, tc.path)
		})
	}
}

func TestRouter_ImagesNetworksVolumesRequireAuth(t *testing.T) {
	cfg := &config.Config{NodeID: "node-1", APIKey: "secret", MaxMemoryMB: 4096, MaxCPUUnits: 4}
	ch, hh := newTestHandler(nil, nil, cfg)
	router := NewRouter(cfg, ch, hh, testLogger())

	for _, path := range []string{"/images?name=x", "/networks", "/volumes"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code, path)
	}
}

func TestRouter_Webhook(t *testing.T) {
	post := func(router http.Handler, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/webhook/containers/app/deploy", strings.NewReader(`{"image":"ghcr.io/a/b:v2"}`))
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	repo := &mockRepo{ContainerImageFunc: func(ctx context.Context, name string) (string, error) { return "ghcr.io/a/b:v1", nil }}

	t.Run("not mounted without webhook key", func(t *testing.T) {
		cfg := &config.Config{NodeID: "n", APIKey: "secret"}
		ch, hh := newTestHandler(repo, nil, cfg)
		rec := post(NewRouter(cfg, ch, hh, testLogger()), map[string]string{"Authorization": "Bearer "})
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	cfg := &config.Config{NodeID: "n", APIKey: "secret", WebhookAPIKey: "hook"}
	ch, hh := newTestHandler(repo, nil, cfg)
	router := NewRouter(cfg, ch, hh, testLogger())

	t.Run("main key rejected", func(t *testing.T) {
		assert.Equal(t, http.StatusUnauthorized, post(router, map[string]string{"X-API-KEY": "secret"}).Code)
	})
	t.Run("webhook key rejected on main routes", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/containers", nil)
		req.Header.Set("Authorization", "Bearer hook")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})
	t.Run("redeploys", func(t *testing.T) {
		rec := post(router, map[string]string{"Authorization": "Bearer hook"})
		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Contains(t, rec.Body.String(), "redeployed")
	})
	t.Run("other repository rejected", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/webhook/containers/app/deploy", strings.NewReader(`{"image":"evil/x:1"}`))
		req.Header.Set("Authorization", "Bearer hook")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})
}
