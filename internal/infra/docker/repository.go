package docker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"io"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/distribution/reference"
	dockertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/registry"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/jsonmessage"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	units "github.com/docker/go-units"

	"github.com/fitraditya/litepod/internal/domain"
	"github.com/fitraditya/litepod/pkg/logger"
)

const timeout = 30 * time.Second

// Repository implements domain.ContainerRepo using the Docker API.
type Repository struct {
	cli *client.Client
	log *logger.Logger

	registryAuth map[string]RegistryCredential
}

func NewRepository(cli *client.Client, log *logger.Logger) *Repository {
	return &Repository{cli: cli, log: log}
}

// RegistryCredential is a username/password (or token) for one registry host.
type RegistryCredential struct {
	Username string
	Password string
}

// WithRegistryAuth sets per-registry pull credentials, keyed by normalized
// registry host ("docker.io", "ghcr.io", "host:5000"). Images from hosts not
// in the map are pulled anonymously.
func (r *Repository) WithRegistryAuth(creds map[string]RegistryCredential) *Repository {
	r.registryAuth = creds
	return r
}

// pullAuth returns the base64 X-Registry-Auth value for imageName's registry,
// or "" if no credentials are configured for it. The Docker daemon doesn't
// read ~/.docker/config.json — credentials must accompany each pull request.
func (r *Repository) pullAuth(imageName string) (string, error) {
	if len(r.registryAuth) == 0 {
		return "", nil
	}
	ref, err := reference.ParseNormalizedNamed(imageName)
	if err != nil {
		return "", fmt.Errorf("%w: invalid image reference %q: %v", domain.ErrInvalidInput, imageName, err)
	}
	host := reference.Domain(ref)
	c, ok := r.registryAuth[host]
	if !ok {
		return "", nil
	}
	enc, err := registry.EncodeAuthConfig(registry.AuthConfig{
		Username:      c.Username,
		Password:      c.Password,
		ServerAddress: host,
	})
	if err != nil {
		return "", fmt.Errorf("encode registry auth for %s: %w", host, err)
	}
	return enc, nil
}

func (r *Repository) Run(ctx context.Context, spec domain.DeploySpec) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cfg := &dockertypes.Config{
		Image: spec.Image,
	}
	if len(spec.Env) > 0 {
		cfg.Env = spec.Env
	}
	if len(spec.Command) > 0 {
		cfg.Cmd = spec.Command
	}
	if len(spec.Entrypoint) > 0 {
		cfg.Entrypoint = spec.Entrypoint
	}
	if spec.Healthcheck != nil {
		cfg.Healthcheck = &dockertypes.HealthConfig{
			Test:     spec.Healthcheck.Test,
			Interval: time.Duration(spec.Healthcheck.IntervalSeconds) * time.Second,
			Timeout:  time.Duration(spec.Healthcheck.TimeoutSeconds) * time.Second,
			Retries:  spec.Healthcheck.Retries,
		}
	}
	cfg.User = spec.User
	cfg.WorkingDir = spec.WorkingDir
	if len(spec.Labels) > 0 {
		cfg.Labels = spec.Labels
	}
	cfg.StopSignal = spec.StopSignal
	if spec.StopGracePeriod > 0 {
		cfg.StopTimeout = &spec.StopGracePeriod
	}

	hostCfg := &dockertypes.HostConfig{
		Resources: dockertypes.Resources{
			Memory:            spec.MemoryLimit,
			MemoryReservation: spec.SoftLimit,
			NanoCPUs:          spec.CPUNano,
		},
		RestartPolicy:  dockertypes.RestartPolicy{Name: dockertypes.RestartPolicyMode(spec.RestartPolicy)},
		ReadonlyRootfs: spec.ReadOnly,
		ShmSize:        spec.ShmSize,
		CapDrop:        spec.CapDrop,
		SecurityOpt:    spec.SecurityOpt,
	}
	if spec.PidsLimit != 0 {
		hostCfg.Resources.PidsLimit = &spec.PidsLimit
	}
	if len(spec.Ulimits) > 0 {
		ulimits := make([]*units.Ulimit, len(spec.Ulimits))
		for i, u := range spec.Ulimits {
			ulimits[i] = &units.Ulimit{Name: u.Name, Soft: u.Soft, Hard: u.Hard}
		}
		hostCfg.Resources.Ulimits = ulimits
	}
	if len(spec.DNS) > 0 {
		hostCfg.DNS = spec.DNS
	}
	if len(spec.DNSSearch) > 0 {
		hostCfg.DNSSearch = spec.DNSSearch
	}
	if len(spec.ExtraHosts) > 0 {
		hostCfg.ExtraHosts = spec.ExtraHosts
	}
	if spec.Logging != nil {
		hostCfg.LogConfig = dockertypes.LogConfig{
			Type:   spec.Logging.Driver,
			Config: spec.Logging.Options,
		}
	}
	if len(spec.Tmpfs) > 0 {
		hostCfg.Tmpfs = spec.Tmpfs
	}
	if len(spec.Sysctls) > 0 {
		hostCfg.Sysctls = spec.Sysctls
	}

	var binds []string
	for _, v := range spec.Volumes {
		mode := ""
		if v.ReadOnly {
			mode = ":ro"
		}
		source := v.VolumeName
		if v.Type == "host" {
			source = v.HostPath
		}
		binds = append(binds, fmt.Sprintf("%s:%s%s", source, v.BindPath, mode))
	}
	if len(binds) > 0 {
		hostCfg.Binds = binds
	}

	// Handle Port Mappings
	if len(spec.Ports) > 0 {
		portExposed := make(nat.PortSet)
		portBindings := make(nat.PortMap)
		for _, p := range spec.Ports {
			proto := p.Protocol
			if proto == "" {
				proto = "tcp"
			}
			dockerPort, err := nat.NewPort(proto, fmt.Sprintf("%d", p.ContainerPort))
			if err != nil {
				return "", fmt.Errorf("invalid container port %d/%s: %w", p.ContainerPort, proto, err)
			}
			portExposed[dockerPort] = struct{}{}

			if p.HostPort > 0 {
				portBindings[dockerPort] = []nat.PortBinding{
					{HostPort: fmt.Sprintf("%d", p.HostPort)},
				}
			}
		}
		cfg.ExposedPorts = portExposed
		hostCfg.PortBindings = portBindings
	}

	// Handle Networking
	var endpoints map[string]*network.EndpointSettings
	if len(spec.Networks) > 0 {
		endpoints = make(map[string]*network.EndpointSettings)
		for _, netName := range spec.Networks {
			endpoints[netName] = &network.EndpointSettings{}
		}
	}
	networkingCfg := &network.NetworkingConfig{
		EndpointsConfig: endpoints,
	}

	resp, err := r.cli.ContainerCreate(ctx, cfg, hostCfg, networkingCfg, nil, spec.Name)
	if err != nil {
		if cerrdefs.IsNotFound(err) {
			// Missing image (or network) is a caller mistake, not a node fault.
			return "", fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
		}
		return "", fmt.Errorf("create container: %w", err)
	}

	if err := r.cli.ContainerStart(ctx, resp.ID, dockertypes.StartOptions{}); err != nil {
		return "", fmt.Errorf("start container: %w", err)
	}

	return resp.ID, nil
}

func (r *Repository) Stop(ctx context.Context, name string, stopTimeout int) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.cli.ContainerStop(ctx, name, dockertypes.StopOptions{Timeout: &stopTimeout})
}

func (r *Repository) Start(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.cli.ContainerStart(ctx, name, dockertypes.StartOptions{})
}

func (r *Repository) Remove(ctx context.Context, name string, force, removeVols bool) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.cli.ContainerRemove(ctx, name, dockertypes.RemoveOptions{Force: force, RemoveVolumes: removeVols})
}

func (r *Repository) Restart(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.cli.ContainerRestart(ctx, name, dockertypes.StopOptions{})
}

func (r *Repository) Pause(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.cli.ContainerPause(ctx, name)
}

func (r *Repository) Unpause(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.cli.ContainerUnpause(ctx, name)
}

// Kill sends signal to the container's main process without waiting for it
// to exit. An empty signal defaults to SIGKILL (Docker's default).
func (r *Repository) Kill(ctx context.Context, name string, signal string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return r.cli.ContainerKill(ctx, name, signal)
}

func (r *Repository) List(ctx context.Context, all bool) ([]domain.ContainerSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	list, err := r.cli.ContainerList(ctx, dockertypes.ListOptions{All: all})
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}

	result := make([]domain.ContainerSummary, 0, len(list))
	for _, c := range list {
		ports := make([]domain.PortMapping, 0, len(c.Ports))
		for _, p := range c.Ports {
			ports = append(ports, domain.PortMapping{
				HostPort:      int(p.PublicPort),
				ContainerPort: int(p.PrivatePort),
				Protocol:      p.Type,
			})
		}
		result = append(result, domain.ContainerSummary{
			ID:     c.ID,
			Names:  c.Names,
			Image:  c.Image,
			State:  c.State,
			Status: c.Status,
			Ports:  ports,
		})
	}
	return result, nil
}

// ImageExists checks if the specified image exists locally.
func (r *Repository) ImageExists(ctx context.Context, imageName string) (bool, error) {
	_, _, err := r.cli.ImageInspectWithRaw(ctx, imageName)
	if err != nil {
		if client.IsErrNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// PullImage pulls an image and blocks until the pull completes. The Docker
// pull API streams progress as newline-delimited JSON and returns a nil error
// as soon as the request is accepted — actual failures (bad tag, rate limit,
// auth) show up as an "error" field inside the stream, not as a Go error, so
// the stream must be decoded and inspected rather than just drained.
func (r *Repository) PullImage(ctx context.Context, imageName string) error {
	auth, err := r.pullAuth(imageName)
	if err != nil {
		return err
	}
	out, err := r.cli.ImagePull(ctx, imageName, image.PullOptions{RegistryAuth: auth})
	if err != nil {
		return fmt.Errorf("pull image %s: %w", imageName, err)
	}
	defer out.Close()

	dec := json.NewDecoder(out)
	for {
		var msg jsonmessage.JSONMessage
		if err := dec.Decode(&msg); err != nil {
			if err == io.EOF {
				break
			}
			return fmt.Errorf("decode pull output for %s: %w", imageName, err)
		}
		if msg.Error != nil {
			return fmt.Errorf("pull image %s: %s", imageName, msg.Error.Message)
		}
	}
	return nil
}

// ListImages returns all images cached locally on this node.
func (r *Repository) ListImages(ctx context.Context) ([]domain.ImageSummary, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	images, err := r.cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list images: %w", err)
	}

	result := make([]domain.ImageSummary, 0, len(images))
	for _, img := range images {
		result = append(result, domain.ImageSummary{
			ID:       img.ID,
			RepoTags: img.RepoTags,
			SizeMB:   img.Size / (1024 * 1024),
		})
	}
	return result, nil
}

func (r *Repository) Resources(ctx context.Context, id string) (domain.ContainerResources, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ins, err := r.cli.ContainerInspect(ctx, id)
	if err != nil {
		return domain.ContainerResources{}, fmt.Errorf("inspect container %q: %w", id, err)
	}
	return domain.ContainerResources{
		MemoryBytes: ins.HostConfig.Resources.Memory,
		NanoCPUs:    ins.HostConfig.Resources.NanoCPUs,
	}, nil
}

func (r *Repository) Stats(ctx context.Context, name string) (*domain.Stats, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	stats, err := r.cli.ContainerStatsOneShot(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get container stats: %w", err)
	}
	defer stats.Body.Close()

	var raw dockertypes.StatsResponse
	if err := json.NewDecoder(stats.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode stats: %w", err)
	}

	cpuDelta := float64(raw.CPUStats.CPUUsage.TotalUsage) - float64(raw.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(raw.CPUStats.SystemUsage) - float64(raw.PreCPUStats.SystemUsage)

	numCPUs := int(raw.CPUStats.OnlineCPUs)
	if numCPUs <= 0 {
		numCPUs = 1
	}

	var rxBytes, txBytes uint64
	for _, n := range raw.Networks {
		rxBytes += n.RxBytes
		txBytes += n.TxBytes
	}

	if sysDelta > 0 && cpuDelta > 0 {
		return &domain.Stats{
			CPUPercent:     (cpuDelta / sysDelta) * float64(numCPUs) * 100.0,
			MemUsage:       raw.MemoryStats.Usage - raw.MemoryStats.Stats["inactive_file"],
			MemLimit:       raw.MemoryStats.Limit,
			NetworkRxBytes: rxBytes,
			NetworkTxBytes: txBytes,
		}, nil
	}

	return &domain.Stats{
		CPUPercent:     0.01,
		MemUsage:       raw.MemoryStats.Usage,
		MemLimit:       raw.MemoryStats.Limit,
		NetworkRxBytes: rxBytes,
		NetworkTxBytes: txBytes,
	}, nil
}

func (r *Repository) State(ctx context.Context, name string) (*domain.ContainerState, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ins, err := r.cli.ContainerInspect(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("inspect container %q: %w", name, err)
	}

	state := &domain.ContainerState{
		Status:     ins.State.Status,
		Running:    ins.State.Running,
		Paused:     ins.State.Paused,
		Restarting: ins.State.Restarting,
		OOMKilled:  ins.State.OOMKilled,
		Dead:       ins.State.Dead,
		Pid:        ins.State.Pid,
		ExitCode:   ins.State.ExitCode,
		Error:      ins.State.Error,
		StartedAt:  ins.State.StartedAt,
		FinishedAt: ins.State.FinishedAt,
	}

	if ins.State.Health != nil {
		state.Health = &domain.Health{
			Status:        ins.State.Health.Status,
			FailingStreak: ins.State.Health.FailingStreak,
		}
	}

	return state, nil
}

func (r *Repository) VolumePath(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ins, err := r.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", fmt.Errorf("inspect container %q: %w", name, err)
	}
	if len(ins.HostConfig.Binds) == 0 {
		return "", domain.ErrContainerNotFound
	}
	// Bind format: "/host/path:/container/path[:options]"
	parts := strings.SplitN(ins.HostConfig.Binds[0], ":", 2)
	if len(parts) < 1 {
		return "", fmt.Errorf("invalid bind format for container %q", name)
	}
	return parts[0], nil
}

// ContainerImage returns the image reference the container was created from.
func (r *Repository) ContainerImage(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ins, err := r.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", fmt.Errorf("inspect container %q: %w", name, err)
	}
	return ins.Config.Image, nil
}

// Redeploy recreates the container from its inspected config with newImage.
// The old container is stopped and renamed aside (not removed) until the new
// one has started, so a failed create/start rolls back to the old container.
// A container that was stopped stays stopped.
func (r *Repository) Redeploy(ctx context.Context, name, newImage string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*timeout)
	defer cancel()

	ins, err := r.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", fmt.Errorf("inspect container %q: %w", name, err)
	}

	cfg := ins.Config
	cfg.Image = newImage
	cfg.Hostname = "" // inherited hostname is the old container's short ID
	hostCfg := ins.HostConfig

	endpoints := make(map[string]*network.EndpointSettings)
	if ins.NetworkSettings != nil {
		for netName := range ins.NetworkSettings.Networks {
			endpoints[netName] = &network.EndpointSettings{}
		}
	}
	networkingCfg := &network.NetworkingConfig{EndpointsConfig: endpoints}

	wasRunning := ins.State != nil && ins.State.Running
	stopTimeout := 10
	if cfg.StopTimeout != nil {
		stopTimeout = *cfg.StopTimeout
	}
	if err := r.cli.ContainerStop(ctx, ins.ID, dockertypes.StopOptions{Timeout: &stopTimeout}); err != nil {
		return "", fmt.Errorf("stop container %q: %w", name, err)
	}

	oldName := fmt.Sprintf("%s-old-%d", name, time.Now().Unix())
	if err := r.cli.ContainerRename(ctx, ins.ID, oldName); err != nil {
		if wasRunning {
			_ = r.cli.ContainerStart(ctx, ins.ID, dockertypes.StartOptions{})
		}
		return "", fmt.Errorf("rename container %q: %w", name, err)
	}

	rollback := func() {
		_ = r.cli.ContainerRename(ctx, ins.ID, name)
		if wasRunning {
			_ = r.cli.ContainerStart(ctx, ins.ID, dockertypes.StartOptions{})
		}
	}

	resp, err := r.cli.ContainerCreate(ctx, cfg, hostCfg, networkingCfg, nil, name)
	if err != nil {
		rollback()
		if cerrdefs.IsNotFound(err) {
			return "", fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
		}
		return "", fmt.Errorf("create container: %w", err)
	}
	if wasRunning {
		if err := r.cli.ContainerStart(ctx, resp.ID, dockertypes.StartOptions{}); err != nil {
			_ = r.cli.ContainerRemove(ctx, resp.ID, dockertypes.RemoveOptions{Force: true})
			rollback()
			return "", fmt.Errorf("start container: %w", err)
		}
	}

	// Volumes are kept (removeVols=false); the new container uses them.
	_ = r.cli.ContainerRemove(ctx, ins.ID, dockertypes.RemoveOptions{Force: true})
	return resp.ID, nil
}

func (r *Repository) Logs(ctx context.Context, name string, tail int, timestamps bool) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	tailStr := "all"
	if tail > 0 {
		tailStr = strconv.Itoa(tail)
	}

	out, err := r.cli.ContainerLogs(ctx, name, dockertypes.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Tail:       tailStr,
		Timestamps: timestamps,
	})
	if err != nil {
		if client.IsErrNotFound(err) {
			return "", domain.ErrContainerNotFound
		}
		return "", fmt.Errorf("get container logs %q: %w", name, err)
	}
	defer out.Close()

	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, &buf, out); err != nil {
		return "", fmt.Errorf("read container logs %q: %w", name, err)
	}
	return buf.String(), nil
}

func (r *Repository) IP(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ins, err := r.cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", fmt.Errorf("inspect container %q: %w", name, err)
	}
	for _, network := range ins.NetworkSettings.Networks {
		if network.IPAddress != "" {
			return network.IPAddress, nil
		}
	}
	return "", fmt.Errorf("no IP address found for container %q", name)
}

func (r *Repository) CreateNetwork(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := r.cli.NetworkCreate(ctx, name, network.CreateOptions{
		Driver: "bridge",
	})
	if err != nil {
		return fmt.Errorf("create network %q: %w", name, err)
	}
	return nil
}

func (r *Repository) DeleteNetwork(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := r.cli.NetworkRemove(ctx, name); err != nil {
		return fmt.Errorf("delete network %q: %w", name, err)
	}
	return nil
}

func (r *Repository) ListNetworks(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	networks, err := r.cli.NetworkList(ctx, network.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}

	names := make([]string, 0, len(networks))
	for _, n := range networks {
		names = append(names, n.Name)
	}
	return names, nil
}

func (r *Repository) CreateVolume(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	_, err := r.cli.VolumeCreate(ctx, volume.CreateOptions{
		Name: name,
	})
	if err != nil {
		return fmt.Errorf("create volume %q: %w", name, err)
	}
	return nil
}

func (r *Repository) DeleteVolume(ctx context.Context, name string) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := r.cli.VolumeRemove(ctx, name, false); err != nil {
		return fmt.Errorf("delete volume %q: %w", name, err)
	}
	return nil
}

// VolumeMountpoint returns the host directory backing a named Docker volume.
func (r *Repository) VolumeMountpoint(ctx context.Context, name string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	v, err := r.cli.VolumeInspect(ctx, name)
	if err != nil {
		return "", fmt.Errorf("inspect volume %q: %w", name, err)
	}
	return v.Mountpoint, nil
}

func (r *Repository) ListVolumes(ctx context.Context) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	vols, err := r.cli.VolumeList(ctx, volume.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list volumes: %w", err)
	}

	names := make([]string, 0, len(vols.Volumes))
	for _, v := range vols.Volumes {
		names = append(names, v.Name)
	}
	return names, nil
}
