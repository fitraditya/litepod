package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPortRange_Contains(t *testing.T) {
	t.Run("unset range allows any port", func(t *testing.T) {
		var r PortRange
		assert.True(t, r.Contains(1))
		assert.True(t, r.Contains(65535))
	})
	t.Run("within range", func(t *testing.T) {
		r := PortRange{Min: 20000, Max: 30000}
		assert.True(t, r.Contains(25000))
		assert.True(t, r.Contains(20000))
		assert.True(t, r.Contains(30000))
	})
	t.Run("outside range", func(t *testing.T) {
		r := PortRange{Min: 20000, Max: 30000}
		assert.False(t, r.Contains(19999))
		assert.False(t, r.Contains(30001))
	})
}

func TestParsePortRange(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    PortRange
		wantErr bool
	}{
		{"valid", "20000-30000", PortRange{20000, 30000}, false},
		{"valid with spaces", " 20000 - 30000 ", PortRange{20000, 30000}, false},
		{"no dash", "20000", PortRange{}, true},
		{"bad min", "abc-30000", PortRange{}, true},
		{"bad max", "20000-abc", PortRange{}, true},
		{"min > max", "30000-20000", PortRange{}, true},
		{"zero min", "0-30000", PortRange{}, true},
		{"negative", "-1-30000", PortRange{}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parsePortRange(tc.in)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestLoad(t *testing.T) {
	t.Run("file not found", func(t *testing.T) {
		_, err := Load("/nonexistent/config.yaml")
		assert.Error(t, err)
	})

	t.Run("invalid yaml", func(t *testing.T) {
		path := writeTempConfig(t, "not: valid: yaml: [")
		_, err := Load(path)
		assert.Error(t, err)
	})

	t.Run("defaults applied", func(t *testing.T) {
		path := writeTempConfig(t, `
node_id: "node-1"
api_key: "key1"
max_memory_mb: 4096
max_cpu_units: 2.0
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, "node-1", cfg.NodeID)
		assert.Equal(t, "key1", cfg.APIKey)
		assert.Equal(t, defaultVolumeBase, cfg.VolumeBase)
	})

	t.Run("explicit volume_base preserved", func(t *testing.T) {
		path := writeTempConfig(t, `
node_id: "node-1"
api_key: "key1"
volume_base: "/custom/base/"
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, "/custom/base/", cfg.VolumeBase)
	})

	t.Run("env overrides", func(t *testing.T) {
		path := writeTempConfig(t, `
node_id: "node-1"
api_key: "from-file"
`)
		t.Setenv("AGENT_BOX_API_KEY", "from-env")
		t.Setenv("DEPLOY_PORT_RANGE", "1000-2000")
		t.Setenv("VOLUME_BASE", "/env/base/")
		t.Setenv("SENTRY_DSN", "https://sentry.example/dsn")
		t.Setenv("PORT", "9443")

		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, "from-env", cfg.APIKey)
		assert.Equal(t, PortRange{1000, 2000}, cfg.DeployPortRange)
		assert.Equal(t, "/env/base/", cfg.VolumeBase)
		assert.Equal(t, "https://sentry.example/dsn", cfg.SentryDSN)
		assert.Equal(t, 9443, cfg.Port)
	})

	t.Run("invalid DEPLOY_PORT_RANGE env errors", func(t *testing.T) {
		path := writeTempConfig(t, `node_id: "node-1"`)
		t.Setenv("DEPLOY_PORT_RANGE", "not-a-range")
		_, err := Load(path)
		assert.Error(t, err)
	})

	t.Run("port defaults to zero (resolved by main, not config)", func(t *testing.T) {
		path := writeTempConfig(t, `
node_id: "node-1"
api_key: "key1"
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, 0, cfg.Port)
	})

	t.Run("explicit port in yaml preserved", func(t *testing.T) {
		path := writeTempConfig(t, `
node_id: "node-1"
api_key: "key1"
port: 9090
`)
		cfg, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, 9090, cfg.Port)
	})

	t.Run("invalid PORT env errors", func(t *testing.T) {
		path := writeTempConfig(t, `
node_id: "node-1"
api_key: "key1"
`)
		t.Setenv("PORT", "not-a-port")
		_, err := Load(path)
		assert.Error(t, err)
	})

	t.Run("missing api_key errors", func(t *testing.T) {
		path := writeTempConfig(t, `node_id: "node-1"`)
		_, err := Load(path)
		assert.Error(t, err)
	})
}
