package handler

import (
	"net/http"
	"net/http/httptest"
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
		assert.Equal(t, http.StatusForbidden, rec.Code)
	})

	t.Run("wrong key", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/containers", nil)
		req.Header.Set("X-API-KEY", "nope")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusForbidden, rec.Code)
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
		assert.Equal(t, http.StatusForbidden, rec.Code, path)
	}
}
