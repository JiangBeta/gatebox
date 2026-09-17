// Package component 提供组件运行时的统一抽象：Descriptor、可选能力接口与注册表。
//
// 设计见 docs/architecture.md §3 与 ADR-028。P0 仅定义类型与接口，实现于 P1。
package component

import (
	"context"
	"io"
)

// Kind 组件/插件形态。
type Kind string

const (
	KindCore        Kind = "core"         // 核心组件（caddy/acme/docker/...）
	KindCaddyModule Kind = "caddy-module" // 插件：替换 caddy 制品 + 注入片段
	KindProcess     Kind = "process"      // 插件：独立进程
	KindConfigOnly  Kind = "config-only"  // 插件：只注入配置
)

// Source 制品源（ADR-027）。
type Source struct {
	Channel string // official | custom | system
	Index   string // channel=custom 时的索引地址
}

// Descriptor 组件的静态元数据（UI 与注册表都读它）。
type Descriptor struct {
	ID             string
	Name           string
	Summary        string
	Kind           Kind
	Tags           []string // 商店标签，如「独立进程」「Caddy插件」
	Source         Source
	Provision      string   // managed | attached
	Runtime        string   // manage | observe
	Upgrade        string   // replace | system | none
	Capabilities   []string // runnable|health|config|upgradable|logs|operable
	Tier           string   // core | optional
	DefaultEnabled bool
	Removable      bool
	Bundled        bool

	// v4：功能 + 四契约（ADR-040 / ADR-041 §3）。
	// 这些字段不加 json tag，故 /api/v1/components 输出保持原样（零回归）；
	// 新端点 /api/v1/descriptors 用独立 DTO 以 camelCase 输出。
	Functions  []Function
	Config     []ConfigField
	Consumes   []InfoPort
	Produces   []InfoPort
	Observable Observability
}

// Status 运行态快照。
type Status struct {
	State   string // running | stopped | error | unknown
	Healthy bool
	Message string
	Metrics map[string]any
}

// VersionInfo 版本信息（latest 相对 Source 通道解析）。
type VersionInfo struct {
	Current         string
	Latest          string
	UpdateAvailable bool
}

// Operation 声明式操作（process 形态）。
type Operation struct {
	ID       string
	Label    string
	ReadOnly bool
}

// Result 操作结果。
type Result struct {
	Output string
	Data   map[string]any
}

// Component 是所有组件的最小契约。
type Component interface {
	Descriptor() Descriptor
}

// 可选能力接口：实现哪个具备哪个能力，UI 由 Descriptor.Capabilities 驱动（ADR-028）。
type (
	Runnable interface {
		Start(context.Context) error
		Stop(context.Context) error
		Restart(context.Context) error
	}
	Health interface {
		Status(context.Context) (Status, error)
	}
	Configurable interface {
		ReadConfig(context.Context) ([]byte, error)
		WriteConfig(context.Context, []byte) error
		Reload(context.Context) error
	}
	Upgradable interface {
		CheckUpdate(context.Context) (VersionInfo, error)
		Upgrade(context.Context, string) error
		Rollback(context.Context) error
	}
	Loggable interface {
		Logs(context.Context, map[string]string) (io.ReadCloser, error)
	}
	Operable interface {
		Operations() []Operation
		Operate(context.Context, string, map[string]string) (Result, error)
	}
)

// Registry 组件注册表。
type Registry interface {
	Get(id string) (Component, bool)
	List() []Descriptor
}
