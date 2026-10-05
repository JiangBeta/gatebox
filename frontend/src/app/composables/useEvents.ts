import { ref } from 'vue'
import { subscribeEvents } from '@/api/events'
import type { ObserveEvent, Run } from '@/shared/observe'

/**
 * 单一 SSE 订阅（应用级单例）：把 state/activity/run 事件分发到响应式状态，
 * 并把事件转播给其他订阅者（顶部横幅、任务角标等）。
 * 避免每个组件各开一条 EventSource。
 */
const states = ref<Record<string, string>>({})
const activities = ref<Record<string, { task?: string; step?: string }>>({})
const runSteps = ref<Record<string, string>>({})
const activeRun = ref<Run | null>(null)
let started = false

type RunListener = (runId: string | null, state: string) => void
const runListeners = new Set<RunListener>()

export function useEvents() {
  if (!started) {
    started = true
    subscribeEvents((kind, ev) => {
      if (kind === 'state' && ev.component) {
        const data = ev.data as { state?: string } | undefined
        states.value = { ...states.value, [ev.component]: data?.state ?? 'unknown' }
      } else if (kind === 'activity' && ev.component) {
        const data = ev.data as { task?: string; step?: string } | undefined
        activities.value = { ...activities.value, [ev.component]: { task: data?.task, step: data?.step } }
      } else if (kind === 'run') {
        const data = ev.data as Record<string, unknown> | undefined
        if (ev.component) {
          // 单个 Step：标记该组件的执行状态。
          const state = String(data?.state ?? '')
          runSteps.value = { ...runSteps.value, [ev.component]: state }
        } else if (data?.state === 'running') {
          // 新一轮 run 开始：清空高亮。
          runSteps.value = {}
          activeRun.value = null
        } else if (Array.isArray(data?.steps)) {
          // run 结束：保留高亮直到下一次 run。
          activeRun.value = data as unknown as Run
        }
        notifyRun(String(ev.runId ?? ''), String(data?.state ?? ''))
      }
    })
  }
  return { states, activities, runSteps, activeRun, onRun }
}

/**
 * onRun 订阅 run 生命周期事件（开始 / 结束），返回退订函数。
 *
 * 供任务角标重拉列表用。透传事件而非轮询：轮询会在无任务时空转，
 * 而 run 的开始与结束本来就有事件可挂。
 */
export function onRun(fn: RunListener): () => void {
  runListeners.add(fn)
  return () => { runListeners.delete(fn) }
}

function notifyRun(runId: string, state: string) {
  for (const fn of runListeners) {
    try {
      fn(runId, state)
    } catch {
      /* 单个订阅者出错不影响其他订阅者 */
    }
  }
}