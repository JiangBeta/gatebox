/** v4 观测与调和的跨模块类型（L2/L3）。 */

export interface ComponentStatus {
  State: string
  Healthy: boolean
  Message?: string
}

export interface ComponentActivity {
  task?: string
  step?: string
  progress?: number
  actor?: string
  since?: number
}

export interface RunStep {
  component: string
  action: string
  state: string
  detail?: string
  startedAt?: number
  endedAt?: number
  events?: string[]
}

export interface Run {
  id: string
  trigger: string
  intent?: string
  state: string
  startedAt: number
  endedAt?: number
  steps: RunStep[]
}

export interface ObserveEvent {
  seq: number
  kind: 'state' | 'activity' | 'run' | string
  component?: string
  runId?: string
  data?: unknown
  at: number
}
