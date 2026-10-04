package handler

// DeployPayload is the request body for POST /containers and PUT /containers/{name}.
type DeployPayload struct {
	Image             string            `json:"image"               example:"n8nio/n8n:latest"`
	ContainerName     string            `json:"name"                example:"n8n-alice"`
	MemoryLimit       int64             `json:"memory_limit"        example:"805306368"`
	MemoryReservation int64             `json:"memory_reservation"  example:"536870912"`
	CPULimit          float64           `json:"cpu_limit"           example:"0.5"`
	RestartPolicy     string            `json:"restart_policy"      example:"unless-stopped"`
	Env               map[string]string `json:"env"`

	Volumes     []VolumeBind  `json:"volumes"`
	Ports       []PortMapping `json:"ports"`
	Command     []string      `json:"command"`
	Entrypoint  []string      `json:"entrypoint"`
	Networks    []string      `json:"networks"`
	Healthcheck *Healthcheck  `json:"healthcheck,omitempty"`

	User            string            `json:"user,omitempty"              example:"node"`
	WorkingDir      string            `json:"working_dir,omitempty"       example:"/home/node"`
	Labels          map[string]string `json:"labels,omitempty"`
	StopSignal      string            `json:"stop_signal,omitempty"       example:"SIGTERM"`
	StopGracePeriod int               `json:"stop_grace_period,omitempty" example:"30"`
	ReadOnly        bool              `json:"read_only,omitempty"         example:"false"`

	PidsLimit   int64    `json:"pids_limit,omitempty"   example:"256"`
	ShmSize     int64    `json:"shm_size,omitempty"     example:"67108864"`
	Ulimits     []Ulimit `json:"ulimits,omitempty"`
	CapDrop     []string `json:"cap_drop,omitempty"     example:"['NET_RAW']"`
	SecurityOpt []string `json:"security_opt,omitempty" example:"['no-new-privileges']"`

	DNS        []string          `json:"dns,omitempty"         example:"['1.1.1.1']"`
	DNSSearch  []string          `json:"dns_search,omitempty"  example:"['example.com']"`
	ExtraHosts []string          `json:"extra_hosts,omitempty" example:"['host.local:203.0.113.1']"`
	Logging    *Logging          `json:"logging,omitempty"`
	Tmpfs      map[string]string `json:"tmpfs,omitempty"`
	Sysctls    map[string]string `json:"sysctls,omitempty"`
}

// Ulimit is the handler representation of domain.Ulimit.
type Ulimit struct {
	Name string `json:"name" example:"nofile"`
	Soft int64  `json:"soft" example:"1024"`
	Hard int64  `json:"hard" example:"2048"`
}

// Logging is the handler representation of domain.LoggingSpec.
type Logging struct {
	Driver  string            `json:"driver" example:"json-file"`
	Options map[string]string `json:"options,omitempty"`
}

// Healthcheck is the handler representation of domain.HealthcheckSpec.
type Healthcheck struct {
	Test            []string `json:"test"             example:"['CMD-SHELL', 'pg_isready -U postgres']"`
	IntervalSeconds int      `json:"interval_seconds" example:"10"`
	TimeoutSeconds  int      `json:"timeout_seconds"  example:"5"`
	Retries         int      `json:"retries"          example:"5"`
}

// VolumeBind is the handler representation of domain.VolumeBind.
type VolumeBind struct {
	Type       string `json:"type,omitempty"        example:"docker"`
	VolumeName string `json:"volume_name,omitempty" example:"user1_pgdata"`
	HostPath   string `json:"host_path,omitempty"   example:"alice/data"`
	BindPath   string `json:"bind_path"             example:"/var/lib/postgresql/data"`
	ReadOnly   bool   `json:"read_only"             example:"false"`
}

// PortMapping is the handler representation of domain.PortMapping.
type PortMapping struct {
	HostPort      int    `json:"host_port"      example:"5432"`
	ContainerPort int    `json:"container_port" example:"5432"`
	Protocol      string `json:"protocol"       example:"tcp"`
}

// DeployResponse is returned after a successful container deployment.
type DeployResponse struct {
	ID     string `json:"id"     example:"a1b2c3d4e5f6"`
	Node   string `json:"node"   example:"node-01"`
	Status string `json:"status" example:"deployed"`
}

// UpdateResponse is returned after a successful container update.
type UpdateResponse struct {
	ID     string `json:"id"     example:"a1b2c3d4e5f6"`
	Status string `json:"status" example:"updated"`
}

// ActionResponse is returned for restart and reset operations.
type ActionResponse struct {
	Status string `json:"status" example:"restarted"`
}

// ErrorResponse is returned when a request fails.
type ErrorResponse struct {
	Error string `json:"error" example:"invalid input: image is required"`
}

// StatsResponse holds live resource metrics for a container.
type StatsResponse struct {
	CPUUsagePercent float64 `json:"cpu_usage_percent" example:"12.5"`
	MemUsageBytes   uint64  `json:"mem_usage_bytes"   example:"134217728"`
	MemLimitBytes   uint64  `json:"mem_limit_bytes"   example:"805306368"`
	NetworkRxBytes  uint64  `json:"network_rx_bytes"  example:"10485760"`
	NetworkTxBytes  uint64  `json:"network_tx_bytes"  example:"5242880"`
}

// HealthResponse holds node-level health information.
type HealthResponse struct {
	NodeID    string  `json:"node_id"    example:"node-01"`
	CPUActual float64 `json:"cpu_actual"  example:"23.5"`
	RAMFree   uint64  `json:"ram_free"    example:"4096"`
	Status    string  `json:"status"      example:"online"`
}

// IPResponse holds the IP address of a container.
type IPResponse struct {
	IP string `json:"ip" example:"172.17.0.2"`
}

// LogsResponse holds recent stdout/stderr output for a container.
type LogsResponse struct {
	Logs string `json:"logs"`
}

// ContainerItem mirrors domain.ContainerSummary for swagger documentation.
type ContainerItem struct {
	ID     string   `json:"ID"     example:"a1b2c3d4e5f6"`
	Names  []string `json:"Names"  example:"['/n8n-alice']"`
	Image  string   `json:"Image"  example:"n8nio/n8n:latest"`
	State  string   `json:"State"  example:"running"`
	Status string   `json:"Status" example:"Up 2 hours"`
}

// NetworkPayload is the request body for POST /networks.
type NetworkPayload struct {
	Name string `json:"name" example:"my-net"`
}

// VolumePayload is the request body for POST /volumes.
type VolumePayload struct {
	Name string `json:"name" example:"my-vol"`
}

// ResourceListResponse is returned for ListNetworks and ListVolumes.
type ResourceListResponse struct {
	Resources []string `json:"resources" example:"['my-net', 'prod-db']"`
}

type PullImagePayload struct {
	Image string `json:"image" example:"nginx:latest"`
}

// WebhookDeployPayload is the request body for POST /webhook/containers/{name}/deploy.
type WebhookDeployPayload struct {
	Image string `json:"image" example:"ghcr.io/acme/app:sha-abc123"`
}

// ImageItem mirrors domain.ImageSummary for swagger documentation.
type ImageItem struct {
	ID       string   `json:"id"        example:"sha256:a1b2c3d4e5f6"`
	RepoTags []string `json:"repo_tags" example:"['nginx:latest']"`
	SizeMB   int64    `json:"size_mb"   example:"142"`
}
