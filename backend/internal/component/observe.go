package component

import "context"

// 本文件定义 v4 的观测契约运行时面与调和接口（ADR-041 §5/§7）。

// Activity 组件当前活动（观测契约的动态部分）。
type Activity struct {
	Task     string  `json:"task,omitempty"`     // 当前任务，如 "issue-cert"
	Step     string  `json:"step,omitempty"`     // 当前步骤，如 "waiting-dns"
	Progress float64 `json:"progress,omitempty"` // 0..1；-1 表示不确定
	Actor    string  `json:"actor,omitempty"`    // 执行者（进程/组件/run）
	Since    int64   `json:"since,omitempty"`    // Unix 秒
}

// ActivityProvider 可选接口：实现则具备 activity 观测。
type ActivityProvider interface {
	Activity(context.Context) (Activity, error)
}

// FactView 调和输入中的只读事实视图。
type FactView struct {
	Kind   string
	ID     string
	Info   InfoType
	Origin string
	Value  any
}

// ReconcileInput 调和输入：期望态事实快照 + 本次受影响的信息类型。
type ReconcileInput struct {
	Facts    []FactView
	Affected []InfoType
}

// ReconcileResult 单个组件的调和结果。
type ReconcileResult struct {
	State  string // success | degraded | skipped
	Detail string
	Events []string
}

// Reconciler 可选接口：实现则可被调和器驱动（ADR-041 §5）。
// 约定：Reconcile 必须幂等，可安全重放。
type Reconciler interface {
	Reconcile(context.Context, ReconcileInput) (ReconcileResult, error)
}
