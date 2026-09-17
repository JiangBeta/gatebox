// Package reconcile 提供 v4 的声明式调和：意图 + 依赖图驱动的调和器 + Run/Step。
//
// 设计见 docs/v4/L3-01-reconcile.md；依赖方向 reconcile → {graph, component, observe, repository}。
package reconcile

// Intent 意图：只声明某事实发生变化，不含如何传播。
type Intent struct {
	Op   string `json:"op"`   // add | update | delete
	Kind string `json:"kind"` // 事实类别（service/domain/credential/port/variable/fragment/compose）
	ID   string `json:"id"`
}

// Step 单个组件的一次调和动作。
type Step struct {
	Component string   `json:"component"`
	Action    string   `json:"action"`
	State     string   `json:"state"` // pending | running | success | error | degraded | skipped
	Detail    string   `json:"detail,omitempty"`
	StartedAt int64    `json:"startedAt,omitempty"`
	EndedAt   int64    `json:"endedAt,omitempty"`
	Events    []string `json:"events,omitempty"`
}

// Run 一次调和的执行轨迹。
type Run struct {
	ID        string `json:"id"`
	Trigger   string `json:"trigger"` // intent | periodic | manual
	Intent    string `json:"intent,omitempty"`
	State     string `json:"state"` // running | success | degraded | error
	StartedAt int64  `json:"startedAt"`
	EndedAt   int64  `json:"endedAt,omitempty"`
	Steps     []Step `json:"steps"`
}
