package domain

import "context"

// ContainerRepo is the contract for container runtime operations.
// Implemented by infra/docker, consumed by the use case layer.
type ContainerRepo interface {
	Run(ctx context.Context, spec DeploySpec) (id string, err error)
	Stop(ctx context.Context, name string, timeout int) error
	Start(ctx context.Context, name string) error
	Remove(ctx context.Context, name string, force, removeVols bool) error
	Restart(ctx context.Context, name string) error
	Pause(ctx context.Context, name string) error
	Unpause(ctx context.Context, name string) error
	Kill(ctx context.Context, name string, signal string) error
	List(ctx context.Context, all bool) ([]ContainerSummary, error)
	Resources(ctx context.Context, id string) (ContainerResources, error)
	Stats(ctx context.Context, name string) (*Stats, error)
	VolumePath(ctx context.Context, name string) (string, error)
	IP(ctx context.Context, name string) (string, error)
	State(ctx context.Context, name string) (*ContainerState, error)
	// ContainerImage returns the image reference the container was created from.
	ContainerImage(ctx context.Context, name string) (string, error)
	// Redeploy recreates the container from its current config with a new
	// image (which must already be pulled), rolling back if the new one fails
	// to start. The container's running/stopped state is preserved.
	Redeploy(ctx context.Context, name, image string) (id string, err error)
	Logs(ctx context.Context, name string, tail int, timestamps bool) (string, error)

	// Network Management
	CreateNetwork(ctx context.Context, name string) error
	DeleteNetwork(ctx context.Context, name string) error
	ListNetworks(ctx context.Context) ([]string, error)

	// Volume Management
	CreateVolume(ctx context.Context, name string) error
	DeleteVolume(ctx context.Context, name string) error
	ListVolumes(ctx context.Context) ([]string, error)
	// VolumeMountpoint returns the host directory backing a named volume.
	VolumeMountpoint(ctx context.Context, name string) (string, error)

	// Image Management
	ImageExists(ctx context.Context, image string) (bool, error)
	PullImage(ctx context.Context, image string, auth *RegistryAuth) error
	ListImages(ctx context.Context) ([]ImageSummary, error)
}

// SystemMetrics provides node-level resource readings.
// Implemented by infra/system, consumed by the use case layer.
type SystemMetrics interface {
	CPUPercent() (float64, error)
	MemAvailMB() (uint64, error)
}
