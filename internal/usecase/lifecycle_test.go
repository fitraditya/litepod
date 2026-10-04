package usecase

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fitraditya/litepod/internal/config"
	"github.com/fitraditya/litepod/internal/domain"
	"github.com/fitraditya/litepod/pkg/logger"
)

func testLogger() *logger.Logger {
	return logger.NewSilent()
}

func newTestUC(repo *mockRepo, sys *mockSystemMetrics, cfg *config.Config) *ContainerUseCase {
	if repo == nil {
		repo = &mockRepo{}
	}
	if sys == nil {
		sys = &mockSystemMetrics{}
	}
	if cfg == nil {
		cfg = &config.Config{NodeID: "node-1", MaxMemoryMB: 4096, MaxCPUUnits: 4}
	}
	return NewContainerUseCase(repo, sys, cfg, testLogger())
}

func validDeployReq(t *testing.T, volumeBase string) domain.DeployRequest {
	t.Helper()
	return domain.DeployRequest{
		Image:       "nginx",
		Name:        "app1",
		MemoryLimit: 256 * 1024 * 1024,
		CPULimit:    0.5,
		Ports:       []domain.PortMapping{{ContainerPort: 80, Protocol: "tcp"}},
	}
}

func TestDeploy_Success(t *testing.T) {
	base := t.TempDir()
	cfg := &config.Config{NodeID: "node-1", MaxMemoryMB: 4096, MaxCPUUnits: 4, VolumeBase: base}
	repo := &mockRepo{
		RunFunc: func(ctx context.Context, spec domain.DeploySpec) (string, error) {
			return "cid-123", nil
		},
	}
	uc := newTestUC(repo, nil, cfg)

	req := validDeployReq(t, base)
	req.Volumes = []domain.VolumeBind{
		{Type: "host", HostPath: "/data", BindPath: "/data"},
	}

	res, err := uc.Deploy(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "cid-123", res.ContainerID)
	assert.Equal(t, "node-1", res.NodeID)

	// prepareVolume should have created the directory under volumeBase
	info, statErr := os.Stat(filepath.Join(base, "data"))
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())
}

func TestDeploy_ValidationError(t *testing.T) {
	uc := newTestUC(nil, nil, nil)
	_, err := uc.Deploy(context.Background(), domain.DeployRequest{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestDeploy_AdmissionDenied(t *testing.T) {
	cfg := &config.Config{NodeID: "node-1", MaxMemoryMB: 1, MaxCPUUnits: 4}
	uc := newTestUC(nil, nil, cfg)
	req := validDeployReq(t, "")
	_, err := uc.Deploy(context.Background(), req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInsufficientResources))
}

func TestDeploy_PortConflict(t *testing.T) {
	cfg := &config.Config{NodeID: "node-1", MaxMemoryMB: 4096, MaxCPUUnits: 4}
	repo := &mockRepo{
		ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
			return []domain.ContainerSummary{
				{Names: []string{"/other"}, Ports: []domain.PortMapping{{HostPort: 8080, ContainerPort: 80}}},
			}, nil
		},
	}
	uc := newTestUC(repo, nil, cfg)
	req := validDeployReq(t, "")
	req.Ports = []domain.PortMapping{{HostPort: 8080, ContainerPort: 80}}

	_, err := uc.Deploy(context.Background(), req)
	require.Error(t, err)
	assert.True(t, errors.Is(err, domain.ErrInvalidInput))
}

func TestDeploy_CanAcceptListError(t *testing.T) {
	repo := &mockRepo{
		ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
			return nil, errors.New("docker down")
		},
	}
	uc := newTestUC(repo, nil, nil)
	req := validDeployReq(t, "")
	_, err := uc.Deploy(context.Background(), req)
	require.Error(t, err)
}

func TestDeploy_RunError(t *testing.T) {
	repo := &mockRepo{
		RunFunc: func(ctx context.Context, spec domain.DeploySpec) (string, error) {
			return "", errors.New("create failed")
		},
	}
	uc := newTestUC(repo, nil, nil)
	req := validDeployReq(t, "")
	_, err := uc.Deploy(context.Background(), req)
	require.Error(t, err)
}

func TestDeploy_HostVolumePrepareError(t *testing.T) {
	// A file (not dir) as volumeBase makes MkdirAll fail underneath it.
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))

	cfg := &config.Config{NodeID: "node-1", MaxMemoryMB: 4096, MaxCPUUnits: 4, VolumeBase: blocker}
	uc := newTestUC(nil, nil, cfg)

	req := validDeployReq(t, blocker)
	req.Volumes = []domain.VolumeBind{{Type: "host", HostPath: "/sub", BindPath: "/data"}}

	_, err := uc.Deploy(context.Background(), req)
	require.Error(t, err)
}

func TestUpdate_Success(t *testing.T) {
	repo := &mockRepo{}
	uc := newTestUC(repo, nil, nil)
	req := validDeployReq(t, "")
	res, err := uc.Update(context.Background(), "app1", req)
	require.NoError(t, err)
	assert.Equal(t, "container-id", res.ContainerID)
}

func TestUpdate_ValidationError(t *testing.T) {
	uc := newTestUC(nil, nil, nil)
	_, err := uc.Update(context.Background(), "app1", domain.DeployRequest{})
	require.Error(t, err)
}

func TestUpdate_PortConflict(t *testing.T) {
	repo := &mockRepo{
		ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
			return []domain.ContainerSummary{
				{Names: []string{"/other"}, Ports: []domain.PortMapping{{HostPort: 9000, ContainerPort: 80}}},
			}, nil
		},
	}
	uc := newTestUC(repo, nil, nil)
	req := validDeployReq(t, "")
	req.Ports = []domain.PortMapping{{HostPort: 9000, ContainerPort: 80}}
	_, err := uc.Update(context.Background(), "app1", req)
	require.Error(t, err)
}

func TestUpdate_RunError(t *testing.T) {
	repo := &mockRepo{
		RunFunc: func(ctx context.Context, spec domain.DeploySpec) (string, error) {
			return "", errors.New("boom")
		},
	}
	uc := newTestUC(repo, nil, nil)
	req := validDeployReq(t, "")
	_, err := uc.Update(context.Background(), "app1", req)
	require.Error(t, err)
}

func TestDestroy(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		uc := newTestUC(&mockRepo{}, nil, nil)
		assert.NoError(t, uc.Destroy(context.Background(), "app1"))
	})
	t.Run("remove error", func(t *testing.T) {
		repo := &mockRepo{RemoveFunc: func(ctx context.Context, name string, force, removeVols bool) error {
			return errors.New("remove failed")
		}}
		uc := newTestUC(repo, nil, nil)
		assert.Error(t, uc.Destroy(context.Background(), "app1"))
	})
}

func TestSimpleLifecycleOps(t *testing.T) {
	cases := []struct {
		name string
		call func(uc *ContainerUseCase) error
		fail func(repo *mockRepo)
	}{
		{"Start", func(uc *ContainerUseCase) error { return uc.Start(context.Background(), "app1") },
			func(r *mockRepo) {
				r.StartFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Stop", func(uc *ContainerUseCase) error { return uc.Stop(context.Background(), "app1") },
			func(r *mockRepo) {
				r.StopFunc = func(ctx context.Context, name string, t int) error { return errors.New("x") }
			}},
		{"Restart", func(uc *ContainerUseCase) error { return uc.Restart(context.Background(), "app1") },
			func(r *mockRepo) {
				r.RestartFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Pause", func(uc *ContainerUseCase) error { return uc.Pause(context.Background(), "app1") },
			func(r *mockRepo) {
				r.PauseFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Unpause", func(uc *ContainerUseCase) error { return uc.Unpause(context.Background(), "app1") },
			func(r *mockRepo) {
				r.UnpauseFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
		{"Suspend", func(uc *ContainerUseCase) error { return uc.Suspend(context.Background(), "app1") },
			func(r *mockRepo) {
				r.StopFunc = func(ctx context.Context, name string, t int) error { return errors.New("x") }
			}},
		{"Unsuspend", func(uc *ContainerUseCase) error { return uc.Unsuspend(context.Background(), "app1") },
			func(r *mockRepo) {
				r.RestartFunc = func(ctx context.Context, name string) error { return errors.New("x") }
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name+"_success", func(t *testing.T) {
			uc := newTestUC(&mockRepo{}, nil, nil)
			assert.NoError(t, tc.call(uc))
		})
		t.Run(tc.name+"_error", func(t *testing.T) {
			repo := &mockRepo{}
			tc.fail(repo)
			uc := newTestUC(repo, nil, nil)
			assert.Error(t, tc.call(uc))
		})
	}
}

func TestKill(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		var gotSignal string
		repo := &mockRepo{KillFunc: func(ctx context.Context, name, signal string) error {
			gotSignal = signal
			return nil
		}}
		uc := newTestUC(repo, nil, nil)
		require.NoError(t, uc.Kill(context.Background(), "app1", "SIGTERM"))
		assert.Equal(t, "SIGTERM", gotSignal)
	})
	t.Run("error", func(t *testing.T) {
		repo := &mockRepo{KillFunc: func(ctx context.Context, name, signal string) error { return errors.New("x") }}
		uc := newTestUC(repo, nil, nil)
		assert.Error(t, uc.Kill(context.Background(), "app1", ""))
	})
}

func TestState(t *testing.T) {
	repo := &mockRepo{StateFunc: func(ctx context.Context, name string) (*domain.ContainerState, error) {
		return &domain.ContainerState{Status: "running", Running: true}, nil
	}}
	uc := newTestUC(repo, nil, nil)
	st, err := uc.State(context.Background(), "app1")
	require.NoError(t, err)
	assert.True(t, st.Running)

	repo.StateFunc = func(ctx context.Context, name string) (*domain.ContainerState, error) {
		return nil, errors.New("not found")
	}
	_, err = uc.State(context.Background(), "app1")
	assert.Error(t, err)
}

func TestLogs(t *testing.T) {
	repo := &mockRepo{LogsFunc: func(ctx context.Context, name string, tail int, ts bool) (string, error) {
		return "log line", nil
	}}
	uc := newTestUC(repo, nil, nil)
	out, err := uc.Logs(context.Background(), "app1", 10, true)
	require.NoError(t, err)
	assert.Equal(t, "log line", out)

	repo.LogsFunc = func(ctx context.Context, name string, tail int, ts bool) (string, error) {
		return "", errors.New("x")
	}
	_, err = uc.Logs(context.Background(), "app1", 10, true)
	assert.Error(t, err)
}

func TestReset_NamedVolume(t *testing.T) {
	repo := &mockRepo{
		VolumePathFunc: func(ctx context.Context, name string) (string, error) { return "user1_data", nil },
	}
	uc := newTestUC(repo, nil, nil)
	require.NoError(t, uc.Reset(context.Background(), "app1"))
}

func TestReset_NamedVolume_MountpointError(t *testing.T) {
	repo := &mockRepo{
		VolumePathFunc:       func(ctx context.Context, name string) (string, error) { return "user1_data", nil },
		VolumeMountpointFunc: func(ctx context.Context, name string) (string, error) { return "", errors.New("x") },
	}
	uc := newTestUC(repo, nil, nil)
	assert.Error(t, uc.Reset(context.Background(), "app1"))
}

func TestReset_NamedVolume_WipesContentsKeepsVolume(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644))
	require.NoError(t, os.Mkdir(filepath.Join(dir, "sub"), 0o755))
	deleted := false
	repo := &mockRepo{
		VolumePathFunc:       func(ctx context.Context, name string) (string, error) { return "user1_data", nil },
		VolumeMountpointFunc: func(ctx context.Context, name string) (string, error) { return dir, nil },
		DeleteVolumeFunc:     func(ctx context.Context, name string) error { deleted = true; return nil },
	}
	uc := newTestUC(repo, nil, nil)
	require.NoError(t, uc.Reset(context.Background(), "app1"))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
	assert.False(t, deleted)
}

func TestReset_NamedVolume_RestartError(t *testing.T) {
	repo := &mockRepo{
		VolumePathFunc: func(ctx context.Context, name string) (string, error) { return "user1_data", nil },
		RestartFunc:    func(ctx context.Context, name string) error { return errors.New("x") },
	}
	uc := newTestUC(repo, nil, nil)
	assert.Error(t, uc.Reset(context.Background(), "app1"))
}

func TestReset_HostVolume(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("x"), 0644))

	repo := &mockRepo{
		VolumePathFunc: func(ctx context.Context, name string) (string, error) { return dir, nil },
	}
	uc := newTestUC(repo, nil, nil)
	require.NoError(t, uc.Reset(context.Background(), "app1"))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestReset_VolumePathError(t *testing.T) {
	repo := &mockRepo{
		VolumePathFunc: func(ctx context.Context, name string) (string, error) { return "", errors.New("not found") },
	}
	uc := newTestUC(repo, nil, nil)
	assert.Error(t, uc.Reset(context.Background(), "app1"))
}

func TestReset_HostVolume_RestartError(t *testing.T) {
	dir := t.TempDir()
	repo := &mockRepo{
		VolumePathFunc: func(ctx context.Context, name string) (string, error) { return dir, nil },
		RestartFunc:    func(ctx context.Context, name string) error { return errors.New("x") },
	}
	uc := newTestUC(repo, nil, nil)
	assert.Error(t, uc.Reset(context.Background(), "app1"))
}

func TestList(t *testing.T) {
	repo := &mockRepo{ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
		assert.True(t, all)
		return []domain.ContainerSummary{{ID: "1"}}, nil
	}}
	uc := newTestUC(repo, nil, nil)
	out, err := uc.List(context.Background())
	require.NoError(t, err)
	assert.Len(t, out, 1)

	repo.ListFunc = func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
		return nil, errors.New("x")
	}
	_, err = uc.List(context.Background())
	assert.Error(t, err)
}

func TestIP(t *testing.T) {
	repo := &mockRepo{IPFunc: func(ctx context.Context, name string) (string, error) { return "10.0.0.5", nil }}
	uc := newTestUC(repo, nil, nil)
	ip, err := uc.IP(context.Background(), "app1")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.5", ip)

	repo.IPFunc = func(ctx context.Context, name string) (string, error) { return "", errors.New("x") }
	_, err = uc.IP(context.Background(), "app1")
	assert.Error(t, err)
}

func TestStats(t *testing.T) {
	repo := &mockRepo{StatsFunc: func(ctx context.Context, name string) (*domain.Stats, error) {
		return &domain.Stats{CPUPercent: 5.5}, nil
	}}
	uc := newTestUC(repo, nil, nil)
	s, err := uc.Stats(context.Background(), "app1")
	require.NoError(t, err)
	assert.Equal(t, 5.5, s.CPUPercent)

	repo.StatsFunc = func(ctx context.Context, name string) (*domain.Stats, error) { return nil, errors.New("x") }
	_, err = uc.Stats(context.Background(), "app1")
	assert.Error(t, err)
}

func TestHealth(t *testing.T) {
	sys := &mockSystemMetrics{
		CPUPercentFunc: func() (float64, error) { return 12.3, nil },
		MemAvailMBFunc: func() (uint64, error) { return 2048, nil },
	}
	cfg := &config.Config{NodeID: "node-9"}
	uc := newTestUC(nil, sys, cfg)
	h, err := uc.Health(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "node-9", h.NodeID)
	assert.Equal(t, 12.3, h.CPUPercent)
	assert.Equal(t, uint64(2048), h.MemFreeMB)
	assert.Equal(t, "online", h.Status)
}

func TestHealth_MetricsErrors(t *testing.T) {
	sys := &mockSystemMetrics{
		CPUPercentFunc: func() (float64, error) { return 0, errors.New("x") },
		MemAvailMBFunc: func() (uint64, error) { return 0, errors.New("x") },
	}
	uc := newTestUC(nil, sys, nil)
	h, err := uc.Health(context.Background())
	require.NoError(t, err) // errors are logged, not propagated
	assert.Equal(t, "online", h.Status)
}

func TestNetworkLifecycle(t *testing.T) {
	var created, deleted string
	repo := &mockRepo{
		CreateNetworkFunc: func(ctx context.Context, name string) error { created = name; return nil },
		DeleteNetworkFunc: func(ctx context.Context, name string) error { deleted = name; return nil },
		ListNetworksFunc: func(ctx context.Context) ([]string, error) {
			return []string{"net1", "net2"}, nil
		},
	}
	uc := newTestUC(repo, nil, nil)

	require.NoError(t, uc.CreateNetwork(context.Background(), "net1"))
	assert.Equal(t, "net1", created)

	require.NoError(t, uc.DeleteNetwork(context.Background(), "net1"))
	assert.Equal(t, "net1", deleted)

	list, err := uc.ListNetworks(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"net1", "net2"}, list)
}

func TestListNetworks_Error(t *testing.T) {
	repo := &mockRepo{ListNetworksFunc: func(ctx context.Context) ([]string, error) {
		return nil, errors.New("x")
	}}
	uc := newTestUC(repo, nil, nil)
	_, err := uc.ListNetworks(context.Background())
	assert.Error(t, err)
}

func TestVolumeLifecycle(t *testing.T) {
	var created, deleted string
	repo := &mockRepo{
		CreateVolumeFunc: func(ctx context.Context, name string) error { created = name; return nil },
		DeleteVolumeFunc: func(ctx context.Context, name string) error { deleted = name; return nil },
		ListVolumesFunc: func(ctx context.Context) ([]string, error) {
			return []string{"vol1"}, nil
		},
	}
	uc := newTestUC(repo, nil, nil)

	require.NoError(t, uc.CreateVolume(context.Background(), "vol1"))
	assert.Equal(t, "vol1", created)

	require.NoError(t, uc.DeleteVolume(context.Background(), "vol1"))
	assert.Equal(t, "vol1", deleted)

	list, err := uc.ListVolumes(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"vol1"}, list)
}

func TestListVolumes_Error(t *testing.T) {
	repo := &mockRepo{ListVolumesFunc: func(ctx context.Context) ([]string, error) {
		return nil, errors.New("x")
	}}
	uc := newTestUC(repo, nil, nil)
	_, err := uc.ListVolumes(context.Background())
	assert.Error(t, err)
}

func TestCanAccept(t *testing.T) {
	cfg := &config.Config{MaxMemoryMB: 512, MaxCPUUnits: 1}

	t.Run("within quota", func(t *testing.T) {
		repo := &mockRepo{
			ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
				return []domain.ContainerSummary{{ID: "a"}}, nil
			},
			ResourcesFunc: func(ctx context.Context, id string) (domain.ContainerResources, error) {
				return domain.ContainerResources{MemoryBytes: 100 * 1024 * 1024, NanoCPUs: 200_000_000}, nil
			},
		}
		uc := newTestUC(repo, nil, cfg)
		ok, reason, err := uc.canAccept(context.Background(), "new", 100*1024*1024, 0.2)
		require.NoError(t, err)
		assert.True(t, ok)
		assert.Empty(t, reason)
	})

	t.Run("memory exceeded", func(t *testing.T) {
		uc := newTestUC(&mockRepo{}, nil, cfg)
		ok, reason, err := uc.canAccept(context.Background(), "new", 1024*1024*1024, 0.1)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Contains(t, reason, "memory quota exceeded")
	})

	t.Run("cpu exceeded", func(t *testing.T) {
		uc := newTestUC(&mockRepo{}, nil, cfg)
		ok, reason, err := uc.canAccept(context.Background(), "new", 1024*1024, 2)
		require.NoError(t, err)
		assert.False(t, ok)
		assert.Contains(t, reason, "CPU quota exceeded")
	})

	t.Run("list error", func(t *testing.T) {
		repo := &mockRepo{ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
			return nil, errors.New("x")
		}}
		uc := newTestUC(repo, nil, cfg)
		_, _, err := uc.canAccept(context.Background(), "new", 1, 0.1)
		assert.Error(t, err)
	})

	t.Run("resources inspect error is skipped, not fatal", func(t *testing.T) {
		repo := &mockRepo{
			ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
				return []domain.ContainerSummary{{ID: "a"}}, nil
			},
			ResourcesFunc: func(ctx context.Context, id string) (domain.ContainerResources, error) {
				return domain.ContainerResources{}, errors.New("gone")
			},
		}
		uc := newTestUC(repo, nil, cfg)
		ok, _, err := uc.canAccept(context.Background(), "new", 1024*1024, 0.1)
		require.NoError(t, err)
		assert.True(t, ok)
	})

	t.Run("reservation counted, self excluded", func(t *testing.T) {
		uc := newTestUC(&mockRepo{}, nil, cfg)
		uc.reservations["other"] = reservation{memory: 600 * 1024 * 1024}
		uc.reservations["new"] = reservation{memory: 600 * 1024 * 1024}
		ok, _, err := uc.canAccept(context.Background(), "new", 1024*1024, 0.1)
		require.NoError(t, err)
		assert.False(t, ok) // "other"'s reservation alone should exceed 512MB quota; "new" is self-excluded
	})
}

func TestCheckPortConflicts(t *testing.T) {
	t.Run("no ports requested", func(t *testing.T) {
		uc := newTestUC(&mockRepo{}, nil, nil)
		err := uc.checkPortConflicts(context.Background(), "self", nil)
		assert.NoError(t, err)
	})

	t.Run("conflict with running container", func(t *testing.T) {
		repo := &mockRepo{ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
			return []domain.ContainerSummary{
				{Names: []string{"/other"}, Ports: []domain.PortMapping{{HostPort: 8080}}},
			}, nil
		}}
		uc := newTestUC(repo, nil, nil)
		err := uc.checkPortConflicts(context.Background(), "self", []domain.PortMapping{{HostPort: 8080}})
		assert.Error(t, err)
	})

	t.Run("self excluded from conflict check", func(t *testing.T) {
		repo := &mockRepo{ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
			return []domain.ContainerSummary{
				{Names: []string{"/self"}, Ports: []domain.PortMapping{{HostPort: 8080}}},
			}, nil
		}}
		uc := newTestUC(repo, nil, nil)
		err := uc.checkPortConflicts(context.Background(), "self", []domain.PortMapping{{HostPort: 8080}})
		assert.NoError(t, err)
	})

	t.Run("list error", func(t *testing.T) {
		repo := &mockRepo{ListFunc: func(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
			return nil, errors.New("x")
		}}
		uc := newTestUC(repo, nil, nil)
		err := uc.checkPortConflicts(context.Background(), "self", []domain.PortMapping{{HostPort: 8080}})
		assert.Error(t, err)
	})

	t.Run("conflict with in-flight reservation", func(t *testing.T) {
		uc := newTestUC(&mockRepo{}, nil, nil)
		uc.reservations["other"] = reservation{ports: map[int]bool{9090: true}}
		err := uc.checkPortConflicts(context.Background(), "self", []domain.PortMapping{{HostPort: 9090}})
		assert.Error(t, err)
	})

	t.Run("self reservation excluded", func(t *testing.T) {
		uc := newTestUC(&mockRepo{}, nil, nil)
		uc.reservations["self"] = reservation{ports: map[int]bool{9090: true}}
		err := uc.checkPortConflicts(context.Background(), "self", []domain.PortMapping{{HostPort: 9090}})
		assert.NoError(t, err)
	})
}

func TestContainsName(t *testing.T) {
	assert.True(t, containsName([]string{"/foo", "/bar"}, "foo"))
	assert.False(t, containsName([]string{"/foo"}, "baz"))
}

func TestPathWithinBase(t *testing.T) {
	assert.True(t, pathWithinBase("/data", "/data"))
	assert.True(t, pathWithinBase("/data/sub", "/data"))
	// Sibling directory sharing base's name as a prefix must NOT count as
	// "within" — this is the exact bug pathWithinBase exists to fix.
	assert.False(t, pathWithinBase("/data-evil", "/data"))
	assert.False(t, pathWithinBase("/data-evil/secret", "/data"))
	assert.False(t, pathWithinBase("/other", "/data"))
}

func TestScrubEnv(t *testing.T) {
	in := map[string]string{
		"NORMAL_VAR":        "ok",
		"DOCKER_HOST":       "tcp://evil",
		"AGENT_KEY":         "secret",
		"AGENT_BOX_API_KEY": "secret2",
		"agent_box_custom":  "blocked-by-prefix",
	}
	out := scrubEnv(in)
	assert.Equal(t, map[string]string{"NORMAL_VAR": "ok"}, out)
}

func TestPrepareVolume(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "dir")
	require.NoError(t, prepareVolume(dir))
	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestPrepareVolume_Error(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "file")
	require.NoError(t, os.WriteFile(blocker, []byte("x"), 0644))
	err := prepareVolume(filepath.Join(blocker, "child"))
	assert.Error(t, err)
}

func TestImageOps(t *testing.T) {
	repo := &mockRepo{
		ImageExistsFunc: func(ctx context.Context, image string) (bool, error) { return true, nil },
		PullImageFunc:   func(ctx context.Context, image string) error { return nil },
		ListImagesFunc: func(ctx context.Context) ([]domain.ImageSummary, error) {
			return []domain.ImageSummary{{ID: "sha256:abc", RepoTags: []string{"nginx:latest"}}}, nil
		},
	}
	uc := newTestUC(repo, nil, nil)

	exists, err := uc.ImageExists(context.Background(), "nginx")
	require.NoError(t, err)
	assert.True(t, exists)

	require.NoError(t, uc.PullImage(context.Background(), "nginx"))

	imgs, err := uc.ListImages(context.Background())
	require.NoError(t, err)
	assert.Len(t, imgs, 1)
}

func TestNewContainerUseCase(t *testing.T) {
	uc := NewContainerUseCase(&mockRepo{}, &mockSystemMetrics{}, &config.Config{}, testLogger())
	assert.NotNil(t, uc)
	assert.NotNil(t, uc.reservations)
}

func TestImageRepo(t *testing.T) {
	assert.Equal(t, "ghcr.io/a/b", imageRepo("ghcr.io/a/b:v1"))
	assert.Equal(t, "ghcr.io/a/b", imageRepo("ghcr.io/a/b"))
	assert.Equal(t, "ghcr.io/a/b", imageRepo("ghcr.io/a/b:v1@sha256:abc"))
	assert.Equal(t, "localhost:5000/app", imageRepo("localhost:5000/app:v2"))
	assert.Equal(t, "localhost:5000/app", imageRepo("localhost:5000/app"))
}

func TestRedeployImage(t *testing.T) {
	newRepo := func() (*mockRepo, *[]string) {
		var calls []string
		return &mockRepo{
			ContainerImageFunc: func(ctx context.Context, name string) (string, error) { return "ghcr.io/a/b:v1", nil },
			PullImageFunc:      func(ctx context.Context, image string) error { calls = append(calls, "pull"); return nil },
			RedeployFunc: func(ctx context.Context, name, image string) (string, error) {
				calls = append(calls, "redeploy:"+image)
				return "newid", nil
			},
		}, &calls
	}

	t.Run("pulls then redeploys", func(t *testing.T) {
		repo, calls := newRepo()
		res, err := newTestUC(repo, nil, nil).RedeployImage(context.Background(), "app", "ghcr.io/a/b:v2")
		require.NoError(t, err)
		assert.Equal(t, "newid", res.ContainerID)
		assert.Equal(t, []string{"pull", "redeploy:ghcr.io/a/b:v2"}, *calls)
	})

	t.Run("rejects different repository", func(t *testing.T) {
		repo, calls := newRepo()
		_, err := newTestUC(repo, nil, nil).RedeployImage(context.Background(), "app", "evil/miner:latest")
		assert.ErrorIs(t, err, domain.ErrInvalidInput)
		assert.Empty(t, *calls)
	})

	t.Run("rejects empty and whitespace image", func(t *testing.T) {
		repo, _ := newRepo()
		uc := newTestUC(repo, nil, nil)
		_, err := uc.RedeployImage(context.Background(), "app", "")
		assert.ErrorIs(t, err, domain.ErrInvalidInput)
		_, err = uc.RedeployImage(context.Background(), "app", "ghcr.io/a/b:v2 --x")
		assert.ErrorIs(t, err, domain.ErrInvalidInput)
	})

	t.Run("pull failure leaves container alone", func(t *testing.T) {
		repo, calls := newRepo()
		repo.PullImageFunc = func(ctx context.Context, image string) error { return errors.New("denied") }
		_, err := newTestUC(repo, nil, nil).RedeployImage(context.Background(), "app", "ghcr.io/a/b:v2")
		assert.ErrorIs(t, err, domain.ErrInvalidInput)
		assert.Empty(t, *calls)
	})

	t.Run("concurrent redeploy conflicts", func(t *testing.T) {
		repo, _ := newRepo()
		started, release := make(chan struct{}), make(chan struct{})
		repo.PullImageFunc = func(ctx context.Context, image string) error {
			close(started)
			<-release
			return nil
		}
		uc := newTestUC(repo, nil, nil)
		done := make(chan error)
		go func() {
			_, err := uc.RedeployImage(context.Background(), "app", "ghcr.io/a/b:v2")
			done <- err
		}()
		<-started
		_, err := uc.RedeployImage(context.Background(), "app", "ghcr.io/a/b:v3")
		assert.ErrorIs(t, err, domain.ErrConflict)
		close(release)
		assert.NoError(t, <-done)
	})
}
