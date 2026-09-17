// Package observe 提供 v4 的观测聚合与事件总线（ADR-041 §7）。
package observe

import "encoding/json"

// Kind 事件类别。
type Kind string

const (
	KindState    Kind = "state"    // 组件 state 变化
	KindActivity Kind = "activity" // 组件 activity 变化
	KindRun      Kind = "run"      // Run/Step 推进（调和器写入）
)

// Event 观测事件（SSE 载荷）。
type Event struct {
	Seq       uint64          `json:"seq"`
	Kind      Kind            `json:"kind"`
	Component string          `json:"component,omitempty"`
	RunID     string          `json:"runId,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	At        int64           `json:"at"`
}
