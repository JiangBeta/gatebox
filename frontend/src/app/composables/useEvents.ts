import { ref } from 'vue'
import { subscribeEvents } from '@/api/events'
import type { ObserveEvent } from '@/shared/observe'

/**
 * 单一 SSE 订阅（应用级单例）：把 state/activity/run 事件分发到响应式状态。
 * 避免每个组件各开一条 EventSource。
 */
const states = ref<Record<string, string>>({})
const activities = ref<Record<string, { task?: string; step?: string }>>({})
const lastRun = ref<ObserveEvent | null>(null)
let started = false

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
        lastRun.value = { ...(ev as ObserveEvent) }
      }
    })
  }
  return { states, activities, lastRun }
}
