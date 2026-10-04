package usecase

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fitraditya/litepod/internal/config"
	"github.com/fitraditya/litepod/internal/domain"
	"github.com/fitraditya/litepod/pkg/logger"
)

// sensitiveEnvKeys lists env var names that must never reach containers.
var sensitiveEnvKeys = []string{"DOCKER_HOST", "AGENT_KEY", "AGENT_BOX_API_KEY"}

const (
	softLimitBytes  = int64(512 * 1024 * 1024)
	stopTimeoutSecs = 10
)

// reservation tracks the resources an in-flight Deploy/Update has been
// admitted for, before the container actually exists and shows up in
// uc.repo.List/Resources. This lets canAccept and checkPortConflicts see
// concurrent in-flight claims without requiring the lock to be held for the
// full duration of slow Docker calls.
type reservation struct {
	memory int64
	cpu    float64
	ports  map[int]bool
}

// ContainerUseCase orchestrates all container lifecycle operations.
type ContainerUseCase struct {
	repo domain.ContainerRepo
	sys  domain.SystemMetrics
	cfg  *config.Config
	log  *logger.Logger

	mu           sync.Mutex             // guards reservations and the check-then-reserve admission step below
	redeploying  map[string]bool        // containers with a webhook redeploy in flight, guarded by mu
	reservations map[string]reservation // pending allocations for in-flight deploys/updates, keyed by container name
}

func NewContainerUseCase(
	repo domain.ContainerRepo,
	sys domain.SystemMetrics,
	cfg *config.Config,
	log *logger.Logger,
) *ContainerUseCase {
	return &ContainerUseCase{repo: repo, sys: sys, cfg: cfg, log: log, reservations: make(map[string]reservation), redeploying: make(map[string]bool)}
}

// reserve records a pending allocation for name. Must be called with uc.mu held.
func (uc *ContainerUseCase) reserve(name string, mem int64, cpu float64, ports []domain.PortMapping) {
	portSet := make(map[int]bool, len(ports))
	for _, p := range ports {
		if p.HostPort > 0 {
			portSet[p.HostPort] = true
		}
	}
	uc.reservations[name] = reservation{memory: mem, cpu: cpu, ports: portSet}
}

// release clears a pending allocation once the container is running (so the
// running container itself becomes the source of truth for admission checks)
// or the deploy/update failed.
func (uc *ContainerUseCase) release(name string) {
	uc.mu.Lock()
	delete(uc.reservations, name)
	uc.mu.Unlock()
}

// Deploy validates, checks capacity, creates the volume, and starts the container.
func (uc *ContainerUseCase) Deploy(ctx context.Context, req domain.DeployRequest) (*domain.DeployResult, error) {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "deploy", "container": req.Name, "image": req.Image})

	if err := validateRequest(req, uc.cfg.DeployPortRange, uc.cfg.VolumeBase); err != nil {
		log.WithError(err).Warn("Invalid deploy request")
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err)
	}

	// Hold the lock only long enough to admit and reserve capacity/ports —
	// not across the slow Docker calls below — so concurrent deploys
	// don't serialize on network I/O. The reservation keeps canAccept and
	// checkPortConflicts consistent for other in-flight requests until this
	// container actually exists (or the attempt fails).
	if err := func() error {
		uc.mu.Lock()
		defer uc.mu.Unlock()

		ok, reason, aerr := uc.canAccept(ctx, req.Name, req.MemoryLimit, req.CPULimit)
		if aerr != nil {
			log.WithError(aerr).Error("Admission control check failed")
			return aerr
		}
		if !ok {
			log.WithField("reason", reason).Warn("Admission denied")
			return fmt.Errorf("%w: %s", domain.ErrInsufficientResources, reason)
		}
		if perr := uc.checkPortConflicts(ctx, req.Name, req.Ports); perr != nil {
			log.WithError(perr).Warn("Port conflict detected")
			return fmt.Errorf("%w: %s", domain.ErrInvalidInput, perr)
		}

		uc.reserve(req.Name, req.MemoryLimit, req.CPULimit, req.Ports)
		return nil
	}(); err != nil {
		return nil, err
	}
	defer uc.release(req.Name)

	spec := buildSpec(req, uc.cfg.VolumeBase)

	for _, v := range spec.Volumes {
		if v.Type == "host" {
			if err := prepareVolume(v.HostPath); err != nil {
				log.WithError(err).Error("Volume preparation failed")
				return nil, err
			}
		}
	}

	id, err := uc.repo.Run(ctx, spec)
	if err != nil {
		log.WithError(err).Error("Container start failed")
		return nil, err
	}

	log.WithField("container_id", id).Info("Container deployed")
	return &domain.DeployResult{ContainerID: id, NodeID: uc.cfg.NodeID}, nil
}

// Update performs an atomic stop-remove-start cycle with the new spec.
func (uc *ContainerUseCase) Update(ctx context.Context, name string, req domain.DeployRequest) (*domain.DeployResult, error) {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "update", "container": name})

	req.Name = name
	if err := validateRequest(req, uc.cfg.DeployPortRange, uc.cfg.VolumeBase); err != nil {
		log.WithError(err).Warn("Invalid update request")
		return nil, fmt.Errorf("%w: %s", domain.ErrInvalidInput, err)
	}

	// Narrow lock scope (see Deploy): only the port-conflict check + reservation
	// need uc.mu; the stop/remove/run calls below run unlocked. Memory/CPU
	// aren't reserved here because Update doesn't run admission control (the
	// replaced container's old allocation is still counted until it's removed).
	if err := func() error {
		uc.mu.Lock()
		defer uc.mu.Unlock()

		if perr := uc.checkPortConflicts(ctx, name, req.Ports); perr != nil {
			log.WithError(perr).Warn("Port conflict detected")
			return fmt.Errorf("%w: %s", domain.ErrInvalidInput, perr)
		}

		uc.reserve(name, 0, 0, req.Ports)
		return nil
	}(); err != nil {
		return nil, err
	}
	defer uc.release(name)

	t := stopTimeoutSecs
	_ = uc.repo.Stop(ctx, name, t)
	_ = uc.repo.Remove(ctx, name, true, false)

	spec := buildSpec(req, uc.cfg.VolumeBase)
	for _, v := range spec.Volumes {
		if v.Type == "host" {
			if err := prepareVolume(v.HostPath); err != nil {
				log.WithError(err).Error("Volume preparation failed")
				return nil, err
			}
		}
	}

	id, err := uc.repo.Run(ctx, spec)
	if err != nil {
		log.WithError(err).Error("Re-deploy failed")
		return nil, err
	}

	log.WithField("container_id", id).Info("Container updated")
	return &domain.DeployResult{ContainerID: id, NodeID: uc.cfg.NodeID}, nil
}

// imageRepo strips the tag and digest from an image reference, leaving the
// repository (registry host + path) — "ghcr.io/a/b:v1@sha256:x" -> "ghcr.io/a/b".
func imageRepo(ref string) string {
	if i := strings.Index(ref, "@"); i >= 0 {
		ref = ref[:i]
	}
	if i := strings.LastIndex(ref, ":"); i > strings.LastIndex(ref, "/") {
		ref = ref[:i]
	}
	return ref
}

// RedeployImage pulls image and recreates the named container from its
// current config with that image. The image must belong to the same
// repository the container already runs, so a holder of the (narrowly scoped)
// webhook credential can roll tags forward but can't swap in an arbitrary
// image. Pulling happens before anything is touched, so a failed pull leaves
// the running container alone.
func (uc *ContainerUseCase) RedeployImage(ctx context.Context, name, image string) (*domain.DeployResult, error) {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "redeploy", "container": name, "image": image})

	if image == "" || strings.ContainsAny(image, " \t\r\n") {
		return nil, fmt.Errorf("%w: invalid image reference", domain.ErrInvalidInput)
	}

	uc.mu.Lock()
	if uc.redeploying[name] {
		uc.mu.Unlock()
		return nil, fmt.Errorf("%w: redeploy already in progress for %q", domain.ErrConflict, name)
	}
	uc.redeploying[name] = true
	uc.mu.Unlock()
	defer func() {
		uc.mu.Lock()
		delete(uc.redeploying, name)
		uc.mu.Unlock()
	}()

	current, err := uc.repo.ContainerImage(ctx, name)
	if err != nil {
		log.WithError(err).Error("Inspect failed before redeploy")
		return nil, err
	}
	if imageRepo(current) != imageRepo(image) {
		log.WithField("current_image", current).Warn("Redeploy rejected: image repository differs")
		return nil, fmt.Errorf("%w: image must be from repository %q", domain.ErrInvalidInput, imageRepo(current))
	}

	if err := uc.repo.PullImage(ctx, image); err != nil {
		log.WithError(err).Error("Pull failed")
		return nil, fmt.Errorf("%w: %v", domain.ErrInvalidInput, err)
	}

	id, err := uc.repo.Redeploy(ctx, name, image)
	if err != nil {
		log.WithError(err).Error("Redeploy failed")
		return nil, err
	}

	log.WithFields(logger.Fields{"container_id": id, "previous_image": current}).Info("Container redeployed")
	return &domain.DeployResult{ContainerID: id, NodeID: uc.cfg.NodeID}, nil
}

// Destroy stops and removes a container, including its volumes.
func (uc *ContainerUseCase) Destroy(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "destroy", "container": name})

	t := stopTimeoutSecs
	_ = uc.repo.Stop(ctx, name, t)
	if err := uc.repo.Remove(ctx, name, true, true); err != nil {
		log.WithError(err).Error("Remove failed")
		return err
	}

	log.Info("Container destroyed")
	return nil
}

// Start explicitly starts a stopped container.
func (uc *ContainerUseCase) Start(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "start", "container": name})
	if err := uc.repo.Start(ctx, name); err != nil {
		log.WithError(err).Error("Start failed")
		return err
	}
	log.Info("Container started")
	return nil
}

// Stop explicitly stops a running container.
func (uc *ContainerUseCase) Stop(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "stop", "container": name})
	t := stopTimeoutSecs
	if err := uc.repo.Stop(ctx, name, t); err != nil {
		log.WithError(err).Error("Stop failed")
		return err
	}
	log.Info("Container stopped")
	return nil
}

// Restart gracefully restarts a running container.
func (uc *ContainerUseCase) Restart(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "restart", "container": name})
	if err := uc.repo.Restart(ctx, name); err != nil {
		log.WithError(err).Error("Restart failed")
		return err
	}
	log.Info("Container restarted")
	return nil
}

// Pause freezes all processes in a running container (cgroup freezer) without
// terminating them.
func (uc *ContainerUseCase) Pause(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "pause", "container": name})
	if err := uc.repo.Pause(ctx, name); err != nil {
		log.WithError(err).Error("Pause failed")
		return err
	}
	log.Info("Container paused")
	return nil
}

// Unpause resumes a paused container's frozen processes.
func (uc *ContainerUseCase) Unpause(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "unpause", "container": name})
	if err := uc.repo.Unpause(ctx, name); err != nil {
		log.WithError(err).Error("Unpause failed")
		return err
	}
	log.Info("Container unpaused")
	return nil
}

// Kill sends a signal to the container's main process immediately, with no
// grace period (unlike Stop, which sends SIGTERM and waits before SIGKILL).
func (uc *ContainerUseCase) Kill(ctx context.Context, name string, signal string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "kill", "container": name, "signal": signal})
	if err := uc.repo.Kill(ctx, name, signal); err != nil {
		log.WithError(err).Error("Kill failed")
		return err
	}
	log.Info("Container killed")
	return nil
}

// Suspend stops the container.
func (uc *ContainerUseCase) Suspend(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "suspend", "container": name})

	t := stopTimeoutSecs
	if err := uc.repo.Stop(ctx, name, t); err != nil {
		log.WithError(err).Error("Stop failed during suspend")
		return err
	}

	log.Info("Container suspended")
	return nil
}

// Unsuspend starts the container.
func (uc *ContainerUseCase) Unsuspend(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "unsuspend", "container": name})

	if err := uc.repo.Restart(ctx, name); err != nil {
		log.WithError(err).Error("Start failed during unsuspend")
		return err
	}

	log.Info("Container unsuspended")
	return nil
}

// State returns the real-time status and health of a container.
func (uc *ContainerUseCase) State(ctx context.Context, name string) (*domain.ContainerState, error) {
	state, err := uc.repo.State(ctx, name)
	if err != nil {
		uc.log.WithField("container", name).WithError(err).Error("State failed")
		return nil, err
	}
	return state, nil
}

// Logs returns recent stdout/stderr output for a container.
func (uc *ContainerUseCase) Logs(ctx context.Context, name string, tail int, timestamps bool) (string, error) {
	logs, err := uc.repo.Logs(ctx, name, tail, timestamps)
	if err != nil {
		uc.log.WithField("container", name).WithError(err).Error("Logs failed")
		return "", err
	}
	return logs, nil
}

// Reset wipes the container's volume directory then restarts it.
func (uc *ContainerUseCase) Reset(ctx context.Context, name string) error {
	log := logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "reset", "container": name})

	volPath, err := uc.repo.VolumePath(ctx, name)
	if err != nil {
		log.WithError(err).Error("Could not resolve volume path")
		return err
	}

	t := stopTimeoutSecs
	_ = uc.repo.Stop(ctx, name, t)

	if !filepath.IsAbs(volPath) {
		// volPath is a Docker named volume name, not a host directory.
		// Docker refuses to delete a volume still referenced by the (stopped)
		// container, so wipe the contents in place via the volume's mountpoint.
		mp, err := uc.repo.VolumeMountpoint(ctx, volPath)
		if err != nil {
			log.WithError(err).Error("Resolve named volume mountpoint failed")
			return fmt.Errorf("resolve volume: %w", err)
		}
		if !filepath.IsAbs(mp) {
			return fmt.Errorf("resolve volume: unexpected mountpoint %q", mp)
		}
		entries, err := os.ReadDir(mp)
		if err != nil {
			log.WithError(err).Error("Read named volume failed")
			return fmt.Errorf("read volume: %w", err)
		}
		for _, e := range entries {
			if err := os.RemoveAll(filepath.Join(mp, e.Name())); err != nil {
				log.WithError(err).Error("Wipe named volume failed")
				return fmt.Errorf("wipe volume: %w", err)
			}
		}

		if err := uc.repo.Restart(ctx, name); err != nil {
			log.WithError(err).Error("Restart after reset failed")
			return err
		}

		log.WithField("volume", volPath).Info("Named volume reset complete")
		return nil
	}

	files, err := filepath.Glob(filepath.Join(volPath, "*"))
	if err != nil {
		log.WithError(err).Error("Glob failed on volume path")
		return fmt.Errorf("glob volume: %w", err)
	}
	for _, f := range files {
		os.RemoveAll(f)
	}

	if err := os.Chown(volPath, 1000, 1000); err != nil {
		log.WithError(err).Error("Chown after reset failed")
		return fmt.Errorf("chown volume: %w", err)
	}

	if err := uc.repo.Restart(ctx, name); err != nil {
		log.WithError(err).Error("Restart after reset failed")
		return err
	}

	log.WithFields(logger.Fields{"vol_path": volPath, "files_removed": len(files)}).Info("Volume reset complete")
	return nil
}

// List returns all containers (stopped + running).
func (uc *ContainerUseCase) List(ctx context.Context) ([]domain.ContainerSummary, error) {
	list, err := uc.repo.List(ctx, true)
	if err != nil {
		uc.log.WithError(err).Error("List containers failed")
		return nil, err
	}
	return list, nil
}

// IP returns the first non-empty IP address assigned to the named container.
func (uc *ContainerUseCase) IP(ctx context.Context, name string) (string, error) {
	ip, err := uc.repo.IP(ctx, name)
	if err != nil {
		uc.log.WithField("container", name).WithError(err).Error("IP lookup failed")
		return "", err
	}
	return ip, nil
}

// Stats returns live CPU and memory metrics for a named container.
func (uc *ContainerUseCase) Stats(ctx context.Context, name string) (*domain.Stats, error) {
	stats, err := uc.repo.Stats(ctx, name)
	if err != nil {
		uc.log.WithField("container", name).WithError(err).Error("Stats failed")
		return nil, err
	}
	return stats, nil
}

// Health returns current node-level resource metrics.
func (uc *ContainerUseCase) Health(ctx context.Context) (*domain.NodeHealth, error) {
	cpuPct, err := uc.sys.CPUPercent()
	if err != nil {
		uc.log.WithError(err).Warn("CPU metrics unavailable")
	}
	memMB, err := uc.sys.MemAvailMB()
	if err != nil {
		uc.log.WithError(err).Warn("Memory metrics unavailable")
	}
	return &domain.NodeHealth{
		NodeID:     uc.cfg.NodeID,
		CPUPercent: cpuPct,
		MemFreeMB:  memMB,
		Status:     "online",
	}, nil
}

// --- Network Lifecycle ---

func (uc *ContainerUseCase) CreateNetwork(ctx context.Context, name string) error {
	logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "create_network", "name": name}).Info("Creating network")
	return uc.repo.CreateNetwork(ctx, name)
}

func (uc *ContainerUseCase) DeleteNetwork(ctx context.Context, name string) error {
	logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "delete_network", "name": name}).Info("Deleting network")
	return uc.repo.DeleteNetwork(ctx, name)
}

func (uc *ContainerUseCase) ListNetworks(ctx context.Context) ([]string, error) {
	return uc.repo.ListNetworks(ctx)
}

// --- Volume Lifecycle ---

func (uc *ContainerUseCase) CreateVolume(ctx context.Context, name string) error {
	logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "create_volume", "name": name}).Info("Creating volume")
	return uc.repo.CreateVolume(ctx, name)
}

func (uc *ContainerUseCase) DeleteVolume(ctx context.Context, name string) error {
	logger.FromContext(ctx, uc.log).WithFields(logger.Fields{"op": "delete_volume", "name": name}).Info("Deleting volume")
	return uc.repo.DeleteVolume(ctx, name)
}

func (uc *ContainerUseCase) ListVolumes(ctx context.Context) ([]string, error) {
	return uc.repo.ListVolumes(ctx)
}

// --- private helpers ---

// canAccept checks running-container allocations, plus any pending in-flight
// reservations (see reservation), against node quotas. selfName excludes a
// same-named reservation from the tally (defensive against retries/overlap).
// Must be called with uc.mu held.
func (uc *ContainerUseCase) canAccept(ctx context.Context, selfName string, newMem int64, newCPU float64) (bool, string, error) {
	// Only running containers consume resources.
	containers, err := uc.repo.List(ctx, false)
	if err != nil {
		return false, "", fmt.Errorf("list containers for admission: %w", err)
	}

	var allocMem int64
	var allocCPU float64
	for _, c := range containers {
		res, err := uc.repo.Resources(ctx, c.ID)
		if err != nil {
			uc.log.WithField("container_id", c.ID).WithError(err).Warn("Could not inspect container; skipping for admission")
			continue
		}
		allocMem += res.MemoryBytes
		allocCPU += float64(res.NanoCPUs) / 1e9
	}

	for name, r := range uc.reservations {
		if name == selfName {
			continue
		}
		allocMem += r.memory
		allocCPU += r.cpu
	}

	if (allocMem+newMem)/1024/1024 > uc.cfg.MaxMemoryMB {
		return false, fmt.Sprintf("memory quota exceeded (allocated %dMB + request %dMB > limit %dMB)",
			allocMem/1024/1024, newMem/1024/1024, uc.cfg.MaxMemoryMB), nil
	}
	if allocCPU+newCPU > uc.cfg.MaxCPUUnits {
		return false, fmt.Sprintf("CPU quota exceeded (allocated %.2f + request %.2f > limit %.2f)",
			allocCPU, newCPU, uc.cfg.MaxCPUUnits), nil
	}
	return true, "", nil
}

// checkPortConflicts ensures none of the requested host ports are already
// published by another running container on this node. selfName is excluded
// from the check (relevant for Update, which restarts the same container).
// Must be called with uc.mu held, alongside canAccept.
func (uc *ContainerUseCase) checkPortConflicts(ctx context.Context, selfName string, ports []domain.PortMapping) error {
	requested := make(map[int]bool, len(ports))
	for _, p := range ports {
		if p.HostPort > 0 {
			requested[p.HostPort] = true
		}
	}
	if len(requested) == 0 {
		return nil
	}

	containers, err := uc.repo.List(ctx, false)
	if err != nil {
		return fmt.Errorf("list containers for port check: %w", err)
	}
	for _, c := range containers {
		if containsName(c.Names, selfName) {
			continue
		}
		for _, p := range c.Ports {
			if p.HostPort > 0 && requested[p.HostPort] {
				return fmt.Errorf("host port %d is already in use by another container", p.HostPort)
			}
		}
	}

	for name, r := range uc.reservations {
		if name == selfName {
			continue
		}
		for port := range r.ports {
			if requested[port] {
				return fmt.Errorf("host port %d is reserved by an in-flight deployment", port)
			}
		}
	}
	return nil
}

// pathWithinBase reports whether abs (already filepath.Clean'd) is base
// itself or a descendant of it. A plain strings.HasPrefix(abs, base) would
// wrongly accept a sibling directory whose name merely starts with base's
// name (e.g. base "/data" matching "/data-evil"), so this requires a path
// separator boundary right after base.
func pathWithinBase(abs, base string) bool {
	return abs == base || strings.HasPrefix(abs, base+string(filepath.Separator))
}

// containsName reports whether names (as returned by Docker, e.g. "/my-app")
// includes the given bare container name.
func containsName(names []string, name string) bool {
	for _, n := range names {
		if strings.TrimPrefix(n, "/") == name {
			return true
		}
	}
	return false
}

func validateRequest(req domain.DeployRequest, portRange config.PortRange, volumeBase string) error {
	if req.Image == "" {
		return fmt.Errorf("image is required")
	}
	if req.Name == "" {
		return fmt.Errorf("name is required")
	}
	// Volume validation with precedence
	if len(req.Volumes) > 0 {
		cleanBase := filepath.Clean(volumeBase)
		for i, v := range req.Volumes {
			if v.BindPath == "" {
				return fmt.Errorf("volume[%d].bind_path is required", i)
			}
			if !filepath.IsAbs(v.BindPath) {
				return fmt.Errorf("volume[%d].bind_path must be an absolute path", i)
			}
			switch v.Type {
			case "host":
				if v.HostPath == "" {
					return fmt.Errorf("volume[%d].host_path is required", i)
				}
				if !filepath.IsAbs(v.HostPath) {
					return fmt.Errorf("volume[%d].host_path must be an absolute path", i)
				}
				abs := filepath.Clean(filepath.Join(volumeBase, v.HostPath))
				if !pathWithinBase(abs, cleanBase) {
					return fmt.Errorf("volume[%d].host_path is invalid", i)
				}
			case "", "docker":
				if v.VolumeName == "" {
					return fmt.Errorf("volume[%d].volume_name is required", i)
				}
			default:
				return fmt.Errorf("volume[%d].type is invalid", i)
			}
		}
	}

	// Port validation
	if len(req.Ports) > 0 {
		for i, p := range req.Ports {
			if p.HostPort < 0 || p.HostPort > 65535 {
				return fmt.Errorf("port[%d].host_port is invalid", i)
			}
			if p.ContainerPort <= 0 || p.ContainerPort > 65535 {
				return fmt.Errorf("port[%d].container_port is invalid", i)
			}
			if p.HostPort > 0 && !portRange.Contains(p.HostPort) {
				return fmt.Errorf("port[%d].host_port %d is outside the allowed range (%d-%d)", i, p.HostPort, portRange.Min, portRange.Max)
			}
		}
	}

	if req.MemoryReservation != 0 && req.MemoryReservation > req.MemoryLimit {
		return fmt.Errorf("memory_reservation (%d) must not exceed memory_limit (%d)", req.MemoryReservation, req.MemoryLimit)
	}
	if req.MemoryLimit <= 0 {
		return fmt.Errorf("memory_limit must be positive")
	}
	if req.CPULimit <= 0 {
		return fmt.Errorf("cpu_limit must be positive")
	}
	if req.StopGracePeriod < 0 {
		return fmt.Errorf("stop_grace_period must not be negative")
	}
	if req.PidsLimit < 0 {
		return fmt.Errorf("pids_limit must not be negative")
	}
	if req.ShmSize < 0 {
		return fmt.Errorf("shm_size must not be negative")
	}
	for i, u := range req.Ulimits {
		if u.Name == "" {
			return fmt.Errorf("ulimits[%d].name is required", i)
		}
		if u.Soft < 0 || u.Hard < 0 {
			return fmt.Errorf("ulimits[%d] soft/hard must not be negative", i)
		}
		if u.Soft > u.Hard {
			return fmt.Errorf("ulimits[%d].soft must not exceed hard", i)
		}
	}
	for i, opt := range req.SecurityOpt {
		lower := strings.ToLower(opt)
		if strings.Contains(lower, "unconfined") || strings.HasPrefix(lower, "no-new-privileges=false") {
			return fmt.Errorf("security_opt[%d] must not disable a security profile", i)
		}
	}
	for i, h := range req.ExtraHosts {
		parts := strings.SplitN(h, ":", 2)
		if len(parts) != 2 || parts[0] == "" || net.ParseIP(parts[1]) == nil {
			return fmt.Errorf("extra_hosts[%d] must be in \"host:ip\" form with a valid IP", i)
		}
	}
	if req.Logging != nil && req.Logging.Driver == "" {
		return fmt.Errorf("logging.driver is required when logging is set")
	}
	for path := range req.Tmpfs {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("tmpfs path %q must be an absolute path", path)
		}
	}
	return nil
}

func scrubEnv(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	for k, v := range env {
		upper := strings.ToUpper(k)
		blocked := strings.HasPrefix(upper, "AGENT_BOX_") // block any env vars that start with these prefixes
		for _, sensitive := range sensitiveEnvKeys {
			if upper == sensitive {
				blocked = true
				break
			}
		}
		if !blocked {
			out[k] = v
		}
	}
	return out
}

func buildSpec(req domain.DeployRequest, volumeBase string) domain.DeploySpec {
	hardLimit := req.MemoryLimit
	softLimit := req.MemoryReservation
	if softLimit == 0 {
		softLimit = min(softLimitBytes, hardLimit)
	}

	clean := scrubEnv(req.Env)
	envSlice := make([]string, 0, len(clean))
	for k, v := range clean {
		envSlice = append(envSlice, fmt.Sprintf("%s=%s", k, v))
	}

	spec := domain.DeploySpec{
		Image:         req.Image,
		Name:          req.Name,
		MemoryLimit:   hardLimit,
		SoftLimit:     softLimit,
		CPUNano:       int64(req.CPULimit * 1e9),
		Env:           envSlice,
		RestartPolicy: req.RestartPolicy,
		Command:       req.Command,
		Entrypoint:    req.Entrypoint,
		Networks:      req.Networks,
		Healthcheck:   req.Healthcheck,

		User:            req.User,
		WorkingDir:      req.WorkingDir,
		Labels:          req.Labels,
		StopSignal:      req.StopSignal,
		StopGracePeriod: req.StopGracePeriod,
		ReadOnly:        req.ReadOnly,

		PidsLimit:   req.PidsLimit,
		ShmSize:     req.ShmSize,
		Ulimits:     req.Ulimits,
		CapDrop:     req.CapDrop,
		SecurityOpt: req.SecurityOpt,

		DNS:        req.DNS,
		DNSSearch:  req.DNSSearch,
		ExtraHosts: req.ExtraHosts,
		Logging:    req.Logging,
		Tmpfs:      req.Tmpfs,
		Sysctls:    req.Sysctls,
	}

	if len(req.Volumes) > 0 {
		spec.Volumes = make([]domain.VolumeBind, len(req.Volumes))
		copy(spec.Volumes, req.Volumes)
		for i, v := range spec.Volumes {
			if v.Type == "host" {
				spec.Volumes[i].HostPath = filepath.Clean(filepath.Join(volumeBase, v.HostPath))
			}
		}
	}

	spec.Ports = req.Ports

	return spec
}

func prepareVolume(path string) error {
	if err := os.MkdirAll(path, 0755); err != nil {
		return fmt.Errorf("create volume dir: %w", err)
	}
	if err := os.Chown(path, 1000, 1000); err != nil {
		return fmt.Errorf("chown volume dir: %w", err)
	}
	return nil
}

func (uc *ContainerUseCase) ImageExists(ctx context.Context, image string) (bool, error) {
	return uc.repo.ImageExists(ctx, image)
}

func (uc *ContainerUseCase) PullImage(ctx context.Context, image string) error {
	return uc.repo.PullImage(ctx, image)
}

func (uc *ContainerUseCase) ListImages(ctx context.Context) ([]domain.ImageSummary, error) {
	return uc.repo.ListImages(ctx)
}
