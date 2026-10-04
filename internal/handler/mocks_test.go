package handler

import (
	"context"
	"os"

	"github.com/fitraditya/litepod/internal/domain"
)

// mockRepo is a function-field fake of domain.ContainerRepo, local to the
// handler package so tests can drive a real *usecase.ContainerUseCase
// end-to-end through HTTP without a Docker daemon.
type mockRepo struct {
	RunFunc              func(ctx context.Context, spec domain.DeploySpec) (string, error)
	StopFunc             func(ctx context.Context, name string, timeout int) error
	StartFunc            func(ctx context.Context, name string) error
	RemoveFunc           func(ctx context.Context, name string, force, removeVols bool) error
	RestartFunc          func(ctx context.Context, name string) error
	PauseFunc            func(ctx context.Context, name string) error
	UnpauseFunc          func(ctx context.Context, name string) error
	KillFunc             func(ctx context.Context, name string, signal string) error
	ListFunc             func(ctx context.Context, all bool) ([]domain.ContainerSummary, error)
	ResourcesFunc        func(ctx context.Context, id string) (domain.ContainerResources, error)
	StatsFunc            func(ctx context.Context, name string) (*domain.Stats, error)
	ContainerImageFunc   func(ctx context.Context, name string) (string, error)
	RedeployFunc         func(ctx context.Context, name, image string) (string, error)
	VolumePathFunc       func(ctx context.Context, name string) (string, error)
	IPFunc               func(ctx context.Context, name string) (string, error)
	StateFunc            func(ctx context.Context, name string) (*domain.ContainerState, error)
	LogsFunc             func(ctx context.Context, name string, tail int, timestamps bool) (string, error)
	CreateNetworkFunc    func(ctx context.Context, name string) error
	DeleteNetworkFunc    func(ctx context.Context, name string) error
	ListNetworksFunc     func(ctx context.Context) ([]string, error)
	CreateVolumeFunc     func(ctx context.Context, name string) error
	DeleteVolumeFunc     func(ctx context.Context, name string) error
	VolumeMountpointFunc func(ctx context.Context, name string) (string, error)
	ListVolumesFunc      func(ctx context.Context) ([]string, error)
	ImageExistsFunc      func(ctx context.Context, image string) (bool, error)
	PullImageFunc        func(ctx context.Context, image string, auth *domain.RegistryAuth) error
	ListImagesFunc       func(ctx context.Context) ([]domain.ImageSummary, error)
}

func (m *mockRepo) Run(ctx context.Context, spec domain.DeploySpec) (string, error) {
	if m.RunFunc != nil {
		return m.RunFunc(ctx, spec)
	}
	return "container-id", nil
}

func (m *mockRepo) Stop(ctx context.Context, name string, timeout int) error {
	if m.StopFunc != nil {
		return m.StopFunc(ctx, name, timeout)
	}
	return nil
}

func (m *mockRepo) Start(ctx context.Context, name string) error {
	if m.StartFunc != nil {
		return m.StartFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) Remove(ctx context.Context, name string, force, removeVols bool) error {
	if m.RemoveFunc != nil {
		return m.RemoveFunc(ctx, name, force, removeVols)
	}
	return nil
}

func (m *mockRepo) Restart(ctx context.Context, name string) error {
	if m.RestartFunc != nil {
		return m.RestartFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) Pause(ctx context.Context, name string) error {
	if m.PauseFunc != nil {
		return m.PauseFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) Unpause(ctx context.Context, name string) error {
	if m.UnpauseFunc != nil {
		return m.UnpauseFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) Kill(ctx context.Context, name string, signal string) error {
	if m.KillFunc != nil {
		return m.KillFunc(ctx, name, signal)
	}
	return nil
}

func (m *mockRepo) List(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, all)
	}
	return nil, nil
}

func (m *mockRepo) Resources(ctx context.Context, id string) (domain.ContainerResources, error) {
	if m.ResourcesFunc != nil {
		return m.ResourcesFunc(ctx, id)
	}
	return domain.ContainerResources{}, nil
}

func (m *mockRepo) Stats(ctx context.Context, name string) (*domain.Stats, error) {
	if m.StatsFunc != nil {
		return m.StatsFunc(ctx, name)
	}
	return &domain.Stats{}, nil
}

func (m *mockRepo) VolumePath(ctx context.Context, name string) (string, error) {
	if m.VolumePathFunc != nil {
		return m.VolumePathFunc(ctx, name)
	}
	return "", nil
}

func (m *mockRepo) IP(ctx context.Context, name string) (string, error) {
	if m.IPFunc != nil {
		return m.IPFunc(ctx, name)
	}
	return "", nil
}

func (m *mockRepo) State(ctx context.Context, name string) (*domain.ContainerState, error) {
	if m.StateFunc != nil {
		return m.StateFunc(ctx, name)
	}
	return &domain.ContainerState{}, nil
}

func (m *mockRepo) Logs(ctx context.Context, name string, tail int, timestamps bool) (string, error) {
	if m.LogsFunc != nil {
		return m.LogsFunc(ctx, name, tail, timestamps)
	}
	return "", nil
}

func (m *mockRepo) CreateNetwork(ctx context.Context, name string) error {
	if m.CreateNetworkFunc != nil {
		return m.CreateNetworkFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) DeleteNetwork(ctx context.Context, name string) error {
	if m.DeleteNetworkFunc != nil {
		return m.DeleteNetworkFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) ListNetworks(ctx context.Context) ([]string, error) {
	if m.ListNetworksFunc != nil {
		return m.ListNetworksFunc(ctx)
	}
	return nil, nil
}

func (m *mockRepo) CreateVolume(ctx context.Context, name string) error {
	if m.CreateVolumeFunc != nil {
		return m.CreateVolumeFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) DeleteVolume(ctx context.Context, name string) error {
	if m.DeleteVolumeFunc != nil {
		return m.DeleteVolumeFunc(ctx, name)
	}
	return nil
}

func (m *mockRepo) ListVolumes(ctx context.Context) ([]string, error) {
	if m.ListVolumesFunc != nil {
		return m.ListVolumesFunc(ctx)
	}
	return nil, nil
}

func (m *mockRepo) ImageExists(ctx context.Context, image string) (bool, error) {
	if m.ImageExistsFunc != nil {
		return m.ImageExistsFunc(ctx, image)
	}
	return false, nil
}

func (m *mockRepo) PullImage(ctx context.Context, image string, auth *domain.RegistryAuth) error {
	if m.PullImageFunc != nil {
		return m.PullImageFunc(ctx, image, auth)
	}
	return nil
}

func (m *mockRepo) ListImages(ctx context.Context) ([]domain.ImageSummary, error) {
	if m.ListImagesFunc != nil {
		return m.ListImagesFunc(ctx)
	}
	return nil, nil
}

// mockSystemMetrics is a function-field fake of domain.SystemMetrics.
type mockSystemMetrics struct {
	CPUPercentFunc func() (float64, error)
	MemAvailMBFunc func() (uint64, error)
}

func (m *mockSystemMetrics) CPUPercent() (float64, error) {
	if m.CPUPercentFunc != nil {
		return m.CPUPercentFunc()
	}
	return 0, nil
}

func (m *mockSystemMetrics) MemAvailMB() (uint64, error) {
	if m.MemAvailMBFunc != nil {
		return m.MemAvailMBFunc()
	}
	return 0, nil
}

func (m *mockRepo) VolumeMountpoint(ctx context.Context, name string) (string, error) {
	if m.VolumeMountpointFunc != nil {
		return m.VolumeMountpointFunc(ctx, name)
	}
	// Fresh empty dir so a default Reset never touches real data.
	return os.MkdirTemp("", "mock-volume-")
}

func (m *mockRepo) ContainerImage(ctx context.Context, name string) (string, error) {
	if m.ContainerImageFunc != nil {
		return m.ContainerImageFunc(ctx, name)
	}
	return "", nil
}

func (m *mockRepo) Redeploy(ctx context.Context, name, image string) (string, error) {
	if m.RedeployFunc != nil {
		return m.RedeployFunc(ctx, name, image)
	}
	return "id", nil
}
