import type { ObserveEvent } from '@/shared/observe'

/**
 * 订阅观测事件流（SSE）。返回退订函数。
 * 事件 kind 由 SSE `event:` 字段给出，data 为 ObserveEvent JSON（不含 kind）。
 */
export function subscribeEvents(onEvent: (kind: string, ev: Partial<ObserveEvent>) => void): () => void {
  const es = new EventSource('/api/v1/events')
  const kinds = ['state', 'activity', 'run']
  for (const kind of kinds) {
    es.addEventListener(kind, (raw) => {
      const msg = raw as MessageEvent
      try {
        onEvent(kind, JSON.parse(msg.data) as Partial<ObserveEvent>)
      } catch {
        /* 忽略坏帧 */
      }
    })
  }
  return () => es.close()
}
