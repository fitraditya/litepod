package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fitraditya/litepod/internal/config"
	"github.com/fitraditya/litepod/internal/domain"
	"github.com/fitraditya/litepod/internal/usecase"
	"github.com/fitraditya/litepod/pkg/logger"
)

func testLogger() *logger.Logger {
	return logger.NewSilent()
}

func newTestHandler(repo *mockRepo, sys *mockSystemMetrics, cfg *config.Config) (*ContainerHandler, *HealthHandler) {
	if repo == nil {
		repo = &mockRepo{}
	}
	if sys == nil {
		sys = &mockSystemMetrics{}
	}
	if cfg == nil {
		cfg = &config.Config{NodeID: "node-1", MaxMemoryMB: 4096, MaxCPUUnits: 4}
	}
	uc := usecase.NewContainerUseCase(repo, sys, cfg, testLogger())
	return NewContainerHandler(uc, testLogger()), NewHealthHandler(uc, testLogger())
}

// withChiParam wraps req so chi.URLParam(r, "name") resolves inside the
// handler without going through a full router.
func withChiParam(r *http.Request, key, value string) *http.Request {
	rc := chi.NewRouteContext()
	rc.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rc))
}

func doJSON(t *testing.T, h http.HandlerFunc, method, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		require.NoError(t, json.NewEncoder(&buf).Encode(body))
	}
	req := httptest.NewRequest(method, target, &buf)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func validDeployPayload() DeployPayload {
	return DeployPayload{
		Image:         "nginx",
		ContainerName: "app1",
		MemoryLimit:   256 * 1024 * 1024,
		CPULimit:      0.5,
		Ports:         []PortMapping{{ContainerPort: 80, Protocol: "tcp"}},
	}
}

func TestDeployHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		rec := doJSON(t, h.Deploy, http.MethodPost, "/containers", validDeployPayload())
		assert.Equal(t, http.StatusCreated, rec.Code)
		var resp DeployResponse
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
		assert.Equal(t, "container-id", resp.ID)
		assert.Equal(t, "deployed", resp.Status)
	})

	t.Run("invalid json", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/containers", bytes.NewBufferString("{bad"))
		rec := httptest.NewRecorder()
		h.Deploy(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("unknown field rejected", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		body := bytes.NewBufferString(`{"image":"nginx","name":"app1","memory_limit":1,"cpu_limit":0.5,"bogus_field":true}`)
		req := httptest.NewRequest(http.MethodPost, "/containers", body)
		rec := httptest.NewRecorder()
		h.Deploy(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})

	t.Run("usecase validation error maps to 422", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		rec := doJSON(t, h.Deploy, http.MethodPost, "/containers", DeployPayload{})
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("repo error maps to 500", func(t *testing.T) {
		repo := &mockRepo{RunFunc: func(ctx context.Context, spec domain.DeploySpec) (string, error) {
			return "", errors.New("boom")
		}}
		h, _ := newTestHandler(repo, nil, nil)
		rec := doJSON(t, h.Deploy, http.MethodPost, "/containers", validDeployPayload())
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestUpdateHandler(t *testing.T) {
	h, _ := newTestHandler(nil, nil, nil)
	req := httptest.NewRequest(http.MethodPut, "/containers/app1", jsonBody(t, validDeployPayload()))
	req = withChiParam(req, "name", "app1")
	rec := httptest.NewRecorder()
	h.Update(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)

	t.Run("invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPut, "/containers/app1", bytes.NewBufferString("{bad"))
		req = withChiParam(req, "name", "app1")
		rec := httptest.NewRecorder()
		h.Update(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(v))
	return &buf
}

func TestDestroyHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		req := withChiParam(httptest.NewRequest(http.MethodDelete, "/containers/app1", nil), "name", "app1")
		rec := httptest.NewRecorder()
		h.Destroy(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
	t.Run("not found", func(t *testing.T) {
		repo := &mockRepo{RemoveFunc: func(ctx context.Context, name string, force, removeVols bool) error {
			return domain.ErrContainerNotFound
		}}
		h, _ := newTestHandler(repo, nil, nil)
		req := withChiParam(httptest.NewRequest(http.MethodDelete, "/containers/app1", nil), "name", "app1")
		rec := httptest.NewRecorder()
		h.Destroy(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
}

func TestSimpleActionHandlers(t *testing.T) {
	cases := []struct {
		name       string
		call       func(h *ContainerHandler) http.HandlerFunc
		wantStatus string
	}{
		{"Restart", func(h *ContainerHandler) http.HandlerFunc { return h.Restart }, "restarted"},
		{"Start", func(h *ContainerHandler) http.HandlerFunc { return h.Start }, "started"},
		{"Stop", func(h *ContainerHandler) http.HandlerFunc { return h.Stop }, "stopped"},
		{"Reset", func(h *ContainerHandler) http.HandlerFunc { return h.Reset }, "reset"},
		{"Pause", func(h *ContainerHandler) http.HandlerFunc { return h.Pause }, "paused"},
		{"Unpause", func(h *ContainerHandler) http.HandlerFunc { return h.Unpause }, "unpaused"},
		{"Kill", func(h *ContainerHandler) http.HandlerFunc { return h.Kill }, "killed"},
		{"Suspend", func(h *ContainerHandler) http.HandlerFunc { return h.Suspend }, "suspended"},
		{"Unsuspend", func(h *ContainerHandler) http.HandlerFunc { return h.Unsuspend }, "unsuspended"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _ := newTestHandler(nil, nil, nil)
			req := withChiParam(httptest.NewRequest(http.MethodPost, "/containers/app1/x", nil), "name", "app1")
			rec := httptest.NewRecorder()
			tc.call(h)(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code)
			var resp ActionResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			assert.Equal(t, tc.wantStatus, resp.Status)
		})
	}
}

func TestKillHandler_SignalQueryParam(t *testing.T) {
	var got string
	repo := &mockRepo{KillFunc: func(ctx context.Context, name, signal string) error {
		got = signal
		return nil
	}}
	h, _ := newTestHandler(repo, nil, nil)
	req := withChiParam(httptest.NewRequest(http.MethodPost, "/containers/app1/kill?signal=SIGTERM", nil), "name", "app1")
	rec := httptest.NewRecorder()
	h.Kill(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "SIGTERM", got)
}

func TestListHandler(t *testing.T) {
	repo := &mockRepo{ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
		return []domain.ContainerSummary{{ID: "1", Names: []string{"/app1"}, Image: "nginx", State: "running"}}, nil
	}}
	h, _ := newTestHandler(repo, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/containers", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var items []ContainerItem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &items))
	assert.Len(t, items, 1)
}

func TestListHandler_Error(t *testing.T) {
	repo := &mockRepo{ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
		return nil, errors.New("x")
	}}
	h, _ := newTestHandler(repo, nil, nil)
	rec := httptest.NewRecorder()
	h.List(rec, httptest.NewRequest(http.MethodGet, "/containers", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestIPHandler(t *testing.T) {
	repo := &mockRepo{IPFunc: func(ctx context.Context, name string) (string, error) { return "10.0.0.1", nil }}
	h, _ := newTestHandler(repo, nil, nil)
	req := withChiParam(httptest.NewRequest(http.MethodGet, "/containers/app1/ip", nil), "name", "app1")
	rec := httptest.NewRecorder()
	h.IP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp IPResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "10.0.0.1", resp.IP)
}

func TestStatsHandler(t *testing.T) {
	repo := &mockRepo{StatsFunc: func(ctx context.Context, name string) (*domain.Stats, error) {
		return &domain.Stats{CPUPercent: 1.5, MemUsage: 100, MemLimit: 200, NetworkRxBytes: 1, NetworkTxBytes: 2}, nil
	}}
	h, _ := newTestHandler(repo, nil, nil)
	req := withChiParam(httptest.NewRequest(http.MethodGet, "/containers/app1/stats", nil), "name", "app1")
	rec := httptest.NewRecorder()
	h.Stats(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestStateHandler(t *testing.T) {
	repo := &mockRepo{StateFunc: func(ctx context.Context, name string) (*domain.ContainerState, error) {
		return &domain.ContainerState{Status: "running", Running: true}, nil
	}}
	h, _ := newTestHandler(repo, nil, nil)
	req := withChiParam(httptest.NewRequest(http.MethodGet, "/containers/app1/state", nil), "name", "app1")
	rec := httptest.NewRecorder()
	h.State(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLogsHandler(t *testing.T) {
	var gotTail int
	var gotTS bool
	repo := &mockRepo{LogsFunc: func(ctx context.Context, name string, tail int, ts bool) (string, error) {
		gotTail, gotTS = tail, ts
		return "hello", nil
	}}
	h, _ := newTestHandler(repo, nil, nil)
	req := withChiParam(httptest.NewRequest(http.MethodGet, "/containers/app1/logs?tail=50&timestamps=true", nil), "name", "app1")
	rec := httptest.NewRecorder()
	h.Logs(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, 50, gotTail)
	assert.True(t, gotTS)
}

func TestLogsHandler_Error(t *testing.T) {
	repo := &mockRepo{LogsFunc: func(ctx context.Context, name string, tail int, ts bool) (string, error) {
		return "", errors.New("x")
	}}
	h, _ := newTestHandler(repo, nil, nil)
	req := withChiParam(httptest.NewRequest(http.MethodGet, "/containers/app1/logs", nil), "name", "app1")
	rec := httptest.NewRecorder()
	h.Logs(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestNetworkHandlers(t *testing.T) {
	h, _ := newTestHandler(nil, nil, nil)

	t.Run("create", func(t *testing.T) {
		rec := doJSON(t, h.CreateNetwork, http.MethodPost, "/networks", NetworkPayload{Name: "net1"})
		assert.Equal(t, http.StatusCreated, rec.Code)
	})
	t.Run("create invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/networks", bytes.NewBufferString("{bad"))
		rec := httptest.NewRecorder()
		h.CreateNetwork(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("delete success", func(t *testing.T) {
		req := withChiParam(httptest.NewRequest(http.MethodDelete, "/networks/net1", nil), "name", "net1")
		rec := httptest.NewRecorder()
		h.DeleteNetwork(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
	t.Run("list success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ListNetworks(rec, httptest.NewRequest(http.MethodGet, "/networks", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestVolumeHandlers(t *testing.T) {
	h, _ := newTestHandler(nil, nil, nil)

	t.Run("create", func(t *testing.T) {
		rec := doJSON(t, h.CreateVolume, http.MethodPost, "/volumes", VolumePayload{Name: "v1"})
		assert.Equal(t, http.StatusCreated, rec.Code)
	})
	t.Run("create invalid json", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/volumes", bytes.NewBufferString("{bad"))
		rec := httptest.NewRecorder()
		h.CreateVolume(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("delete success", func(t *testing.T) {
		req := withChiParam(httptest.NewRequest(http.MethodDelete, "/volumes/v1", nil), "name", "v1")
		rec := httptest.NewRecorder()
		h.DeleteVolume(rec, req)
		assert.Equal(t, http.StatusNoContent, rec.Code)
	})
	t.Run("list success", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ListVolumes(rec, httptest.NewRequest(http.MethodGet, "/volumes", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}

func TestListImagesHandler(t *testing.T) {
	repo := &mockRepo{ListImagesFunc: func(ctx context.Context) ([]domain.ImageSummary, error) {
		return []domain.ImageSummary{{ID: "sha256:a", RepoTags: []string{"nginx:latest"}, SizeMB: 10}}, nil
	}}
	h, _ := newTestHandler(repo, nil, nil)
	rec := httptest.NewRecorder()
	h.ListImages(rec, httptest.NewRequest(http.MethodGet, "/images", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	var items []ImageItem
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &items))
	assert.Len(t, items, 1)
}

func TestListImagesHandler_Error(t *testing.T) {
	repo := &mockRepo{ListImagesFunc: func(ctx context.Context) ([]domain.ImageSummary, error) {
		return nil, errors.New("x")
	}}
	h, _ := newTestHandler(repo, nil, nil)
	rec := httptest.NewRecorder()
	h.ListImages(rec, httptest.NewRequest(http.MethodGet, "/images", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCheckImageHandler(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		rec := httptest.NewRecorder()
		h.CheckImage(rec, httptest.NewRequest(http.MethodHead, "/images", nil))
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("exists", func(t *testing.T) {
		repo := &mockRepo{ImageExistsFunc: func(ctx context.Context, image string) (bool, error) { return true, nil }}
		h, _ := newTestHandler(repo, nil, nil)
		rec := httptest.NewRecorder()
		h.CheckImage(rec, httptest.NewRequest(http.MethodHead, "/images?name=nginx", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	})
	t.Run("not exists", func(t *testing.T) {
		repo := &mockRepo{ImageExistsFunc: func(ctx context.Context, image string) (bool, error) { return false, nil }}
		h, _ := newTestHandler(repo, nil, nil)
		rec := httptest.NewRecorder()
		h.CheckImage(rec, httptest.NewRequest(http.MethodHead, "/images?name=nginx", nil))
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})
	t.Run("repo error", func(t *testing.T) {
		repo := &mockRepo{ImageExistsFunc: func(ctx context.Context, image string) (bool, error) {
			return false, errors.New("x")
		}}
		h, _ := newTestHandler(repo, nil, nil)
		rec := httptest.NewRecorder()
		h.CheckImage(rec, httptest.NewRequest(http.MethodHead, "/images?name=nginx", nil))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestPullImageHandler(t *testing.T) {
	t.Run("invalid json", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		req := httptest.NewRequest(http.MethodPost, "/images/pull", bytes.NewBufferString("{bad"))
		rec := httptest.NewRecorder()
		h.PullImage(rec, req)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("missing image", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		rec := doJSON(t, h.PullImage, http.MethodPost, "/images/pull", PullImagePayload{})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("incomplete registry_auth rejected", func(t *testing.T) {
		h, _ := newTestHandler(nil, nil, nil)
		rec := doJSON(t, h.PullImage, http.MethodPost, "/images/pull",
			PullImagePayload{Image: "nginx", RegistryAuth: &RegistryAuthPayload{Username: "u"}})
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
	t.Run("registry_auth forwarded to repo", func(t *testing.T) {
		got := make(chan *domain.RegistryAuth, 1)
		repo := &mockRepo{PullImageFunc: func(ctx context.Context, image string, auth *domain.RegistryAuth) error {
			got <- auth
			return nil
		}}
		h, _ := newTestHandler(repo, nil, nil)
		rec := doJSON(t, h.PullImage, http.MethodPost, "/images/pull",
			PullImagePayload{Image: "ghcr.io/a/b", RegistryAuth: &RegistryAuthPayload{Username: "u", Password: "p"}})
		assert.Equal(t, http.StatusAccepted, rec.Code)
		assert.Equal(t, &domain.RegistryAuth{Username: "u", Password: "p"}, <-got)
	})
	t.Run("accepted", func(t *testing.T) {
		done := make(chan struct{})
		repo := &mockRepo{PullImageFunc: func(ctx context.Context, image string, auth *domain.RegistryAuth) error {
			close(done)
			return nil
		}}
		h, _ := newTestHandler(repo, nil, nil)
		rec := doJSON(t, h.PullImage, http.MethodPost, "/images/pull", PullImagePayload{Image: "nginx"})
		assert.Equal(t, http.StatusAccepted, rec.Code)
		<-done // wait for the background goroutine to run, for coverage determinism
	})
}

func TestHandleError(t *testing.T) {
	cases := []struct {
		err        error
		wantStatus int
	}{
		{domain.ErrInvalidInput, http.StatusUnprocessableEntity},
		{domain.ErrInsufficientResources, http.StatusUnprocessableEntity},
		{domain.ErrContainerNotFound, http.StatusNotFound},
		{errors.New("other"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		handleError(rec, tc.err)
		assert.Equal(t, tc.wantStatus, rec.Code)
	}
}

func TestSimpleActionHandlers_Errors(t *testing.T) {
	cases := []struct {
		name string
		call func(h *ContainerHandler) http.HandlerFunc
		fail func(repo *mockRepo)
	}{
		{"Restart", func(h *ContainerHandler) http.HandlerFunc { return h.Restart },
			func(r *mockRepo) {
				r.RestartFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Start", func(h *ContainerHandler) http.HandlerFunc { return h.Start },
			func(r *mockRepo) {
				r.StartFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Stop", func(h *ContainerHandler) http.HandlerFunc { return h.Stop },
			func(r *mockRepo) {
				r.StopFunc = func(ctx context.Context, name string, t int) error { return errors.New("x") }
			}},
		{"Reset", func(h *ContainerHandler) http.HandlerFunc { return h.Reset },
			func(r *mockRepo) {
				r.VolumePathFunc = func(ctx context.Context, name string) (string, error) { return "", errors.New("x") }
			}},
		{"Pause", func(h *ContainerHandler) http.HandlerFunc { return h.Pause },
			func(r *mockRepo) {
				r.PauseFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Unpause", func(h *ContainerHandler) http.HandlerFunc { return h.Unpause },
			func(r *mockRepo) {
				r.UnpauseFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Kill", func(h *ContainerHandler) http.HandlerFunc { return h.Kill },
			func(r *mockRepo) {
				r.KillFunc = func(ctx context.Context, name, sig string) error { return errors.New("x") }
			}},
		{"Suspend", func(h *ContainerHandler) http.HandlerFunc { return h.Suspend },
			func(r *mockRepo) {
				r.StopFunc = func(ctx context.Context, name string, t int) error { return errors.New("x") }
			}},
		{"Unsuspend", func(h *ContainerHandler) http.HandlerFunc { return h.Unsuspend },
			func(r *mockRepo) {
				r.RestartFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"IP", func(h *ContainerHandler) http.HandlerFunc { return h.IP },
			func(r *mockRepo) {
				r.IPFunc = func(ctx context.Context, name string) (string, error) { return "", errors.New("x") }
			}},
		{"Stats", func(h *ContainerHandler) http.HandlerFunc { return h.Stats },
			func(r *mockRepo) {
				r.StatsFunc = func(ctx context.Context, name string) (*domain.Stats, error) { return nil, errors.New("x") }
			}},
		{"State", func(h *ContainerHandler) http.HandlerFunc { return h.State },
			func(r *mockRepo) {
				r.StateFunc = func(ctx context.Context, name string) (*domain.ContainerState, error) { return nil, errors.New("x") }
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{}
			tc.fail(repo)
			h, _ := newTestHandler(repo, nil, nil)
			req := withChiParam(httptest.NewRequest(http.MethodGet, "/containers/app1/x", nil), "name", "app1")
			rec := httptest.NewRecorder()
			tc.call(h)(rec, req)
			assert.Equal(t, http.StatusInternalServerError, rec.Code)
		})
	}
}

func TestNetworkHandlers_Errors(t *testing.T) {
	repo := &mockRepo{
		CreateNetworkFunc: func(ctx context.Context, name string) error { return errors.New("x") },
		DeleteNetworkFunc: func(ctx context.Context, name string) error { return errors.New("x") },
		ListNetworksFunc: func(ctx context.Context) ([]string, error) {
			return nil, errors.New("x")
		},
	}
	h, _ := newTestHandler(repo, nil, nil)

	rec := doJSON(t, h.CreateNetwork, http.MethodPost, "/networks", NetworkPayload{Name: "net1"})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	req := withChiParam(httptest.NewRequest(http.MethodDelete, "/networks/net1", nil), "name", "net1")
	rec = httptest.NewRecorder()
	h.DeleteNetwork(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	rec = httptest.NewRecorder()
	h.ListNetworks(rec, httptest.NewRequest(http.MethodGet, "/networks", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestVolumeHandlers_Errors(t *testing.T) {
	repo := &mockRepo{
		CreateVolumeFunc: func(ctx context.Context, name string) error { return errors.New("x") },
		DeleteVolumeFunc: func(ctx context.Context, name string) error { return errors.New("x") },
		ListVolumesFunc: func(ctx context.Context) ([]string, error) {
			return nil, errors.New("x")
		},
	}
	h, _ := newTestHandler(repo, nil, nil)

	rec := doJSON(t, h.CreateVolume, http.MethodPost, "/volumes", VolumePayload{Name: "v1"})
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	req := withChiParam(httptest.NewRequest(http.MethodDelete, "/volumes/v1", nil), "name", "v1")
	rec = httptest.NewRecorder()
	h.DeleteVolume(rec, req)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)

	rec = httptest.NewRecorder()
	h.ListVolumes(rec, httptest.NewRequest(http.MethodGet, "/volumes", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDeployHandler_WithFullFields(t *testing.T) {
	h, _ := newTestHandler(nil, nil, nil)
	payload := validDeployPayload()
	payload.Volumes = []VolumeBind{{Type: "docker", VolumeName: "u1_vol", BindPath: "/data"}}
	payload.Ports = []PortMapping{{ContainerPort: 80, Protocol: "tcp"}}
	payload.Healthcheck = &Healthcheck{Test: []string{"CMD", "true"}, IntervalSeconds: 5, TimeoutSeconds: 2, Retries: 3}
	rec := doJSON(t, h.Deploy, http.MethodPost, "/containers", payload)
	assert.Equal(t, http.StatusCreated, rec.Code)
}

func TestHealthHandler(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		_, hh := newTestHandler(nil, nil, nil)
		rec := httptest.NewRecorder()
		hh.Health(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
		assert.Equal(t, http.StatusOK, rec.Code)
	})
}
