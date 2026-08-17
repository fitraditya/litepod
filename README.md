# Litepod

Litepod is a per-node HTTP agent that manages Docker containers on a single host. It's the thing that runs *on* each node of a multi-node hosting platform — deploy requests come from an external control plane, and this agent executes them locally against the Docker daemon. It is not a standalone dashboard/orchestrator: no UI, no multi-host awareness, no RBAC — those concerns live in the control plane driving it.

## Features

- Container lifecycle: deploy, atomic update (stop-remove-start), start/stop/restart, pause/unpause, kill, suspend/unsuspend, reset (wipe volume + restart), destroy
- Live inspection: list, stats (CPU/mem/net), real-time state + health, logs (tail snapshot), IP
- Admission control: rejects deploys/updates that would exceed configured node memory/CPU capacity or conflict on a host port, tracked in-memory across concurrent in-flight requests
- Volumes: create/delete/list, host-bind mounts scoped under a configurable base path
- Networks: create/delete/list
- Images: exists check, list, background pull (bounded timeout, stream-decoded so registry failures are actually caught)
- Deploy requests cover most of the Docker Engine container-create surface — resources, storage, networking, lifecycle, and hardening options. See [Deploy request fields](#deploy-request-fields) below.
- Structured logging via [obrel/go-lib](https://github.com/obrel/go-lib) with optional Sentry error reporting
- Sensitive env vars (`DOCKER_HOST`, `AGENT_KEY`, `AGENT_BOX_API_KEY`) stripped from any container env passed in a deploy request
- Swagger/OpenAPI docs generated from handler annotations

## Requirements

- Go 1.25+
- Docker daemon reachable via the standard Docker environment (`DOCKER_HOST` etc., or the default local socket)

## Configuration

Copy `config.yaml.example` to `config.yaml` and fill in:

```yaml
node_id: "node-jakarta-01"
api_key: "rahasia-banget-123"
max_memory_mb: 12288
max_cpu_units: 5.0
sentry_dsn: ""   # optional; leave empty to disable Sentry error reporting
```

| Field              | YAML key             | Description                                          |
|--------------------|-----------------------|-------------------------------------------------------|
| Node ID            | `node_id`             | Identifies this node in API responses/logs            |
| API key            | `api_key`              | Required — startup fails if unset. Checked via `X-API-KEY` header on all protected routes |
| Max memory (MB)    | `max_memory_mb`        | Node memory ceiling for admission control              |
| Max CPU (cores)    | `max_cpu_units`        | Node CPU ceiling for admission control                  |
| Deploy port range  | `deploy_port_range`    | `{min, max}`; restricts host ports a deployed container may bind to (unset = unrestricted) |
| Volume base path   | `volume_base`          | Host directory root that host-bind volume paths are validated against (default `/home/deployer/data/`) |
| Sentry DSN         | `sentry_dsn`           | Optional; enables Sentry error reporting when set        |
| Port               | `port`                 | Optional. Listen port; if unset defaults to `8080` (plain) or `8443` (when `TLS_CERT_FILE`/`TLS_KEY_FILE` are set) |

### Environment variable overrides

| Variable              | Overrides                                    |
|------------------------|-----------------------------------------------|
| `AGENT_BOX_API_KEY`    | `api_key`                                      |
| `DEPLOY_PORT_RANGE`    | `deploy_port_range` (format `"min-max"`)       |
| `VOLUME_BASE`          | `volume_base`                                  |
| `SENTRY_DSN`           | `sentry_dsn`                                   |
| `PORT`                 | `port`                                         |
| `TLS_CERT_FILE`        | Path to a TLS certificate — enables HTTPS when set together with `TLS_KEY_FILE` |
| `TLS_KEY_FILE`         | Path to the matching TLS private key           |
| `LOG_FORMAT`           | `json` for structured logs, otherwise text     |
| `LOG_LEVEL`            | logrus level (`debug`, `info`, `warn`, ...)    |
| `SENTRY_ENVIRONMENT`   | Sentry environment tag (default `production`)  |
| `APP_VERSION`          | Sentry release tag (default `dev`)             |

## Running

```bash
make run       # go run main.go — requires config.yaml
make build     # compile to ./bin/litepod
make test      # go test ./...
make coverage  # tests + coverage.html report
make swag      # regenerate docs/ (Swagger) after changing @-annotated handlers/routes
```

Once running, the server listens on `:8080` by default (plain HTTP). Swagger UI is available at `/swagger/index.html`.

### Enabling SSL

Set `TLS_CERT_FILE`/`TLS_KEY_FILE` to a cert/key pair and the server switches to HTTPS, defaulting to port `8443` instead of `8080` (set `port`/`PORT` explicitly to override either default). For a quick self-signed cert to test with:

```bash
scripts/generate-cert.sh                # writes scripts/certs/litepod.{crt,key}, gitignored
TLS_CERT_FILE=scripts/certs/litepod.crt TLS_KEY_FILE=scripts/certs/litepod.key ./bin/litepod
```

A self-signed cert isn't trusted by any client's CA store — fine for local testing (`curl -k`), but use a cert from a real CA (Let's Encrypt, your org's PKI) for anything reachable by real clients.

### Running as a systemd service

`scripts/install.sh` builds the binary, installs it, writes a config with a freshly generated `api_key`, and installs `scripts/litepod.service`:

```bash
sudo scripts/install.sh                          # plain HTTP, review config then enable manually
sudo scripts/install.sh --ssl                     # also generates a self-signed cert, serves HTTPS on :8443
sudo scripts/install.sh --ssl --enable            # ... and starts the service immediately
sudo scripts/install.sh --node-id my-node --enable

journalctl -u litepod -f                          # tail logs
```

The generated `api_key` is printed once at the end — store it. `--help` lists all flags. Runs as root — `prepareVolume()` chowns host-bind volume directories to a fixed uid/gid, which needs root or `CAP_CHOWN` regardless of who currently owns the path, and the agent also needs access to the Docker socket.

Or do it by hand — the same steps `install.sh` automates:

```bash
sudo cp bin/litepod /usr/local/bin/litepod

sudo mkdir -p /etc/litepod
sudo cp config.yaml.example /etc/litepod/config.yaml
sudo $EDITOR /etc/litepod/config.yaml   # set a real api_key, node_id, etc.
sudo chmod 600 /etc/litepod/config.yaml # it holds the API key

sudo cp scripts/litepod.service /etc/systemd/system/litepod.service
sudo systemctl daemon-reload
sudo systemctl enable --now litepod
```

See the comments in `scripts/litepod.service` for config file location, optional env-var overrides via `/etc/litepod/litepod.env`, and the graceful-shutdown timeout.

## API overview

All routes except `/health` and `/swagger/*` require an `X-API-KEY` header matching the configured `api_key`.

| Method & Path                              | Purpose                                 |
|---------------------------------------------|-------------------------------------------|
| `GET /health`                                | Node health (unauthenticated)              |
| `GET /containers`                            | List containers                            |
| `POST /containers`                           | Deploy a container                         |
| `PUT /containers/{name}`                     | Atomic update (stop-remove-start)          |
| `DELETE /containers/{name}`                  | Destroy (stop, remove, remove volumes)     |
| `POST /containers/{name}/start`              | Start                                       |
| `POST /containers/{name}/stop`               | Stop (graceful, SIGTERM then SIGKILL)      |
| `POST /containers/{name}/restart`            | Restart                                     |
| `POST /containers/{name}/reset`              | Wipe volume contents, restart               |
| `POST /containers/{name}/pause`              | Freeze processes (cgroup freezer)          |
| `POST /containers/{name}/unpause`            | Resume frozen processes                     |
| `POST /containers/{name}/kill`               | Immediate signal, no grace period (`?signal=`) |
| `POST /containers/{name}/suspend`            | Stop                                        |
| `POST /containers/{name}/unsuspend`          | Start                                       |
| `GET /containers/{name}/stats`               | Live CPU/mem/net metrics                    |
| `GET /containers/{name}/ip`                  | Container IP                                |
| `GET /containers/{name}/state`               | Real-time status + health                   |
| `GET /containers/{name}/logs`                | Recent stdout/stderr (`?tail=`, `?timestamps=`) |
| `HEAD /images`                               | Check image existence (`?name=`)            |
| `GET /images`                                | List locally cached images                  |
| `POST /images/pull`                          | Pull an image (background, bounded timeout) |
| `GET /networks`, `POST /networks`, `DELETE /networks/{name}` | Docker networks on this node |
| `GET /volumes`, `POST /volumes`, `DELETE /volumes/{name}`     | Docker volumes on this node |

Full request/response schemas: `docs/swagger.json` / `docs/swagger.yaml`, or `/swagger/index.html` when the agent is running.

## Deploy request fields

`POST /containers` and `PUT /containers/{name}` take a JSON body that maps onto the Docker Engine API's container-create fields. The body is decoded with unknown fields rejected (400), so this table is the complete, authoritative list — nothing beyond it is accepted.

**Identity / image**

| Field | Type | Notes |
|---|---|---|
| `image` | string | required |
| `name` | string | required, unique per host |
| `command` | []string | |
| `entrypoint` | []string | |
| `user` | string | uid, name, or `"user:group"` to run as |
| `working_dir` | string | |
| `env` | map[string]string | `DOCKER_HOST`, `AGENT_KEY`, `AGENT_BOX_API_KEY`, and any `AGENT_BOX_*` key are stripped before reaching the container |
| `labels` | map[string]string | |

**Resources**

| Field | Type | Notes |
|---|---|---|
| `memory_limit` | int64 (bytes) | required, > 0 |
| `memory_reservation` | int64 (bytes) | optional; must not exceed `memory_limit` |
| `cpu_limit` | float64 (cores) | required, > 0 |
| `pids_limit` | int64 | caps process/thread count |
| `shm_size` | int64 (bytes) | `/dev/shm` size |
| `ulimits` | `[{name, soft, hard}]` | e.g. `nofile`; soft ≤ hard enforced |

**Storage**

| Field | Type | Notes |
|---|---|---|
| `volumes` | `[{type, volume_name, host_path, bind_path, read_only}]` | `type: "docker"` (default) uses a named volume; `type: "host"` bind-mounts a host path under the configured volume base |
| `tmpfs` | map[string]string | container path → mount options (e.g. `"size=64m"`) |
| `read_only` | bool | mount container root filesystem read-only |

**Networking**

| Field | Type | Notes |
|---|---|---|
| `ports` | `[{host_port, container_port, protocol}]` | `host_port` must fall inside the configured `deploy_port_range` |
| `networks` | []string | network names to attach to |
| `dns` | []string | custom DNS servers |
| `dns_search` | []string | DNS search domains |
| `extra_hosts` | []string | additional `/etc/hosts` entries, `"host:ip"` form |

**Lifecycle**

| Field | Type | Notes |
|---|---|---|
| `restart_policy` | string | `no`, `always`, `unless-stopped`, `on-failure` |
| `stop_signal` | string | |
| `stop_grace_period` | int (seconds) | |
| `healthcheck` | `{test, interval_seconds, timeout_seconds, retries}` | |
| `logging` | `{driver, options}` | `driver` required if `logging` is set |

**Hardening**

| Field | Type | Notes |
|---|---|---|
| `cap_drop` | []string | kernel capabilities to drop |
| `security_opt` | []string | e.g. `no-new-privileges`; entries that weaken the sandbox (`unconfined`, `no-new-privileges=false`) are rejected |
| `sysctls` | map[string]string | namespaced kernel sysctls |

**Not exposed, by design:** `cap_add` and `privileged` (capability escalation is a container-escape vector on a multi-tenant host — only dropping capabilities is allowed), device passthrough, and per-network static IPs/aliases (the host has a single public IP; containers are reached via port mapping).

## Architecture

Ports-and-adapters layout, dependency direction inward only:

```
internal/domain    interfaces (ports) + shared types
internal/usecase    business logic, orchestrates against domain interfaces
internal/infra/*    adapters implementing domain interfaces (docker, system)
internal/handler    HTTP layer (chi router, handlers, middleware)
internal/config      YAML config loading + env var overrides
```

See `CLAUDE.md` for implementation details and gotchas (admission-control reservation locking, deploy field validation rules, etc.).

## Release

Push a `vX.Y.Z` tag and GitHub Actions runs GoReleaser to build linux/amd64 and linux/arm64 binaries and publish them as a GitHub Release:

```bash
git tag v0.1.0
git push origin v0.1.0
```