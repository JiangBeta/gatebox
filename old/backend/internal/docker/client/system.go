package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Info daemon 的运行时信息(GET /info)。
//
// 这里的字段是 GateBox 多处功能的**运行时真值来源**:
//   - LoggingDriver  → 判断容器日志能否回读(docs §5.5)
//   - CgroupVersion  → 选择内存口径的 v1/v2 分支(docs §3.1)
//   - Architecture   → 拉取镜像时的默认架构(docs §3.3)
//   - RegistryConfig → daemon.json 不可写时(NixOS/OpenWrt)只读展示镜像加速器(docs §3.3.1)
//   - LiveRestore    → 设置页的开关状态
type Info struct {
	ID                string `json:"ID"`
	Name              string `json:"Name"`
	ServerVersion     string `json:"ServerVersion"`
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	ContainersPaused  int    `json:"ContainersPaused"`
	ContainersStopped int    `json:"ContainersStopped"`
	Images            int    `json:"Images"`

	Driver        string `json:"Driver"`        // 存储驱动
	LoggingDriver string `json:"LoggingDriver"` // 默认日志驱动
	CgroupDriver  string `json:"CgroupDriver"`
	CgroupVersion string `json:"CgroupVersion"` // "1" / "2"

	OperatingSystem string `json:"OperatingSystem"`
	OSType          string `json:"OSType"`
	Architecture    string `json:"Architecture"` // uname 风格,如 x86_64
	NCPU            int    `json:"NCPU"`
	MemTotal        int64  `json:"MemTotal"`

	RegistryConfig     *RegistryConfig `json:"RegistryConfig,omitempty"`
	LiveRestoreEnabled bool            `json:"LiveRestoreEnabled"`
	Warnings           []string        `json:"Warnings,omitempty"`
}

// RegistryConfig daemon 的镜像仓库配置(只读视图)。
type RegistryConfig struct {
	Mirrors               []string `json:"Mirrors"`
	InsecureRegistryCIDRs []string `json:"InsecureRegistryCIDRs"`
}

// CgroupV2 报告宿主机是否使用 cgroup v2。
func (i *Info) CgroupV2() bool { return i.CgroupVersion == "2" }

// LogsReadable 报告默认日志驱动是否支持回读容器日志。
//
// json-file / local / journald 支持;syslog / fluentd / gelf / awslogs 等不支持,
// 对后者调用 logs 端点 daemon 会直接报错(docs §5.5)。
// 注意:单个容器可覆盖 daemon 默认驱动,精确判断需读该容器的 HostConfig.LogConfig。
func (i *Info) LogsReadable() bool { return LogDriverReadable(i.LoggingDriver) }

// LogDriverReadable 报告指定日志驱动是否支持回读。
func LogDriverReadable(driver string) bool {
	switch driver {
	case "json-file", "local", "journald", "":
		return true
	}
	return false
}

// ImagePlatform 返回适用于镜像拉取的平台标识(如 linux/amd64)。
//
// 必须做名称转换:docker info 报告的是 uname 风格的 x86_64 / aarch64,
// 而镜像 manifest 用的是 OCI 风格的 amd64 / arm64,直接拼会拉不到镜像。
func (i *Info) ImagePlatform() string {
	os := i.OSType
	if os == "" {
		os = "linux"
	}
	return os + "/" + NormalizeArch(i.Architecture)
}

// NormalizeArch 把 uname 风格的架构名转换为 OCI 镜像的架构名。
func NormalizeArch(arch string) string {
	switch strings.ToLower(arch) {
	case "x86_64", "x86-64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "armv7l", "armhf", "arm":
		return "arm"
	case "armv6l":
		return "arm"
	case "i386", "i686", "x86":
		return "386"
	case "riscv64":
		return "riscv64"
	case "ppc64le":
		return "ppc64le"
	case "s390x":
		return "s390x"
	}
	return arch
}

// SystemInfo 获取 daemon 信息。
func (c *Client) SystemInfo(ctx context.Context) (*Info, error) {
	var out Info
	if err := c.getJSON(ctx, "/info", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Version daemon 与 API 的版本信息(GET /version)。
type Version struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
	GitCommit     string `json:"GitCommit"`
	GoVersion     string `json:"GoVersion"`
	Os            string `json:"Os"`
	Arch          string `json:"Arch"` // 此处已是 OCI 风格(amd64),与 Info.Architecture 不同
	KernelVersion string `json:"KernelVersion"`
}

// SystemVersion 获取版本信息。
func (c *Client) SystemVersion(ctx context.Context) (*Version, error) {
	var out Version
	if err := c.getJSON(ctx, "/version", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DiskUsage 磁盘占用汇总(GET /system/df)。
type DiskUsage struct {
	LayersSize int64       `json:"LayersSize"`
	Images     []Image     `json:"Images"`
	Containers []Container `json:"Containers"`
	Volumes    []Volume    `json:"Volumes"`
}

// SystemDiskUsage 获取磁盘占用。
//
// 这个端点在镜像/卷较多时较慢(daemon 需遍历统计),不应放在高频轮询路径上。
func (c *Client) SystemDiskUsage(ctx context.Context) (*DiskUsage, error) {
	var out DiskUsage
	if err := c.getJSON(ctx, "/system/df", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Event daemon 事件(GET /events)。
type Event struct {
	Type   string `json:"Type"`   // container / image / network / volume / daemon
	Action string `json:"Action"` // start / die / destroy / pull ...
	Actor  struct {
		ID         string            `json:"ID"`
		Attributes map[string]string `json:"Attributes"`
	} `json:"Actor"`
	Scope    string `json:"scope"`
	Time     int64  `json:"time"`
	TimeNano int64  `json:"timeNano"`
}

// At 返回事件时间。
func (e *Event) At() time.Time {
	if e.TimeNano > 0 {
		return time.Unix(0, e.TimeNano)
	}
	return time.Unix(e.Time, 0)
}

// SystemEvents 订阅 daemon 事件流,调用方负责 Close。
//
// 用途:容器状态变化时让 UI 即时刷新,避免纯轮询的延迟。
// 注意 ADR-016 已选择「127.0.0.1:映射端口」寻址,故**不需要**靠 events
// 监听容器重建来重新生成 Caddyfile。
func (c *Client) SystemEvents(ctx context.Context, since, until time.Time, filters Filters) (io.ReadCloser, error) {
	q := url.Values{}
	if !since.IsZero() {
		q.Set("since", strconv.FormatInt(since.Unix(), 10))
	}
	if !until.IsZero() {
		q.Set("until", strconv.FormatInt(until.Unix(), 10))
	}
	if err := filters.apply(q); err != nil {
		return nil, err
	}
	return c.stream(ctx, http.MethodGet, "/events", q, nil)
}
