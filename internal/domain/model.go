package domain

// VolumeBind represents a mount point for a container.
type VolumeBind struct {
	Type       string `json:"type,omitempty"`        // "docker" (default) or "host"
	VolumeName string `json:"volume_name,omitempty"` // Docker volume name; used when type is "docker"
	HostPath   string `json:"host_path,omitempty"`   // host directory path; used when type is "host"
	BindPath   string `json:"bind_path"`             // container path
	ReadOnly   bool   `json:"read_only"`
}

// PortMapping represents a port exposed from the container to the host.
type PortMapping struct {
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"` // tcp, udp
}

// Ulimit sets a single resource limit (e.g. "nofile") inside the container.
type Ulimit struct {
	Name string `json:"name"`
	Soft int64  `json:"soft"`
	Hard int64  `json:"hard"`
}

// LoggingSpec configures the container's log driver.
type LoggingSpec struct {
	Driver  string            `json:"driver"`
	Options map[string]string `json:"options,omitempty"`
}

// HealthcheckSpec configures the container's Docker healthcheck.
type HealthcheckSpec struct {
	Test            []string `json:"test"`
	IntervalSeconds int      `json:"interval_seconds"`
	TimeoutSeconds  int      `json:"timeout_seconds"`
	Retries         int      `json:"retries"`
}

// DeployRequest is the raw input for the deploy / update use cases.
type DeployRequest struct {
	Image             string            `json:"image"`
	Name              string            `json:"name"`
	MemoryLimit       int64             `json:"memory_limit"`
	MemoryReservation int64             `json:"memory_reservation"`
	CPULimit          float64           `json:"cpu_limit"`
	RestartPolicy     string            `json:"restart_policy"`
	Env               map[string]string `json:"env"`

	Volumes     []VolumeBind     `json:"volumes,omitempty"`
	Ports       []PortMapping    `json:"ports,omitempty"`
	Command     []string         `json:"command,omitempty"`
	Entrypoint  []string         `json:"entrypoint,omitempty"`
	Networks    []string         `json:"networks,omitempty"`
	Healthcheck *HealthcheckSpec `json:"healthcheck,omitempty"`

	User            string            `json:"user,omitempty"`              // uid, name, or "user:group" to run as
	WorkingDir      string            `json:"working_dir,omitempty"`       // container working directory
	Labels          map[string]string `json:"labels,omitempty"`            // container labels
	StopSignal      string            `json:"stop_signal,omitempty"`       // signal sent to stop the container
	StopGracePeriod int               `json:"stop_grace_period,omitempty"` // seconds to wait after stop_signal before killing
	ReadOnly        bool              `json:"read_only,omitempty"`         // mount container root filesystem as read-only

	PidsLimit   int64    `json:"pids_limit,omitempty"`   // max number of processes/threads
	ShmSize     int64    `json:"shm_size,omitempty"`     // /dev/shm size, in bytes
	Ulimits     []Ulimit `json:"ulimits,omitempty"`      // resource limits (e.g. nofile)
	CapDrop     []string `json:"cap_drop,omitempty"`     // kernel capabilities to drop
	SecurityOpt []string `json:"security_opt,omitempty"` // security profile options (e.g. no-new-privileges)

	DNS        []string          `json:"dns,omitempty"`         // custom DNS servers
	DNSSearch  []string          `json:"dns_search,omitempty"`  // DNS search domains
	ExtraHosts []string          `json:"extra_hosts,omitempty"` // additional /etc/hosts entries, "host:ip" form
	Logging    *LoggingSpec      `json:"logging,omitempty"`     // log driver configuration
	Tmpfs      map[string]string `json:"tmpfs,omitempty"`       // container path -> mount options (e.g. "size=64m")
	Sysctls    map[string]string `json:"sysctls,omitempty"`     // namespaced kernel sysctls
}

// DeploySpec is the validated, processed spec passed to the container runtime.
type DeploySpec struct {
	Image         string
	Name          string
	MemoryLimit   int64
	SoftLimit     int64
	CPUNano       int64
	Env           []string
	RestartPolicy string

	Volumes     []VolumeBind
	Ports       []PortMapping
	Command     []string
	Entrypoint  []string
	Networks    []string
	Healthcheck *HealthcheckSpec

	User            string
	WorkingDir      string
	Labels          map[string]string
	StopSignal      string
	StopGracePeriod int
	ReadOnly        bool

	PidsLimit   int64
	ShmSize     int64
	Ulimits     []Ulimit
	CapDrop     []string
	SecurityOpt []string

	DNS        []string
	DNSSearch  []string
	ExtraHosts []string
	Logging    *LoggingSpec
	Tmpfs      map[string]string
	Sysctls    map[string]string
}

// DeployResult is returned after a successful deploy or update.
type DeployResult struct {
	ContainerID string
	NodeID      string
}

// ContainerSummary is a lightweight view of a container returned by List.
type ContainerSummary struct {
	ID     string
	Names  []string
	Image  string
	State  string
	Status string
	// Ports lists the host<->container port bindings currently published by
	// this container (HostPort is 0 for container-only/unpublished ports).
	Ports []PortMapping
}

// ContainerResources holds the resource limits of an existing container.
type ContainerResources struct {
	MemoryBytes int64
	NanoCPUs    int64
}

// Stats holds runtime resource metrics for a container.
type Stats struct {
	CPUPercent float64
	MemUsage   uint64
	MemLimit   uint64
	// NetworkRxBytes and NetworkTxBytes are the total bytes received/sent
	// across all network interfaces since the container started.
	NetworkRxBytes uint64
	NetworkTxBytes uint64
}

// ImageSummary is a lightweight view of a locally cached image.
type ImageSummary struct {
	ID       string
	RepoTags []string
	SizeMB   int64
}

// NodeHealth holds current node-level metrics.
type NodeHealth struct {
	NodeID     string
	CPUPercent float64
	MemFreeMB  uint64
	Status     string
}

// ContainerState represents the real-time status and health of a container.
type ContainerState struct {
	Status     string  `json:"status"` // e.g., running, exited, paused
	Running    bool    `json:"running"`
	Paused     bool    `json:"paused"`
	Restarting bool    `json:"restarting"`
	OOMKilled  bool    `json:"oom_killed"`
	Dead       bool    `json:"dead"`
	Pid        int     `json:"pid"`
	ExitCode   int     `json:"exit_code"`
	Error      string  `json:"error"`
	StartedAt  string  `json:"started_at"`
	FinishedAt string  `json:"finished_at"`
	Health     *Health `json:"health,omitempty"`
}

type Health struct {
	Status        string `json:"status"` // starting, healthy, unhealthy
	FailingStreak int    `json:"failing_streak"`
}
