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
}
