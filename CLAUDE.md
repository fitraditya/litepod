# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Litepod: a per-node HTTP agent (Go module `github.com/fitraditya/litepod`) that manages Docker containers on a single host. It's the thing that runs *on* each node of a multi-node hosting platform, not a control-plane/orchestrator itself — deploy requests come in from elsewhere and this agent executes them locally against the Docker daemon.

## Commands

```bash
make run       # go run main.go — requires config.yaml (copy from config.yaml.example)
make build     # compile to ./bin/litepod
make test      # go test ./...
make coverage  # tests + coverage.html report
make swag      # regenerate docs/ (Swagger) from annotations in main.go / handlers — run after
               # changing any @-annotated handler or route
```

Run a single test: `go test ./internal/usecase/... -run TestName -v`

There is no lint target in the Makefile; CI does not run one either.

Requires Go 1.25+ and a reachable Docker daemon (standard `DOCKER_HOST` env or local socket). Server listens on `:8080`; Swagger UI at `/swagger/index.html`.

## Architecture

Standard ports-and-adapters layout. Dependency direction is inward only:

```
internal/domain    interfaces (ports) + shared types — no dependency on anything else
internal/usecase    business logic, implements orchestration against domain interfaces
internal/infra/*    adapters that implement domain interfaces (docker, system)
internal/handler    HTTP layer (chi router, handlers, middleware) — depends on usecase
internal/config      YAML config loading + env var overrides
```

- `internal/domain/port.go` defines the two interfaces the use case depends on: `ContainerRepo` (implemented by `internal/infra/docker`, wraps the Docker SDK client) and `SystemMetrics` (implemented by `internal/infra/system`, wraps gopsutil for host CPU/mem).
- `internal/usecase/container.go` (`ContainerUseCase`) is the single orchestrator for all container lifecycle operations (deploy, update, start/stop/restart/reset, suspend/unsuspend, destroy, stats, list, networks, volumes, images). It holds an in-memory `reservation` map guarded by a mutex to admission-control concurrent Deploy/Update calls against node capacity and port conflicts *before* the container actually exists and shows up via `repo.List` — this in-flight tracking exists specifically to close a race window while slow Docker calls are in progress. Be careful preserving this when touching Deploy/Update.
- `internal/handler/container.go` is the HTTP-to-usecase translation layer (chi handlers with Swagger annotations); `internal/handler/router.go` wires routes and auth middleware.
- Auth: a single static API key (`X-API-KEY` header, checked in `internal/handler/middleware/auth.go`) gates `/containers`, `/images`, `/networks`, `/volumes`. `/health` and `/swagger/*` are unauthenticated.
- `DeployRequest`/`DeploySpec` in `internal/domain/model.go` hold every field the deploy/update use case accepts. Full field-by-field reference below ("Deploy payload fields").
- `sensitiveEnvKeys` in `internal/usecase/container.go` (`DOCKER_HOST`, `AGENT_KEY`, `AGENT_BOX_API_KEY`) are stripped from any env vars a deploy request tries to pass into a container.
- Config (`internal/config/config.go`) loads `config.yaml`, then lets env vars override specific fields: `AGENT_BOX_API_KEY`, `DEPLOY_PORT_RANGE` (`"min-max"` format), `VOLUME_BASE`, `SENTRY_DSN`.
- Logging (`pkg/logger/logger.go`) wraps `github.com/obrel/go-lib/pkg/log` (itself a logrus wrapper) — don't reimplement logging here, extend the shared package instead. Level/format via `LOG_LEVEL`/`LOG_FORMAT` env vars; if `SentryDSN` is configured, errors also report to Sentry (`SENTRY_ENVIRONMENT`, default `production`; `APP_VERSION`, default `dev`).
- `main.go` pings the Docker daemon at startup (`client.Ping`, 10s timeout) and `Fatal`s if it's unreachable — a dead/misconfigured daemon fails at boot, not on the first deploy request. The HTTP server also does a graceful shutdown on SIGINT/SIGTERM (`signal.NotifyContext` + `srv.Shutdown` with a 15s drain timeout via `http.Server`, not the bare `http.ListenAndServe` helpers) — in-flight requests get to finish before the process exits.

## Deploy payload fields

Every field the `POST /containers` / `PUT /containers/{name}` body (`handler.DeployPayload` → `domain.DeployRequest` → `domain.DeploySpec`) accepts, and what it maps to on the Docker Engine API (`internal/infra/docker/repository.go`, `Run()`). Body decoding uses `DisallowUnknownFields` — any key not listed here is rejected with 400, not silently dropped. When building a deploy request programmatically, use only what's in this table.

**Identity / image**

| JSON key | Type | Docker API target | Notes |
|---|---|---|---|
| `image` | string | `Config.Image` | required |
| `name` | string | container name | required, unique per host |
| `command` | []string | `Config.Cmd` | |
| `entrypoint` | []string | `Config.Entrypoint` | |
| `user` | string | `Config.User` | uid, name, or `"user:group"` |
| `working_dir` | string | `Config.WorkingDir` | |
| `env` | map[string]string | `Config.Env` | keys `DOCKER_HOST`, `AGENT_KEY`, `AGENT_BOX_API_KEY`, and anything prefixed `AGENT_BOX_` are stripped server-side, never reach the container |
| `labels` | map[string]string | `Config.Labels` | |

**Resources**

| JSON key | Type | Docker API target | Notes |
|---|---|---|---|
| `memory_limit` | int64 (bytes) | `HostConfig.Resources.Memory` | required, > 0 |
| `memory_reservation` | int64 (bytes) | `HostConfig.Resources.MemoryReservation` | optional; must not exceed `memory_limit` |
| `cpu_limit` | float64 (cores) | `HostConfig.Resources.NanoCPUs` | required, > 0; converted to nano-CPUs |
| `pids_limit` | int64 | `HostConfig.Resources.PidsLimit` | caps forkbomb-style abuse |
| `shm_size` | int64 (bytes) | `HostConfig.ShmSize` | `/dev/shm` size |
| `ulimits` | `[{name, soft, hard}]` | `HostConfig.Resources.Ulimits` | soft ≤ hard enforced |

**Storage**

| JSON key | Type | Docker API target | Notes |
|---|---|---|---|
| `volumes` | `[{type, volume_name, host_path, bind_path, read_only}]` | `HostConfig.Binds` | `type: "docker"` (default) uses a named volume (`volume_name`); `type: "host"` bind-mounts `host_path` (validated to stay under the configured volume base, no traversal) |
| `tmpfs` | map[string]string | `HostConfig.Tmpfs` | container path → mount options (e.g. `"size=64m"`); path must be absolute |
| `read_only` | bool | `HostConfig.ReadonlyRootfs` | root filesystem read-only |

**Networking**

| JSON key | Type | Docker API target | Notes |
|---|---|---|---|
| `ports` | `[{host_port, container_port, protocol}]` | `Config.ExposedPorts` / `HostConfig.PortBindings` | `host_port` must fall inside `DEPLOY_PORT_RANGE`; checked for conflicts against other in-flight/running containers |
| `networks` | []string | `NetworkingConfig.EndpointsConfig` | network names to attach to; no per-network static IP/aliases (single-IP host, not needed) |
| `dns` | []string | `HostConfig.DNS` | |
| `dns_search` | []string | `HostConfig.DNSSearch` | |
| `extra_hosts` | []string | `HostConfig.ExtraHosts` | `"host:ip"` form, validated |

**Lifecycle**

| JSON key | Type | Docker API target | Notes |
|---|---|---|---|
| `restart_policy` | string | `HostConfig.RestartPolicy` | e.g. `no`, `always`, `unless-stopped`, `on-failure` |
| `stop_signal` | string | `Config.StopSignal` | |
| `stop_grace_period` | int (seconds) | `Config.StopTimeout` | whole seconds, not a duration string |
| `healthcheck` | `{test, interval_seconds, timeout_seconds, retries}` | `Config.Healthcheck` | |
| `logging` | `{driver, options}` | `HostConfig.LogConfig` | `driver` required if `logging` is set at all |

**Hardening**

| JSON key | Type | Docker API target | Notes |
|---|---|---|---|
| `cap_drop` | []string | `HostConfig.CapDrop` | kernel capabilities to drop |
| `security_opt` | []string | `HostConfig.SecurityOpt` | rejected if any entry contains `unconfined` or is `no-new-privileges=false` — those weaken the sandbox instead of hardening it |
| `sysctls` | map[string]string | `HostConfig.Sysctls` | namespaced kernel sysctls only (Docker itself restricts what's settable without `--privileged`) |

**Deliberately not exposed** — do not add without a deliberate security review, this is a multi-tenant host running untrusted containers:
- `cap_add`, `privileged` — arbitrary capability escalation / full privileged mode is a container-escape vector. Only capability *dropping* is allowed, never adding.
- `devices` — host device passthrough; no current use case.
- Per-network static IP / aliases — host has a single public IP, containers reach the outside via port-mapping, not routable per-container addresses.

## CI/release

- Git remote is `github.com/fitraditya/litepod` (`origin`).
- `.github/workflows/ci.yml` runs `go build`/`go vet`/`go test ./...` on every push to `main` and every pull request. `internal/infra/docker`'s tests are integration tests against a real Docker daemon (skipped automatically if none is reachable) — GitHub's `ubuntu-latest` runners ship Docker pre-installed, so they run for real there, not skipped. The test image is pulled on demand (`pullTestImageOnce` in `repository_test.go`), and the step runs under `sudo` because `internal/usecase`'s host-volume tests `chown` to a fixed uid/gid, which needs root.
- `.github/workflows/release.yml` runs GoReleaser on `v*.*.*` tags, building linux/amd64 and linux/arm64 binaries (stripped via `-trimpath`/`-ldflags="-s -w"`) and publishing them as a GitHub Release with checksums, using the repo-scoped `GITHUB_TOKEN` (no extra secret needed). `.goreleaser.yaml`'s `release.github` points at `owner: fitraditya, name: litepod` — update it if the repo ever moves.
- To cut a release: tag `vX.Y.Z` and push the tag (`git tag vX.Y.Z && git push origin vX.Y.Z`). The workflow does the rest.
