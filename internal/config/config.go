package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// PortRange restricts which host ports a deployed container may bind to. A
// zero-value range (Min == 0 && Max == 0) means no restriction is enforced.
type PortRange struct {
	Min int `yaml:"min"`
	Max int `yaml:"max"`
}

// Contains reports whether port falls within the configured range. An unset
// range (Min == 0 && Max == 0) permits any port.
func (r PortRange) Contains(port int) bool {
	if r.Min == 0 && r.Max == 0 {
		return true
	}
	return port >= r.Min && port <= r.Max
}

// RegistryAuth is a credential for one container registry (a password or
// access token, e.g. a GitHub PAT with read:packages for ghcr.io).
type RegistryAuth struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// defaultVolumeBase is used when volume_base is not set in config.yaml.
const defaultVolumeBase = "/home/deployer/data/"

type Config struct {
	NodeID          string    `yaml:"node_id"`
	APIKey          string    `yaml:"api_key"`
	MaxMemoryMB     int64     `yaml:"max_memory_mb"`
	MaxCPUUnits     float64   `yaml:"max_cpu_units"`
	DeployPortRange PortRange `yaml:"deploy_port_range"`
	VolumeBase      string    `yaml:"volume_base"`
	// WebhookAPIKey gates the /webhook/* routes (Authorization: Bearer). It is
	// deliberately separate from APIKey so CI systems can be handed a
	// credential that can only redeploy a container with a new image tag.
	// Empty = webhook routes are not mounted at all.
	WebhookAPIKey string `yaml:"webhook_api_key"`
	SentryDSN     string `yaml:"sentry_dsn"`
	// Registries maps a registry host (e.g. "ghcr.io", "registry.example.com:5000",
	// "docker.io") to the credentials used when pulling images from it.
	// Hosts not listed are pulled anonymously. Keys are normalized by Load
	// (lowercased; "index.docker.io" -> "docker.io").
	Registries map[string]RegistryAuth `yaml:"registries"`
	// Port the HTTP server listens on. Zero means "use the default for
	// whether TLS is enabled" (see main.go: 8080 plain, 8443 with
	// TLS_CERT_FILE/TLS_KEY_FILE set) — resolved at startup, not here, since
	// that decision also depends on the TLS env vars.
	Port int `yaml:"port"`
	// SwaggerDisabled turns off the /swagger/* routes. They're
	// unauthenticated by design (reachable without an API key), which also
	// means they're API-surface disclosure to anyone who can reach the node.
	// Defaults to enabled (zero value = false = enabled) for dev convenience;
	// set true in production deployments that don't need it reachable.
	SwaggerDisabled bool `yaml:"swagger_disabled"`
	// RateLimitRPS/RateLimitBurst bound per-client-IP request rate (token
	// bucket), so one caller can't exhaust node resources by hammering the
	// API. Zero in config.yaml means "use the default" (see
	// defaultRateLimitRPS/defaultRateLimitBurst) — there's no "unlimited"
	// escape hatch; a negative rps isn't meaningful so there's nothing
	// sensible to map it to.
	RateLimitRPS   float64 `yaml:"rate_limit_rps"`
	RateLimitBurst int     `yaml:"rate_limit_burst"`
}

// Defaults for RateLimitRPS/RateLimitBurst when unset in config.yaml: this is
// a per-node agent fronted by a control plane, not a public API, so these are
// generous rather than tight — they exist to stop runaway/compromised callers,
// not to throttle normal orchestration traffic.
const (
	defaultRateLimitRPS   = 20.0
	defaultRateLimitBurst = 40
)

// parsePortRange parses a "min-max" string (e.g. "20000-30000") into a PortRange.
func parsePortRange(s string) (PortRange, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return PortRange{}, fmt.Errorf("expected format \"min-max\", got %q", s)
	}
	min, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return PortRange{}, fmt.Errorf("invalid min port %q: %w", parts[0], err)
	}
	max, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil {
		return PortRange{}, fmt.Errorf("invalid max port %q: %w", parts[1], err)
	}
	if min <= 0 || max <= 0 || min > max {
		return PortRange{}, fmt.Errorf("invalid port range %q", s)
	}
	return PortRange{Min: min, Max: max}, nil
}

// Load reads config from a YAML file. If AGENT_BOX_API_KEY is set it overrides
// the api_key value from the file.
func Load(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %q: %w", path, err)
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}

	if key := os.Getenv("AGENT_BOX_API_KEY"); key != "" {
		cfg.APIKey = key
	}
	if key := os.Getenv("WEBHOOK_API_KEY"); key != "" {
		cfg.WebhookAPIKey = key
	}
	if v := os.Getenv("DEPLOY_PORT_RANGE"); v != "" {
		pr, err := parsePortRange(v)
		if err != nil {
			return nil, fmt.Errorf("DEPLOY_PORT_RANGE: %w", err)
		}
		cfg.DeployPortRange = pr
	}
	if v := os.Getenv("VOLUME_BASE"); v != "" {
		cfg.VolumeBase = v
	}
	if cfg.VolumeBase == "" {
		cfg.VolumeBase = defaultVolumeBase
	}
	if v := os.Getenv("SENTRY_DSN"); v != "" {
		cfg.SentryDSN = v
	}
	if v := os.Getenv("SWAGGER_DISABLED"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("SWAGGER_DISABLED: invalid bool %q", v)
		}
		cfg.SwaggerDisabled = b
	}
	if v := os.Getenv("RATE_LIMIT_RPS"); v != "" {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil || f <= 0 {
			return nil, fmt.Errorf("RATE_LIMIT_RPS: invalid value %q", v)
		}
		cfg.RateLimitRPS = f
	}
	if v := os.Getenv("RATE_LIMIT_BURST"); v != "" {
		b, err := strconv.Atoi(v)
		if err != nil || b <= 0 {
			return nil, fmt.Errorf("RATE_LIMIT_BURST: invalid value %q", v)
		}
		cfg.RateLimitBurst = b
	}
	if cfg.RateLimitRPS <= 0 {
		cfg.RateLimitRPS = defaultRateLimitRPS
	}
	if cfg.RateLimitBurst <= 0 {
		cfg.RateLimitBurst = defaultRateLimitBurst
	}
	if v := os.Getenv("PORT"); v != "" {
		port, err := strconv.Atoi(v)
		if err != nil || port <= 0 || port > 65535 {
			return nil, fmt.Errorf("PORT: invalid port %q", v)
		}
		cfg.Port = port
	}

	if cfg.APIKey == "" {
		return nil, fmt.Errorf("api_key is required (set it in config.yaml or via AGENT_BOX_API_KEY)")
	}

	if len(cfg.Registries) > 0 {
		norm := make(map[string]RegistryAuth, len(cfg.Registries))
		for host, a := range cfg.Registries {
			h := strings.ToLower(strings.TrimSpace(host))
			if h == "index.docker.io" {
				h = "docker.io"
			}
			if h == "" || strings.ContainsAny(h, "/ ") {
				return nil, fmt.Errorf("registries: invalid registry host %q (use host[:port], no scheme or path)", host)
			}
			if a.Username == "" || a.Password == "" {
				return nil, fmt.Errorf("registries[%q]: username and password are required", host)
			}
			if _, dup := norm[h]; dup {
				return nil, fmt.Errorf("registries: duplicate registry host %q", h)
			}
			norm[h] = a
		}
		cfg.Registries = norm
	}

	if cfg.WebhookAPIKey != "" && cfg.WebhookAPIKey == cfg.APIKey {
		return nil, fmt.Errorf("webhook_api_key must differ from api_key")
	}

	return &cfg, nil
}
