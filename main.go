package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/docker/docker/client"

	_ "github.com/fitraditya/litepod/docs"
	"github.com/fitraditya/litepod/internal/config"
	"github.com/fitraditya/litepod/internal/handler"
	dockerrepo "github.com/fitraditya/litepod/internal/infra/docker"
	"github.com/fitraditya/litepod/internal/infra/system"
	"github.com/fitraditya/litepod/internal/usecase"
	"github.com/fitraditya/litepod/pkg/logger"
)

// dockerPingTimeout bounds the startup check that the configured Docker
// daemon is actually reachable, so a dead/misconfigured daemon fails fast at
// boot instead of surfacing as a mysterious error on the first deploy.
const dockerPingTimeout = 10 * time.Second

// shutdownTimeout bounds how long the server waits for in-flight requests to
// finish on SIGINT/SIGTERM before forcing the process to exit.
const shutdownTimeout = 15 * time.Second

// Default listen ports when Port isn't set in config.yaml/PORT env var:
// the conventional plain-HTTP port, and the conventional HTTPS port when
// TLS_CERT_FILE/TLS_KEY_FILE are configured.
const (
	defaultPlainPort = 8080
	defaultTLSPort   = 8443
)

// @title           Litepod API
// @version         1.0
// @description     HTTP API for managing containerized services on a Litepod node.
// @BasePath        /
// @securityDefinitions.apikey  ApiKeyAuth
// @in                          header
// @name                        X-API-KEY
// @description                 API key required for all /containers endpoints
func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		logger.New("", "").WithError(err).Fatal("Failed to load config")
	}

	log := logger.New(cfg.NodeID, cfg.SentryDSN)
	log.Info("Starting Litepod")

	dockerClient, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		log.WithError(err).Fatal("Docker client initialization failed")
	}

	pingCtx, cancelPing := context.WithTimeout(context.Background(), dockerPingTimeout)
	_, err = dockerClient.Ping(pingCtx)
	cancelPing()
	if err != nil {
		log.WithError(err).Fatal("Docker daemon not reachable")
	}

	repo := dockerrepo.NewRepository(dockerClient, log)
	sys := system.NewMetrics()
	uc := usecase.NewContainerUseCase(repo, sys, cfg, log)

	containerHandler := handler.NewContainerHandler(uc, log)
	healthHandler := handler.NewHealthHandler(uc, log)
	router := handler.NewRouter(cfg, containerHandler, healthHandler, log)

	certFile, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	useTLS := certFile != "" && keyFile != ""

	port := cfg.Port
	if port == 0 {
		if useTLS {
			port = defaultTLSPort
		} else {
			port = defaultPlainPort
		}
	}
	addr := fmt.Sprintf(":%d", port)
	srv := &http.Server{Addr: addr, Handler: router}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() {
		log.WithFields(logger.Fields{"addr": addr, "tls": useTLS}).Info("Litepod running")
		// The API key travels in the X-API-KEY header on every request; without
		// TLS it's plaintext on the wire, so native TLS is offered whenever a
		// cert/key pair is configured rather than requiring a separate proxy.
		if useTLS {
			serveErr <- srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			serveErr <- srv.ListenAndServe()
		}
	}()

	select {
	case err := <-serveErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.WithError(err).Fatal("Server stopped")
		}
	case <-ctx.Done():
		stop()
		log.Info("Shutdown signal received, draining in-flight requests")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.WithError(err).Error("Graceful shutdown failed, forcing exit")
			os.Exit(1)
		}
		log.Info("Litepod stopped")
	}
}
