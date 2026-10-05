/**
 * 全局 Run 列表（应用级单例）：顶栏任务角标与任务页共用一份数据。
 *
 * 数据源是真实 Run（GET /api/v1/runs + SSE），不是前端内存态——
 * 刷新页面/换浏览器任务仍在，任务也是「谁触发了哪次调和」的凭据。
 */
import { ref } from 'vue'
import { listRuns } from '@/api/runs'
import { onRun } from './useEvents'
import type { Run } from '@/shared/observe'

const runs = ref<Run[]>([])
const loading = ref(false)
const loaded = ref(false)
let started = false

export function useRuns() {
  if (!started) {
    started = true
    // run 开始/结束都重拉：列表里的 state 与 steps 是精简版，
    // 靠事件驱动刷新，不用轮询空转。
    onRun(() => { void reload() })
    void reload()
  }
  return { runs, loading, loaded, reload }
}

export async function reload(): Promise<void> {
  loading.value = true
  try {
    runs.value = await listRuns(50)
    loaded.value = true
  } catch {
    /* 拉不到不阻塞界面：角标保持原样，任务页自己会提示错误 */
  } finally {
    loading.value = false
  }
}

/** runningRuns 执行中的 Run（顶栏角标计数）。 */
export function runningRuns(list: Run[]): Run[] {
  return list.filter((r) => r.state === 'running')
}

/**
 * run 进度：按 step 状态推占比。
 *
 * 后端 Run 无 progress 字段（step 只有 state），所以：success/degraded 记完成、
 * running 记半程、error 记满（它已终止）。够画一条会动的进度条，不假装精确。
 */
export function runProgress(run: Run): number {
  if (run.state !== 'running') return 100
  const steps = run.steps || []
  if (!steps.length) return 0
  const done = steps.filter((s) => s.state === 'success' || s.state === 'degraded').length
  const running = steps.filter((s) => s.state === 'running').length
  return Math.min(100, Math.round(((done + running * 0.5) / steps.length) * 100))
}

/** runDetail 任务行下的进度文案：优先最近一步的 detail。 */
export function runDetail(run: Run): string {
  const steps = run.steps || []
  const last = [...steps].reverse().find((s) => s.state === 'running' || s.state === 'error')
  return last?.detail || (steps.length ? `${steps.length} 个步骤` : '')
}

/** runTitle 任务标题：意图优先，缺省用触发源。 */
export function runTitle(run: Run): string {
  return run.intent || run.trigger || '调和任务'
}