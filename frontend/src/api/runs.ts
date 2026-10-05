/** 运行记录（任务中心）。只读 + SSE 实时刷新，后端见 handler/observe.go。 */
import http from './http'
import type { Run } from '@/shared/observe'

/** 最近的运行记录，按开始时间倒序。 */
export async function listRuns(limit = 20): Promise<Run[]> {
  const { data } = await http.get<Run[] | null>(`/runs?limit=${limit}`)
  return data ?? []
}

/** 单次运行的完整轨迹（含每个 step 的 detail/events）。 */
export async function getRun(id: string): Promise<Run> {
  const { data } = await http.get<Run>(`/runs/${encodeURIComponent(id)}`)
  return data
}