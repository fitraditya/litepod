package usecase

import (
	"path/filepath"
	"testing"

	"github.com/fitraditya/litepod/internal/config"
	"github.com/fitraditya/litepod/internal/domain"
	"github.com/stretchr/testify/assert"
)

const testVolumeBase = "/home/deployer/data/"

func TestValidateRequest(t *testing.T) {
	tests := []struct {
		name      string
		req       domain.DeployRequest
		portRange config.PortRange
		wantErr   bool
	}{
		{
			name: "valid deploy request",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Volumes: []domain.VolumeBind{
					{VolumeName: "user1_vol1", BindPath: "/data"},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: false,
		},
		{
			name: "invalid host port",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Ports: []domain.PortMapping{
					{HostPort: 70000, ContainerPort: 80},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: true,
		},
		{
			name: "missing image",
			req: domain.DeployRequest{
				Name: "test",
			},
			wantErr: true,
		},
		{
			name: "host port outside configured range",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Ports: []domain.PortMapping{
					{HostPort: 8080, ContainerPort: 80},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			portRange: config.PortRange{Min: 20000, Max: 30000},
			wantErr:   true,
		},
		{
			name: "host port within configured range",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Ports: []domain.PortMapping{
					{HostPort: 25000, ContainerPort: 80},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			portRange: config.PortRange{Min: 20000, Max: 30000},
			wantErr:   false,
		},
		{
			name: "valid host-type volume",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Volumes: []domain.VolumeBind{
					{Type: "host", HostPath: "/data", BindPath: "/var/lib/data"},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: false,
		},
		{
			name: "host-type volume missing host_path",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Volumes: []domain.VolumeBind{
					{Type: "host", BindPath: "/var/lib/data"},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: true,
		},
		{
			name: "host-type volume with relative host_path",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Volumes: []domain.VolumeBind{
					{Type: "host", HostPath: "data", BindPath: "/var/lib/data"},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: true,
		},
		{
			name: "host-type volume with traversal attempt",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Volumes: []domain.VolumeBind{
					{Type: "host", HostPath: "/../../etc", BindPath: "/var/lib/data"},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: true,
		},
		{
			// Regression guard: a naive strings.HasPrefix(abs, cleanBase) check
			// (without a separator boundary) would let this resolve to the
			// sibling directory "/home/deployer/data-evil/secret" — which
			// starts with the string "/home/deployer/data" but is NOT under
			// it — and wrongly pass validation.
			name: "host-type volume escaping to sibling directory sharing base's name prefix",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Volumes: []domain.VolumeBind{
					{Type: "host", HostPath: "/../../data-evil/secret", BindPath: "/var/lib/data"},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: true,
		},
		{
			name: "volume with invalid type",
			req: domain.DeployRequest{
				Image: "nginx",
				Name:  "test",
				Volumes: []domain.VolumeBind{
					{Type: "bogus", BindPath: "/var/lib/data"},
				},
				MemoryLimit: 1024,
				CPULimit:    0.5,
			},
			wantErr: true,
		},
		{
			name: "negative pids_limit",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				PidsLimit: -1,
			},
			wantErr: true,
		},
		{
			name: "negative shm_size",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				ShmSize: -1,
			},
			wantErr: true,
		},
		{
			name: "ulimit soft exceeds hard",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				Ulimits: []domain.Ulimit{{Name: "nofile", Soft: 4096, Hard: 1024}},
			},
			wantErr: true,
		},
		{
			name: "security_opt unconfined rejected",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				SecurityOpt: []string{"apparmor=unconfined"},
			},
			wantErr: true,
		},
		{
			name: "valid pids_limit/shm_size/ulimits/cap_drop/security_opt",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				PidsLimit:   256,
				ShmSize:     64 * 1024 * 1024,
				Ulimits:     []domain.Ulimit{{Name: "nofile", Soft: 1024, Hard: 2048}},
				CapDrop:     []string{"NET_RAW"},
				SecurityOpt: []string{"no-new-privileges"},
			},
			wantErr: false,
		},
		{
			name: "extra_hosts missing colon",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				ExtraHosts: []string{"host.local"},
			},
			wantErr: true,
		},
		{
			name: "extra_hosts empty host",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				ExtraHosts: []string{":203.0.113.1"},
			},
			wantErr: true,
		},
		{
			name: "extra_hosts invalid ip",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				ExtraHosts: []string{"host.local:not-an-ip"},
			},
			wantErr: true,
		},
		{
			name: "extra_hosts valid ipv6",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				ExtraHosts: []string{"host.local:2001:db8::1"},
			},
			wantErr: false,
		},
		{
			name: "logging without driver",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				Logging: &domain.LoggingSpec{},
			},
			wantErr: true,
		},
		{
			name: "tmpfs relative path",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				Tmpfs: map[string]string{"tmp": "size=64m"},
			},
			wantErr: true,
		},
		{
			name: "valid dns/dns_search/extra_hosts/logging/tmpfs/sysctls",
			req: domain.DeployRequest{
				Image: "nginx", Name: "test",
				MemoryLimit: 1024, CPULimit: 0.5,
				DNS:        []string{"1.1.1.1"},
				DNSSearch:  []string{"example.com"},
				ExtraHosts: []string{"host.local:203.0.113.1"},
				Logging:    &domain.LoggingSpec{Driver: "json-file", Options: map[string]string{"max-size": "10m"}},
				Tmpfs:      map[string]string{"/tmp": "size=64m"},
				Sysctls:    map[string]string{"net.core.somaxconn": "1024"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRequest(tt.req, tt.portRange, testVolumeBase)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestBuildSpec(t *testing.T) {
	req := domain.DeployRequest{
		Image:       "nginx",
		Name:        "test",
		Volumes:     []domain.VolumeBind{{VolumeName: "custom_vol", BindPath: "/mnt"}},
		Ports:       []domain.PortMapping{{HostPort: 8080, ContainerPort: 80}},
		MemoryLimit: 1024,
		CPULimit:    0.5,
		Command:     []string{"ls"},
	}

	spec := buildSpec(req, testVolumeBase)

	assert.Equal(t, "nginx", spec.Image)
	assert.Equal(t, []string{"ls"}, spec.Command)
	assert.Len(t, spec.Volumes, 1)
	assert.Equal(t, "custom_vol", spec.Volumes[0].VolumeName)
	assert.Len(t, spec.Ports, 1)
	assert.Equal(t, 8080, spec.Ports[0].HostPort)
	assert.Equal(t, 80, spec.Ports[0].ContainerPort)
}

func TestBuildSpec_HostVolume(t *testing.T) {
	req := domain.DeployRequest{
		Image: "nginx",
		Name:  "test",
		Volumes: []domain.VolumeBind{
			{Type: "host", HostPath: "/data", BindPath: "/var/lib/data"},
		},
		MemoryLimit: 1024,
		CPULimit:    0.5,
	}

	spec := buildSpec(req, testVolumeBase)

	assert.Len(t, spec.Volumes, 1)
	assert.Equal(t, filepath.Clean(filepath.Join(testVolumeBase, "/data")), spec.Volumes[0].HostPath)
	assert.Equal(t, "/var/lib/data", spec.Volumes[0].BindPath)
}
