import http from './http'
import type { ComponentActivity, ComponentStatus, Run } from '@/shared/observe'

/** 观测与调和端点（v4，L2/L3）。 */

export const getComponentStatus = (id: string) =>
  http.get<ComponentStatus>(`/components/${id}/status`).then((r) => r.data)

export const getComponentActivity = (id: string) =>
  http.get<ComponentActivity>(`/components/${id}/activity`).then((r) => r.data)

export const listRuns = (limit = 20) =>
  http.get<Run[]>('/runs', { params: { limit } }).then((r) => r.data)

export const getRun = (id: string) => http.get<Run>(`/runs/${id}`).then((r) => r.data)

export const triggerReconcile = () =>
  http.post<{ runId: string; state: string }>('/reconcile').then((r) => r.data)
