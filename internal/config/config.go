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

// defaultVolumeBase is used when volume_base is not set in config.yaml.
const defaultVolumeBase = "/home/deployer/data/"

type Config struct {
	NodeID          string    `yaml:"node_id"`
	APIKey          string    `yaml:"api_key"`
	MaxMemoryMB     int64     `yaml:"max_memory_mb"`
	MaxCPUUnits     float64   `yaml:"max_cpu_units"`
	DeployPortRange PortRange `yaml:"deploy_port_range"`
	VolumeBase      string    `yaml:"volume_base"`
	SentryDSN       string    `yaml:"sentry_dsn"`
	// Port the HTTP server listens on. Zero means "use the default for
	// whether TLS is enabled" (see main.go: 8080 plain, 8443 with
	// TLS_CERT_FILE/TLS_KEY_FILE set) — resolved at startup, not here, since
	// that decision also depends on the TLS env vars.
	Port int `yaml:"port"`
}

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

	return &cfg, nil
}
