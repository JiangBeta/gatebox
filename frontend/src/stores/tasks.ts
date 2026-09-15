/**
 * 全局后台任务中心（前端内存态）。
 *
 * 背景：写入类操作（创建/保存代理）会触发后端 reloadCaddy，HTTPS 域名首次
 * 还可能走 acme.sh DNS-01 签发（传播可能数分钟）。这类请求不应阻塞用户操作，
 * 统一以「后台任务」形式执行：调用方立即返回，完成状态在右上角任务中心展示。
 */
import { reactive, computed } from 'vue'

export type TaskStatus = 'running' | 'done' | 'error'

export interface TaskItem {
  id: string
  /** 任务名，如「创建代理「jellyfin」」。 */
  name: string
  status: TaskStatus
  /** 开始时间（毫秒时间戳）。 */
  startedAt: number
  /** 结束时间（毫秒时间戳），运行中为空。 */
  finishedAt?: number
  /** 失败原因，仅 status === 'error' 时有值。 */
  error?: string
}

/** 最多保留的任务数，超出后丢弃最早的已结束任务。 */
const MAX_TASKS = 50

const state = reactive<{ tasks: TaskItem[] }>({ tasks: [] })
let seq = 0

function trim() {
  if (state.tasks.length <= MAX_TASKS) return
  for (let i = state.tasks.length - 1; i >= 0 && state.tasks.length > MAX_TASKS; i--) {
    if (state.tasks[i].status !== 'running') state.tasks.splice(i, 1)
  }
}

/** 新增一个运行中任务，返回任务 id。 */
export function addTask(name: string): string {
  const id = `task-${Date.now()}-${++seq}`
  state.tasks.unshift({ id, name, status: 'running', startedAt: Date.now() })
  trim()
  return id
}

/** 结束任务：无 error 记已完成，有 error 记失败。 */
export function finishTask(id: string, error?: unknown): void {
  const t = state.tasks.find((x) => x.id === id)
  if (!t) return
  t.finishedAt = Date.now()
  if (error) {
    t.status = 'error'
    t.error = error instanceof Error ? error.message : String(error)
  } else {
    t.status = 'done'
  }
}

export function removeTask(id: string): void {
  const i = state.tasks.findIndex((x) => x.id === id)
  if (i >= 0) state.tasks.splice(i, 1)
}

/** 清除所有已结束（完成/失败）的任务。 */
export function clearFinishedTasks(): void {
  for (let i = state.tasks.length - 1; i >= 0; i--) {
    if (state.tasks[i].status !== 'running') state.tasks.splice(i, 1)
  }
}

export interface TaskHandle {
  id: string
  /** 完成时 resolve；失败时 reject（调用方需 catch，任务中心亦已记录失败）。 */
  done: Promise<void>
}

/** 以后台任务形式执行异步操作：立即返回，不阻塞调用方。 */
export function runTask(name: string, fn: () => Promise<void>): TaskHandle {
  const id = addTask(name)
  const done = Promise.resolve()
    .then(fn)
    .then(
      () => { finishTask(id) },
      (e) => { finishTask(id, e); throw e },
    )
  return { id, done }
}

export function useTaskStore() {
  const tasks = computed(() => state.tasks)
  const runningCount = computed(() => state.tasks.filter((t) => t.status === 'running').length)
  return { tasks, runningCount, clearFinishedTasks, removeTask }
}
