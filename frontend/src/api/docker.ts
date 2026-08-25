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

// --- 镜像 ---

export interface ImageView {
  id: string
  names: string[]
  digests: string[]
  arch: string
  size: number
  createdAt: string
  inUse: boolean
  containers?: string[]
}

export interface PullProgress {
  percent: number
  status: string
  done: boolean
  byteWeighted: boolean
  layers: { id: string; phase: string; current: number; total: number }[]
  error?: string
}

// --- 网络 ---

export interface NetworkView {
  id: string
  name: string
  driver: string
  scope: string
  internal: boolean
  attachable: boolean
  subnet?: string
  gateway?: string
  createdAt: string
}

export interface NetworkDetail {
  id: string
  name: string
  driver: string
  scope: string
  internal: boolean
  attachable: boolean
  subnet?: string
  gateway?: string
  createdAt: string
  containers: { name: string; ipv4: string; ipv6?: string }[]
}

// --- 存储卷 ---

export interface VolumeView {
  name: string
  driver: string
  mountpoint: string
  scope: string
  inUse: boolean
  containers?: string[]
  createdAt: string
}

// --- 镜像 API ---

export async function listImages(): Promise<ImageView[]> {
  return (await http.get('/docker/images')).data
}

export async function removeImage(ref: string): Promise<void> {
  await http.delete('/docker/images', { params: { ref } })
}

/** 导入 tar 归档:直接上传原始字节,后端透传 LoadImage 流式处理。 */
export async function loadImage(file: File): Promise<{ loaded: string[] }> {
  const resp = await http.post('/docker/images/load', file, {
    headers: { 'Content-Type': 'application/x-tar' },
    timeout: 0, // 大镜像导入耗时不定,交给用户终止而非超时
  })
  return resp.data
}

// --- 网络 API ---

export async function listNetworks(): Promise<NetworkView[]> {
  return (await http.get('/docker/networks')).data
}

export async function createNetwork(opts: {
  name: string
  driver: string
  internal: boolean
  attachable: boolean
}): Promise<void> {
  await http.post('/docker/networks', opts)
}

export async function inspectNetwork(id: string): Promise<NetworkDetail> {
  return (await http.get(`/docker/networks/${id}`)).data
}

export async function removeNetwork(id: string): Promise<void> {
  await http.delete(`/docker/networks/${id}`)
}

// --- 存储卷 API ---

export async function listVolumes(): Promise<{ volumes: VolumeView[]; warnings: string[] }> {
  return (await http.get('/docker/volumes')).data
}

export async function createVolume(opts: { name: string; driver: string }): Promise<void> {
  await http.post('/docker/volumes', opts)
}

export async function removeVolume(name: string): Promise<void> {
  await http.delete('/docker/volumes', { params: { name } })
}

export async function pruneVolumes(): Promise<{ deleted: string[]; reclaimed: number }> {
  return (await http.post('/docker/volumes/prune')).data
}

// --- 私有仓库(docs §3.3.1) ---

export interface RegistryView {
  id: string
  name: string
  url: string
  scheme: string
  username: string
  hasSecret: boolean
  createdAt: string
}

export interface RegistryInput {
  name: string
  url: string
  scheme: string
  username: string
  secret: string
}

export async function listRegistries(): Promise<RegistryView[]> {
  return (await http.get('/docker/registries')).data
}

export async function createRegistry(input: RegistryInput): Promise<RegistryView> {
  return (await http.post('/docker/registries', input)).data
}

export async function updateRegistry(id: string, input: RegistryInput): Promise<RegistryView> {
  return (await http.put(`/docker/registries/${id}`, input)).data
}

export async function deleteRegistry(id: string): Promise<void> {
  await http.delete(`/docker/registries/${id}`)
}

// --- daemon 配置(docs §3.3.1) ---

export interface DaemonView {
  writable: boolean
  readOnlyReason?: string
  mirrors: string[]
  insecureRegistries: string[]
  maxConcurrentDownloads: number
  liveRestore: boolean
}

export async function getDaemon(): Promise<DaemonView> {
  return (await http.get('/docker/daemon')).data
}

export async function updateDaemon(input: {
  mirrors: string[]
  insecureRegistries: string[]
  maxConcurrentDownloads?: number
}): Promise<void> {
  await http.put('/docker/daemon', input)
}

// --- 编排(docs §3.2) ---

export interface ComposeView {
  projectName: string
  displayName: string
  source: 'managed' | 'external'
  deployed: boolean
  status: string
  runningCount: number
  totalCount: number
  editable: boolean
  configFiles: string
  lastDeployedAt?: string
  createdAt?: string
}

export interface ComposeDetail {
  projectName: string
  displayName: string
  source: string
  managed: boolean
  editable: boolean
  configFiles: string
  yaml: string
  hasDeployedYAML: boolean
  lastDeployedAt?: string
}

export interface DeployProgress {
  id: string
  status: string
  text: string
  error?: string
  done: boolean
}

export async function listCompose(): Promise<ComposeView[]> {
  return (await http.get('/docker/compose')).data
}

export async function createCompose(
  project: string,
  input: { displayName: string; yaml: string },
): Promise<ComposeView> {
  return (await http.post('/docker/compose', input, { params: { project } })).data
}

export async function getCompose(project: string): Promise<ComposeDetail> {
  return (await http.get(`/docker/compose/${project}`)).data
}

export async function saveCompose(
  project: string,
  input: { displayName: string; yaml: string },
): Promise<void> {
  await http.put(`/docker/compose/${project}`, input)
}

export async function validateCompose(input: { project: string; yaml: string }): Promise<void> {
  await http.post('/docker/compose/validate', input)
}

export async function downCompose(project: string): Promise<void> {
  await http.post(`/docker/compose/${project}/down`)
}

export async function restartCompose(project: string): Promise<void> {
  await http.post(`/docker/compose/${project}/restart`)
}

export async function restoreCompose(project: string): Promise<void> {
  await http.post(`/docker/compose/${project}/restore`)
}

export async function deleteCompose(
  project: string,
  opts: { removeData: boolean; removeVolumes: boolean },
): Promise<void> {
  await http.delete(`/docker/compose/${project}`, { params: opts })
}
