package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	httpSwagger "github.com/swaggo/http-swagger"

	"github.com/fitraditya/litepod/internal/config"
	"github.com/fitraditya/litepod/internal/handler/middleware"
	"github.com/fitraditya/litepod/pkg/logger"
)

// NewRouter wires all routes and middleware onto a Chi router.
func NewRouter(
	cfg *config.Config,
	containers *ContainerHandler,
	health *HealthHandler,
	log *logger.Logger,
) http.Handler {
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Recoverer)
	r.Use(middleware.Logger(log))
	r.Use(middleware.RateLimit(cfg.RateLimitRPS, cfg.RateLimitBurst))

	// Public endpoints
	r.Get("/health", health.Health)
	if !cfg.SwaggerDisabled {
		r.Get("/swagger", http.RedirectHandler("/swagger/index.html", http.StatusMovedPermanently).ServeHTTP)
		r.Get("/swagger/", http.RedirectHandler("/swagger/index.html", http.StatusMovedPermanently).ServeHTTP)
		r.Get("/swagger/*", httpSwagger.WrapHandler)
	}

	r.Route("/containers", func(r chi.Router) {
		r.Use(middleware.Auth(cfg))

		r.Get("/", containers.List)
		r.Post("/", containers.Deploy)

		r.Route("/{name}", func(r chi.Router) {
			r.Put("/", containers.Update)
			r.Delete("/", containers.Destroy)
			r.Post("/start", containers.Start)
			r.Post("/stop", containers.Stop)
			r.Post("/restart", containers.Restart)
			r.Post("/reset", containers.Reset)
			r.Post("/pause", containers.Pause)
			r.Post("/unpause", containers.Unpause)
			r.Post("/kill", containers.Kill)
			r.Post("/suspend", containers.Suspend)
			r.Post("/unsuspend", containers.Unsuspend)
			r.Get("/stats", containers.Stats)
			r.Get("/ip", containers.IP)
			r.Get("/state", containers.State)
			r.Get("/logs", containers.Logs)
		})
	})

	r.Route("/images", func(r chi.Router) {
		r.Use(middleware.Auth(cfg))
		r.Get("/", containers.ListImages)
		r.Head("/", containers.CheckImage)
		r.Post("/pull", containers.PullImage)
	})

	r.Route("/networks", func(r chi.Router) {
		r.Use(middleware.Auth(cfg))
		r.Get("/", containers.ListNetworks)
		r.Post("/", containers.CreateNetwork)
		r.Delete("/{name}", containers.DeleteNetwork)
	})

	r.Route("/volumes", func(r chi.Router) {
		r.Use(middleware.Auth(cfg))
		r.Get("/", containers.ListVolumes)
		r.Post("/", containers.CreateVolume)
		r.Delete("/{name}", containers.DeleteVolume)
	})

	return r
}
