package handler

// reconcileTrigger 由 server 注入的调和触发钩子（v4，L3）。
//
// 为空时不做任何事，保持既有行为（不影响不启用调和器的场景）。
var reconcileTrigger func(kind, id string)

// SetReconcileTrigger 注入调和触发回调。
func SetReconcileTrigger(fn func(kind, id string)) { reconcileTrigger = fn }

// triggerReconcile 在事实变更后触发一次调和（修复此前不触发重载的缺口）。
func triggerReconcile(kind, id string) {
	if reconcileTrigger != nil {
		reconcileTrigger(kind, id)
	}
}
