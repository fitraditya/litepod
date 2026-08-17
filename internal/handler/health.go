package handler

import (
	"net/http"

	"github.com/fitraditya/litepod/internal/usecase"
	"github.com/fitraditya/litepod/pkg/logger"
)

// HealthHandler serves the public health check endpoint.
type HealthHandler struct {
	uc  *usecase.ContainerUseCase
	log *logger.Logger
}

func NewHealthHandler(uc *usecase.ContainerUseCase, log *logger.Logger) *HealthHandler {
	return &HealthHandler{uc: uc, log: log}
}

// Health godoc
// @Summary      Node health check
// @Description  Returns current node CPU and available memory. No authentication required.
// @Tags         health
// @Produce      json
// @Success      200  {object}  HealthResponse
// @Failure      500  {object}  ErrorResponse
// @Router       /health [get]
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	health, err := h.uc.Health(r.Context())
	if err != nil {
		jsonError(w, "health check failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, HealthResponse{
		NodeID:    health.NodeID,
		CPUActual: health.CPUPercent,
		RAMFree:   health.MemFreeMB,
		Status:    health.Status,
	})
}
