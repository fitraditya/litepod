package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/fitraditya/litepod/internal/domain"
	"github.com/fitraditya/litepod/internal/usecase"
	"github.com/fitraditya/litepod/pkg/logger"
)

// pullTimeout bounds a background image pull so a stuck registry connection
// can't leak the goroutine forever.
const pullTimeout = 15 * time.Minute

// maxBodyBytes caps request bodies so a caller can't exhaust memory with an
// oversized payload; every request body here is a small JSON object.
const maxBodyBytes = 1 << 20 // 1MB

// decodeJSON reads and decodes a JSON request body, capped at maxBodyBytes,
// writing a 400 response and returning false on any failure. The underlying
// decode error (which can quote internal field/type names) is logged
// server-side only; the client gets a fixed, generic message.
func (h *ContainerHandler) decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		h.log.WithError(err).Warn("Invalid request body")
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return false
	}
	return true
}

// ContainerHandler wires HTTP requests to the container use case.
type ContainerHandler struct {
	uc  *usecase.ContainerUseCase
	log *logger.Logger

	pullMu        sync.Mutex      // guards pullsInFlight
	pullsInFlight map[string]bool // images currently being background-pulled, dedupes repeat requests
}

func NewContainerHandler(uc *usecase.ContainerUseCase, log *logger.Logger) *ContainerHandler {
	return &ContainerHandler{uc: uc, log: log, pullsInFlight: make(map[string]bool)}
}

// Deploy godoc
// @Summary      Deploy a container
// @Description  Checks node capacity, creates the volume directory, and starts a new container.
// @Tags         containers
// @Accept       json
// @Produce      json
// @Param        body  body      DeployPayload  true  "Container deploy request"
// @Success      201   {object}  DeployResponse
// @Failure      400   {object}  ErrorResponse
// @Failure      422   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers [post]
func (h *ContainerHandler) Deploy(w http.ResponseWriter, r *http.Request) {
	var req DeployPayload
	if !h.decodeJSON(w, r, &req) {
		return
	}

	result, err := h.uc.Deploy(r.Context(), toDomainRequest(req))
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, DeployResponse{
		ID:     result.ContainerID,
		Node:   result.NodeID,
		Status: "deployed",
	})
}

// Update godoc
// @Summary      Update a container
// @Description  Performs an atomic stop-remove-start cycle with the new spec.
// @Tags         containers
// @Accept       json
// @Produce      json
// @Param        name  path      string         true  "Container name"
// @Param        body  body      DeployPayload  true  "Container update request"
// @Success      200   {object}  UpdateResponse
// @Failure      400   {object}  ErrorResponse
// @Failure      422   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name} [put]
func (h *ContainerHandler) Update(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var req DeployPayload
	if !h.decodeJSON(w, r, &req) {
		return
	}

	result, err := h.uc.Update(r.Context(), name, toDomainRequest(req))
	if err != nil {
		handleError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, UpdateResponse{
		ID:     result.ContainerID,
		Status: "updated",
	})
}

// Destroy godoc
// @Summary      Destroy a container
// @Description  Stops and removes the container and its volumes.
// @Tags         containers
// @Produce      json
// @Param        name  path  string  true  "Container name"
// @Success      204
// @Failure      404  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name} [delete]
func (h *ContainerHandler) Destroy(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Destroy(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Restart godoc
// @Summary      Restart a container
// @Description  Gracefully restarts a running container.
// @Tags         containers
// @Produce      json
// @Param        name  path      string          true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/restart [post]
func (h *ContainerHandler) Restart(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Restart(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "restarted"})
}

// Start godoc
// @Summary      Start a container
// @Description  Explicitly starts a stopped container.
// @Tags         containers
// @Produce      json
// @Param        name  path      string      true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/start [post]
func (h *ContainerHandler) Start(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Start(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "started"})
}

// Stop godoc
// @Summary      Stop a container
// @Description  Explicitly stops a running container.
// @Tags         containers
// @Produce      json
// @Param        name  path      string      true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/stop [post]
func (h *ContainerHandler) Stop(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Stop(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "stopped"})
}

// Reset godoc
// @Summary      Factory reset a container
// @Description  Wipes the container's volume directory and restarts the container.
// @Tags         containers
// @Produce      json
// @Param        name  path      string          true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/reset [post]
func (h *ContainerHandler) Reset(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Reset(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "reset"})
}

// List godoc
// @Summary      List containers
// @Description  Returns all containers on this node (running and stopped).
// @Tags         containers
// @Produce      json
// @Success      200  {array}   ContainerItem
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers [get]
func (h *ContainerHandler) List(w http.ResponseWriter, r *http.Request) {
	containers, err := h.uc.List(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	items := make([]ContainerItem, 0, len(containers))
	for _, c := range containers {
		items = append(items, ContainerItem{
			ID:     c.ID,
			Names:  c.Names,
			Image:  c.Image,
			State:  c.State,
			Status: c.Status,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

// Pause godoc
// @Summary      Pause a container
// @Description  Freezes all processes in the container (cgroup freezer) without terminating them.
// @Tags         containers
// @Produce      json
// @Param        name  path      string      true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/pause [post]
func (h *ContainerHandler) Pause(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Pause(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "paused"})
}

// Unpause godoc
// @Summary      Unpause a container
// @Description  Resumes a paused container's frozen processes.
// @Tags         containers
// @Produce      json
// @Param        name  path      string      true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/unpause [post]
func (h *ContainerHandler) Unpause(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Unpause(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "unpaused"})
}

// Kill godoc
// @Summary      Kill a container
// @Description  Sends a signal to the container's main process immediately, with no grace period.
// @Tags         containers
// @Produce      json
// @Param        name    path   string  true   "Container name"
// @Param        signal  query  string  false  "Signal to send (default SIGKILL)"
// @Success      200   {object}  ActionResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/kill [post]
func (h *ContainerHandler) Kill(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	signal := r.URL.Query().Get("signal")
	if err := h.uc.Kill(r.Context(), name, signal); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "killed"})
}

// Suspend godoc
// @Summary      Suspend a container
// @Description  Stops the container.
// @Tags         containers
// @Produce      json
// @Param        name  path  string  true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/suspend [post]
func (h *ContainerHandler) Suspend(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Suspend(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "suspended"})
}

// Unsuspend godoc
// @Summary      Unsuspend a container
// @Description  Starts the container.
// @Tags         containers
// @Produce      json
// @Param        name  path  string  true  "Container name"
// @Success      200   {object}  ActionResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/unsuspend [post]
func (h *ContainerHandler) Unsuspend(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.Unsuspend(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ActionResponse{Status: "unsuspended"})
}

// IP godoc
// @Summary      Container IP address
// @Description  Returns the first non-empty IP address assigned to the named container.
// @Tags         containers
// @Produce      json
// @Param        name  path      string      true  "Container name"
// @Success      200   {object}  IPResponse
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/ip [get]
func (h *ContainerHandler) IP(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	ip, err := h.uc.IP(r.Context(), name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, IPResponse{IP: ip})
}

// Stats godoc
// @Summary      Container stats
// @Description  Returns live CPU and memory metrics for a named container.
// @Tags         containers
// @Produce      json
// @Param        name  path      string         true  "Container name"
// @Success      200   {object}  StatsResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/stats [get]
func (h *ContainerHandler) Stats(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	stats, err := h.uc.Stats(r.Context(), name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, StatsResponse{
		CPUUsagePercent: stats.CPUPercent,
		MemUsageBytes:   stats.MemUsage,
		MemLimitBytes:   stats.MemLimit,
		NetworkRxBytes:  stats.NetworkRxBytes,
		NetworkTxBytes:  stats.NetworkTxBytes,
	})
}

// State godoc
// @Summary      Container real-time state
// @Description  Returns the full real-time state and health of a named container.
// @Tags         containers
// @Produce      json
// @Param        name  path      string         true  "Container name"
// @Success      200   {object}  domain.ContainerState
// @Failure      404   {object}  ErrorResponse
// @Failure      500   {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/state [get]
func (h *ContainerHandler) State(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	state, err := h.uc.State(r.Context(), name)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// Logs godoc
// @Summary      Container logs
// @Description  Returns recent stdout/stderr logs for a named container.
// @Tags         containers
// @Produce      json
// @Param        name        path      string  true   "Container name"
// @Param        tail        query     int     false  "Number of lines to return from the end of the logs (0 = all)"
// @Param        timestamps  query     bool    false  "Include timestamps in each log line"
// @Success      200  {object}  LogsResponse
// @Failure      404  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /containers/{name}/logs [get]
func (h *ContainerHandler) Logs(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

	tail := 0
	if v := r.URL.Query().Get("tail"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			tail = n
		}
	}
	timestamps := r.URL.Query().Get("timestamps") == "true"

	logs, err := h.uc.Logs(r.Context(), name, tail, timestamps)
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, LogsResponse{Logs: logs})
}

// --- shared helpers ---

func toDomainRequest(req DeployPayload) domain.DeployRequest {
	domainVols := make([]domain.VolumeBind, len(req.Volumes))
	for i, v := range req.Volumes {
		domainVols[i] = domain.VolumeBind{
			Type:       v.Type,
			VolumeName: v.VolumeName,
			HostPath:   v.HostPath,
			BindPath:   v.BindPath,
			ReadOnly:   v.ReadOnly,
		}
	}

	domainPorts := make([]domain.PortMapping, len(req.Ports))
	for i, p := range req.Ports {
		domainPorts[i] = domain.PortMapping{
			HostPort:      p.HostPort,
			ContainerPort: p.ContainerPort,
			Protocol:      p.Protocol,
		}
	}

	var healthcheck *domain.HealthcheckSpec
	if req.Healthcheck != nil {
		healthcheck = &domain.HealthcheckSpec{
			Test:            req.Healthcheck.Test,
			IntervalSeconds: req.Healthcheck.IntervalSeconds,
			TimeoutSeconds:  req.Healthcheck.TimeoutSeconds,
			Retries:         req.Healthcheck.Retries,
		}
	}

	domainUlimits := make([]domain.Ulimit, len(req.Ulimits))
	for i, u := range req.Ulimits {
		domainUlimits[i] = domain.Ulimit{Name: u.Name, Soft: u.Soft, Hard: u.Hard}
	}

	var logging *domain.LoggingSpec
	if req.Logging != nil {
		logging = &domain.LoggingSpec{Driver: req.Logging.Driver, Options: req.Logging.Options}
	}

	return domain.DeployRequest{
		Image:             req.Image,
		Name:              req.ContainerName,
		MemoryLimit:       req.MemoryLimit,
		MemoryReservation: req.MemoryReservation,
		CPULimit:          req.CPULimit,
		RestartPolicy:     req.RestartPolicy,
		Env:               req.Env,
		Volumes:           domainVols,
		Ports:             domainPorts,
		Command:           req.Command,
		Entrypoint:        req.Entrypoint,
		Networks:          req.Networks,
		Healthcheck:       healthcheck,
		User:              req.User,
		WorkingDir:        req.WorkingDir,
		Labels:            req.Labels,
		StopSignal:        req.StopSignal,
		StopGracePeriod:   req.StopGracePeriod,
		ReadOnly:          req.ReadOnly,
		PidsLimit:         req.PidsLimit,
		ShmSize:           req.ShmSize,
		Ulimits:           domainUlimits,
		CapDrop:           req.CapDrop,
		SecurityOpt:       req.SecurityOpt,
		DNS:               req.DNS,
		DNSSearch:         req.DNSSearch,
		ExtraHosts:        req.ExtraHosts,
		Logging:           logging,
		Tmpfs:             req.Tmpfs,
		Sysctls:           req.Sysctls,
	}
}

// --- Network Management ---

// CreateNetwork godoc
// @Summary      Create a network
// @Description  Creates a new Docker network for a user.
// @Tags         networks
// @Accept       json
// @Produce      json
// @Param        body  body      NetworkPayload  true  "Network creation request"
// @Success      201
// @Failure      400  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /networks [post]
func (h *ContainerHandler) CreateNetwork(w http.ResponseWriter, r *http.Request) {
	var req NetworkPayload
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if err := h.uc.CreateNetwork(r.Context(), req.Name); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// DeleteNetwork godoc
// @Summary      Delete a network
// @Description  Deletes an existing Docker network.
// @Tags         networks
// @Produce      json
// @Param        name  path  string  true  "Network name"
// @Success      204
// @Failure      400  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /networks/{name} [delete]
func (h *ContainerHandler) DeleteNetwork(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.DeleteNetwork(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListNetworks godoc
// @Summary      List networks
// @Description  Lists all Docker networks on this node.
// @Tags         networks
// @Produce      json
// @Success      200  {object}  ResourceListResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /networks [get]
func (h *ContainerHandler) ListNetworks(w http.ResponseWriter, r *http.Request) {
	networks, err := h.uc.ListNetworks(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ResourceListResponse{Resources: networks})
}

// --- Volume Management ---

// CreateVolume godoc
// @Summary      Create a volume
// @Description  Creates a new Docker volume for a user.
// @Tags         volumes
// @Accept       json
// @Produce      json
// @Param        body  body      VolumePayload  true  "Volume creation request"
// @Success      201
// @Failure      400  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /volumes [post]
func (h *ContainerHandler) CreateVolume(w http.ResponseWriter, r *http.Request) {
	var req VolumePayload
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if err := h.uc.CreateVolume(r.Context(), req.Name); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// DeleteVolume godoc
// @Summary      Delete a volume
// @Description  Deletes an existing Docker volume.
// @Tags         volumes
// @Produce      json
// @Param        name  path  string  true  "Volume name"
// @Success      204
// @Failure      400  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /volumes/{name} [delete]
func (h *ContainerHandler) DeleteVolume(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if err := h.uc.DeleteVolume(r.Context(), name); err != nil {
		handleError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListVolumes godoc
// @Summary      List volumes
// @Description  Lists all Docker volumes on this node.
// @Tags         volumes
// @Produce      json
// @Success      200  {object}  ResourceListResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /volumes [get]
func (h *ContainerHandler) ListVolumes(w http.ResponseWriter, r *http.Request) {
	vols, err := h.uc.ListVolumes(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ResourceListResponse{Resources: vols})
}

// ListImages godoc
// @Summary      List images
// @Description  Lists all container images cached locally on this node.
// @Tags         images
// @Produce      json
// @Success      200  {array}   ImageItem
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /images [get]
func (h *ContainerHandler) ListImages(w http.ResponseWriter, r *http.Request) {
	images, err := h.uc.ListImages(r.Context())
	if err != nil {
		handleError(w, err)
		return
	}
	items := make([]ImageItem, 0, len(images))
	for _, img := range images {
		items = append(items, ImageItem{
			ID:       img.ID,
			RepoTags: img.RepoTags,
			SizeMB:   img.SizeMB,
		})
	}
	writeJSON(w, http.StatusOK, items)
}

// CheckImage godoc
// @Summary      Check image existence
// @Description  Checks if a container image exists locally on the node.
// @Tags         images
// @Produce      json
// @Param        name  query  string  true  "Image name"
// @Success      200
// @Failure      400  {object}  ErrorResponse
// @Failure      404  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /images [head]
func (h *ContainerHandler) CheckImage(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		jsonError(w, "name query param is required", http.StatusBadRequest)
		return
	}
	exists, err := h.uc.ImageExists(r.Context(), name)
	if err != nil {
		handleError(w, err)
		return
	}
	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// PullImage godoc
// @Summary      Pull a container image
// @Description  Initiates pulling an image from the container registry in the background.
// @Tags         images
// @Accept       json
// @Produce      json
// @Param        body  body  PullImagePayload  true  "Pull image request"
// @Success      202
// @Failure      400  {object}  ErrorResponse
// @Failure      500  {object}  ErrorResponse
// @Security     ApiKeyAuth
// @Router       /images/pull [post]
func (h *ContainerHandler) PullImage(w http.ResponseWriter, r *http.Request) {
	var req PullImagePayload
	if !h.decodeJSON(w, r, &req) {
		return
	}
	if req.Image == "" {
		jsonError(w, "image is required", http.StatusBadRequest)
		return
	}

	// Dedupe: if this image is already being pulled, don't start a second
	// concurrent pull for it — just let the caller poll HEAD /images as usual.
	h.pullMu.Lock()
	alreadyPulling := h.pullsInFlight[req.Image]
	if !alreadyPulling {
		h.pullsInFlight[req.Image] = true
	}
	h.pullMu.Unlock()

	if !alreadyPulling {
		// Pull in background, bounded so a stuck registry connection can't
		// leak the goroutine forever. Callers should poll HEAD /images to
		// check result.
		go func() {
			defer func() {
				h.pullMu.Lock()
				delete(h.pullsInFlight, req.Image)
				h.pullMu.Unlock()
			}()
			ctx, cancel := context.WithTimeout(context.Background(), pullTimeout)
			defer cancel()
			if err := h.uc.PullImage(ctx, req.Image); err != nil {
				h.log.WithError(err).WithField("image", req.Image).Error("Background image pull failed")
			}
		}()
	}

	w.WriteHeader(http.StatusAccepted)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func jsonError(w http.ResponseWriter, msg string, status int) {
	writeJSON(w, status, ErrorResponse{Error: msg})
}

func handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidInput):
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrInsufficientResources):
		jsonError(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrContainerNotFound):
		jsonError(w, err.Error(), http.StatusNotFound)
	default:
		jsonError(w, err.Error(), http.StatusInternalServerError)
	}
}
