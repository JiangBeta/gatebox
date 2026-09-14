package client

import "time"

// --- 容器 ---

// Container 容器列表项(GET /containers/json)。仅映射 UI 需要的字段。
type Container struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"` // 带前导斜杠,如 "/jellyfin"
	Image   string            `json:"Image"`
	ImageID string            `json:"ImageID"`
	Command string            `json:"Command"`
	Created int64             `json:"Created"` // Unix 秒
	State   string            `json:"State"`   // running / exited / created / paused ...
	Status  string            `json:"Status"`  // 人类可读,如 "Up 6 days (healthy)"
	Ports   []Port            `json:"Ports"`
	Labels  map[string]string `json:"Labels"`

	NetworkSettings *struct {
		Networks map[string]EndpointSettings `json:"Networks"`
	} `json:"NetworkSettings,omitempty"`
}

// Name 返回去掉前导斜杠的主容器名。
func (c *Container) Name() string {
	if len(c.Names) == 0 {
		return ""
	}
	n := c.Names[0]
	if len(n) > 0 && n[0] == '/' {
		return n[1:]
	}
	return n
}

// CreatedAt 创建时间。
func (c *Container) CreatedAt() time.Time { return time.Unix(c.Created, 0) }

// Compose 相关 label 的键。这三者构成容器与编排的关联(ADR-015/016)。
const (
	LabelComposeProject     = "com.docker.compose.project"
	LabelComposeService     = "com.docker.compose.service"
	LabelComposeConfigFiles = "com.docker.compose.project.config_files"
)

// ComposeProject 返回容器所属的 compose 项目名;非编排容器返回空串。
func (c *Container) ComposeProject() string { return c.Labels[LabelComposeProject] }

// ComposeService 返回容器在 compose 中的服务名;非编排容器返回空串。
func (c *Container) ComposeService() string { return c.Labels[LabelComposeService] }

// Port 端口映射。PublicPort 为 0 表示未映射到宿主机。
type Port struct {
	IP          string `json:"IP,omitempty"`
	PrivatePort uint16 `json:"PrivatePort"`
	PublicPort  uint16 `json:"PublicPort,omitempty"`
	Type        string `json:"Type"` // tcp / udp / sctp
}

// EndpointSettings 容器在某个网络中的接入信息。
type EndpointSettings struct {
	NetworkID   string   `json:"NetworkID"`
	EndpointID  string   `json:"EndpointID"`
	Gateway     string   `json:"Gateway"`
	IPAddress   string   `json:"IPAddress"`
	IPPrefixLen int      `json:"IPPrefixLen"`
	MacAddress  string   `json:"MacAddress"`
	Aliases     []string `json:"Aliases"`
}

// ContainerDetail 容器详情(GET /containers/{id}/json)。
type ContainerDetail struct {
	ID      string          `json:"Id"`
	Name    string          `json:"Name"` // 带前导斜杠
	Created time.Time       `json:"Created"`
	Path    string          `json:"Path"`
	Args    []string        `json:"Args"`
	State   ContainerState  `json:"State"`
	Image   string          `json:"Image"`
	Config  ContainerConfig `json:"Config"`

	HostConfig *HostConfig `json:"HostConfig,omitempty"`

	NetworkSettings *struct {
		Networks map[string]EndpointSettings `json:"Networks"`
	} `json:"NetworkSettings,omitempty"`

	Mounts []MountPoint `json:"Mounts"`
}

// TrimmedName 返回去掉前导斜杠的容器名。
func (d *ContainerDetail) TrimmedName() string {
	if len(d.Name) > 0 && d.Name[0] == '/' {
		return d.Name[1:]
	}
	return d.Name
}

// HostConfig 容器的宿主机侧配置。
type HostConfig struct {
	NetworkMode   string `json:"NetworkMode"`
	RestartPolicy struct {
		Name              string `json:"Name"`
		MaximumRetryCount int    `json:"MaximumRetryCount"`
	} `json:"RestartPolicy"`
	Binds        []string                 `json:"Binds"`
	PortBindings map[string][]PortBinding `json:"PortBindings"`
	Privileged   bool                     `json:"Privileged"`

	// LogConfig 该容器的日志驱动,可覆盖 daemon 默认值。
	// 判断某个容器的日志能否回读必须看这里,而非只看 daemon 的全局设置(docs §5.5)。
	LogConfig struct {
		Type   string            `json:"Type"`
		Config map[string]string `json:"Config"`
	} `json:"LogConfig"`
}

// LogDriver 返回该容器实际使用的日志驱动;未显式设置时返回空串(表示随 daemon 默认)。
func (d *ContainerDetail) LogDriver() string {
	if d.HostConfig == nil {
		return ""
	}
	return d.HostConfig.LogConfig.Type
}

// ContainerState 容器运行状态。
type ContainerState struct {
	Status     string    `json:"Status"`
	Running    bool      `json:"Running"`
	Paused     bool      `json:"Paused"`
	Restarting bool      `json:"Restarting"`
	OOMKilled  bool      `json:"OOMKilled"`
	Dead       bool      `json:"Dead"`
	Pid        int       `json:"Pid"`
	ExitCode   int       `json:"ExitCode"`
	Error      string    `json:"Error"`
	StartedAt  time.Time `json:"StartedAt"`
	FinishedAt time.Time `json:"FinishedAt"`
	Health     *Health   `json:"Health,omitempty"`
}

// Uptime 返回容器已运行时长;未运行时返回 0。
func (s *ContainerState) Uptime() time.Duration {
	if !s.Running || s.StartedAt.IsZero() {
		return 0
	}
	return time.Since(s.StartedAt)
}

// Health 健康检查状态。
type Health struct {
	Status        string `json:"Status"` // starting / healthy / unhealthy / none
	FailingStreak int    `json:"FailingStreak"`
}

// ContainerConfig 容器创建时的配置。
type ContainerConfig struct {
	Hostname   string            `json:"Hostname"`
	WorkingDir string            `json:"WorkingDir,omitempty"`
	User       string            `json:"User"`
	Env        []string          `json:"Env"`
	Cmd        []string          `json:"Cmd"`
	Image      string            `json:"Image"`
	Labels     map[string]string `json:"Labels"`
	Entrypoint []string          `json:"Entrypoint"`

	// Tty 决定日志/attach 流是否为多路复用格式。
	// 为 true 时流是原始字节、没有 8 字节帧头,误用 FrameReader 会解析出乱码。
	Tty          bool `json:"Tty"`
	OpenStdin    bool `json:"OpenStdin"`
	AttachStdout bool `json:"AttachStdout"`
	AttachStderr bool `json:"AttachStderr"`
}

// PortBinding 宿主机侧的端口绑定。
type PortBinding struct {
	HostIP   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// MountPoint 容器的挂载点。
type MountPoint struct {
	Type        string `json:"Type"` // bind / volume / tmpfs
	Name        string `json:"Name,omitempty"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	Driver      string `json:"Driver,omitempty"`
	Mode        string `json:"Mode"`
	RW          bool   `json:"RW"`
}

// --- 统计 ---

// Stats 容器资源统计(GET /containers/{id}/stats)。
type Stats struct {
	Read        time.Time               `json:"read"`
	PreRead     time.Time               `json:"preread"`
	CPUStats    CPUStats                `json:"cpu_stats"`
	PreCPUStats CPUStats                `json:"precpu_stats"`
	MemoryStats MemoryStats             `json:"memory_stats"`
	Networks    map[string]NetworkStats `json:"networks"`
	BlkioStats  BlkioStats              `json:"blkio_stats"`
	PidsStats   struct {
		Current uint64 `json:"current"`
		Limit   uint64 `json:"limit"`
	} `json:"pids_stats"`
}

// CPUStats CPU 累计用量。差值计算需要前后两个采样点。
type CPUStats struct {
	CPUUsage struct {
		TotalUsage        uint64   `json:"total_usage"`
		PerCPUUsage       []uint64 `json:"percpu_usage,omitempty"` // cgroup v2 下通常缺失
		UsageInKernelmode uint64   `json:"usage_in_kernelmode"`
		UsageInUsermode   uint64   `json:"usage_in_usermode"`
	} `json:"cpu_usage"`
	SystemUsage    uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint32 `json:"online_cpus"`
	ThrottlingData struct {
		Periods          uint64 `json:"periods"`
		ThrottledPeriods uint64 `json:"throttled_periods"`
		ThrottledTime    uint64 `json:"throttled_time"`
	} `json:"throttling_data"`
}

// MemoryStats 内存用量。
//
// Stats 子字段在 cgroup v1 与 v2 下键名不同,故用 map 承载:
//   - cgroup v1:total_inactive_file、cache、rss …
//   - cgroup v2:inactive_file、active_file、file、anon …
type MemoryStats struct {
	Usage    uint64            `json:"usage"`
	MaxUsage uint64            `json:"max_usage,omitempty"`
	Limit    uint64            `json:"limit"`
	Stats    map[string]uint64 `json:"stats"`
}

// NetworkStats 单个网络接口的累计流量。
type NetworkStats struct {
	RxBytes   uint64 `json:"rx_bytes"`
	RxPackets uint64 `json:"rx_packets"`
	RxErrors  uint64 `json:"rx_errors"`
	RxDropped uint64 `json:"rx_dropped"`
	TxBytes   uint64 `json:"tx_bytes"`
	TxPackets uint64 `json:"tx_packets"`
	TxErrors  uint64 `json:"tx_errors"`
	TxDropped uint64 `json:"tx_dropped"`
}

// BlkioStats 块设备 IO。
type BlkioStats struct {
	IoServiceBytesRecursive []BlkioStatEntry `json:"io_service_bytes_recursive"`
}

// BlkioStatEntry 单条块设备 IO 记录。
type BlkioStatEntry struct {
	Major uint64 `json:"major"`
	Minor uint64 `json:"minor"`
	Op    string `json:"op"` // read / write / sync / async / total
	Value uint64 `json:"value"`
}

// CPUPercent 按 `docker stats` 的口径计算 CPU 占用率(百分比,可超过 100)。
//
//	(Δtotal_usage / Δsystem_cpu_usage) × online_cpus × 100
//
// 首个采样点(PreCPUStats 为空)返回 0——此时无差值可算。
func (s *Stats) CPUPercent() float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemUsage) - float64(s.PreCPUStats.SystemUsage)
	if cpuDelta <= 0 || sysDelta <= 0 {
		return 0
	}
	cpus := float64(s.CPUStats.OnlineCPUs)
	if cpus == 0 {
		cpus = float64(len(s.CPUStats.CPUUsage.PerCPUUsage))
	}
	if cpus == 0 {
		cpus = 1
	}
	return cpuDelta / sysDelta * cpus * 100
}

// MemoryUsage 按 `docker stats` 的口径返回扣除文件缓存后的内存用量(字节)。
//
// 与 docker CLI 的 calculateMemUsageUnixNoCache 保持一致:
// cgroup v1 扣 total_inactive_file,cgroup v2 扣 inactive_file。
//
// 对齐 CLI 而非 ctop,是因为用户会开终端敲 `docker stats` 与界面对照——
// 口径不一致就是 bug 工单(docs/docker.md §3.1)。
func (s *Stats) MemoryUsage() uint64 {
	usage := s.MemoryStats.Usage
	// cgroup v1
	if v, ok := s.MemoryStats.Stats["total_inactive_file"]; ok && v < usage {
		return usage - v
	}
	// cgroup v2
	if v, ok := s.MemoryStats.Stats["inactive_file"]; ok && v < usage {
		return usage - v
	}
	return usage
}

// MemoryPercent 内存占用率(相对 limit)。limit 为 0 时返回 0。
func (s *Stats) MemoryPercent() float64 {
	if s.MemoryStats.Limit == 0 {
		return 0
	}
	return float64(s.MemoryUsage()) / float64(s.MemoryStats.Limit) * 100
}

// NetworkIO 汇总所有接口的收发字节数。
func (s *Stats) NetworkIO() (rx, tx uint64) {
	for _, n := range s.Networks {
		rx += n.RxBytes
		tx += n.TxBytes
	}
	return rx, tx
}

// BlockIO 汇总块设备的读写字节数。
func (s *Stats) BlockIO() (read, write uint64) {
	for _, e := range s.BlkioStats.IoServiceBytesRecursive {
		switch e.Op {
		case "read", "Read":
			read += e.Value
		case "write", "Write":
			write += e.Value
		}
	}
	return read, write
}
