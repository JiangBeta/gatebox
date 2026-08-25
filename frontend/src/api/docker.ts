import http from './http'

export interface PortMapping {
  ip?: string
  host?: number
  container: number
  protocol: string
}

/** 容器来源三态(docs §2.3) */
export type ContainerSource = 'managed' | 'external' | 'loose'

export interface ContainerView {
  id: string
  name: string
  image: string
  state: string
  status: string
  source: ContainerSource
  project?: string
  service?: string
  ports: PortMapping[]
  createdAt: string
  startedAt?: string
  tty: boolean
  logDriver?: string
  logsReadable: boolean
  health?: string
  hasStats: boolean
  cpuPercent: number
  memoryUsage: number
  memoryLimit: number
  memoryPercent: number
}

export interface DockerInfo {
  serverVersion: string
  containers: number
  containersRunning: number
  images: number
  loggingDriver: string
  logsReadable: boolean
  cgroupVersion: string
  architecture: string
  imagePlatform: string
  ncpu: number
  memTotal: number
  liveRestore: boolean
}

export interface ShellProbe {
  available: boolean
  shell: string
}

export async function dockerInfo(): Promise<DockerInfo> {
  return (await http.get('/docker/info')).data
}

export async function listContainers(): Promise<ContainerView[]> {
  return (await http.get('/docker/containers')).data
}

export async function startContainer(id: string): Promise<void> {
  await http.post(`/docker/containers/${id}/start`)
}

export async function stopContainer(id: string): Promise<void> {
  // 停止可能需要等待优雅关闭,超时放宽到 40 秒(daemon 默认 10 秒 + 余量)
  await http.post(`/docker/containers/${id}/stop`, null, { timeout: 40000 })
}

export async function restartContainer(id: string): Promise<void> {
  await http.post(`/docker/containers/${id}/restart`, null, { timeout: 40000 })
}

export async function removeContainer(id: string, removeVolumes = false): Promise<void> {
  await http.delete(`/docker/containers/${id}`, { params: { removeVolumes } })
}

export async function probeShell(id: string): Promise<ShellProbe> {
  return (await http.get(`/docker/containers/${id}/shells`, { timeout: 20000 })).data
}

/** 构造 WebSocket 地址(开发模式经 vite 代理,生产为同源)。 */
export function wsURL(path: string, params: Record<string, string> = {}): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
  const qs = new URLSearchParams(params).toString()
  return `${proto}//${location.host}/api/v1${path}${qs ? '?' + qs : ''}`
}

export interface LogMessage {
  stream?: string
  data?: string
  error?: string
}
