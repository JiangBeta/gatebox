package model

import "time"

// PluginState 插件安装/启用状态（持久化于 BoltDB）。
// 状态机：available → installed → enabled/disabled；任一环节失败 → error（ADR-029）。
type PluginState struct {
	ID          string            `json:"id"`
	State       string            `json:"state"`
	Version     string            `json:"version"`
	Channel     string            `json:"channel"`
	Config      map[string]string `json:"config,omitempty"`
	Message     string            `json:"message,omitempty"`
	InstalledAt time.Time         `json:"installedAt"`
	UpdatedAt   time.Time         `json:"updatedAt"`
	// Kind 插件形态（caddy-module|process|config-only），持久化以便重启后无需在线目录即可恢复。
	Kind string `json:"kind,omitempty"`
	// Token plugin token：绑定 permissions.api scope（ADR-039 §2）。
	// 持久化于 BoltDB；对外 View 不包含（避免经 API 泄露）。
	Token string `json:"token,omitempty"`
	// Port sidecar 后端监听端口（仅 kind:process 使用，绑定 127.0.0.1）。
	Port int `json:"port,omitempty"`
}
