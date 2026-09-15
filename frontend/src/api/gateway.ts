import http from './http'

// 写入类操作会触发后端 reloadCaddy:HTTPS 域名首次可能需 acme.sh DNS-01 签发
// (传播数分钟)。这些请求用长超时,避免 axios 10s 瓶颈误报失败。
const LONG = { timeout: 300000 }

// --- 三．层模型 (ADR-018 修订): App → Service → 域名行 ---

/** 一行域名(一个 site 地址)。 */
export interface ProxyDomainRow {
  protocol: 'https' | 'http'
  subdomain: string
  rootDomain: string
  customPort: boolean
  port?: number
  /** 计算出的完整访问域名(只读展示)。 */
  host?: string
}

/** Service(服务,原 ProxyRoute)。 */
export interface Service {
  id: string
  appId: string
  name: string
  description?: string
  type: 'reverse_proxy' | 'file_server'
  domains: ProxyDomainRow[]
  upstream?: string[]
  upstreamProto?: string
  root?: string
  browse?: boolean
  healthUri?: string
  enabled: boolean
  fragmentIds?: string[]
  excludeFragmentIds?: string[]
  /** 仅 docker 派生服务:容器状态(running / exited / created …);非 running 为异常保留。 */
  containerState?: string
  /** 仅 docker 派生服务:派生告警(ADR-026,如多端口未标注/片段缺失)。 */
  derivedWarning?: string
  createdAt: string
}

/** 分组(App 或 Docker 只读组)。 */
export interface GroupView {
  id: string
  name: string
  description?: string
  source: 'app' | 'docker'
  editable: boolean
  services: ServiceItem[]
}

export interface ServiceItem extends Service {
  source: 'manual' | 'docker'
  health: 'healthy' | 'unhealthy' | 'unknown'
}

// --- App ---

export interface App { id: string; name: string; description?: string; createdAt: string }
export interface AppDetail extends App { services: Service[] }

export interface ServiceInput {
  appId?: string
  name: string
  description?: string
  type: 'reverse_proxy' | 'file_server'
  domains: { protocol: string; subdomain: string; rootDomain: string; customPort: boolean; port?: number }[]
  upstream?: string[]
  upstreamProto?: string
  root?: string
  browse?: boolean
  healthUri?: string
  fragmentIds?: string[]
  excludeFragmentIds?: string[]
}

export interface AppInput {
  name: string
  description?: string
  services?: ServiceInput[]
}

// --- Fragment / 变量 ---

export interface FragmentView {
  id: string
  name: string
  description?: string
  defaultEnabled: boolean
  defaultHidden: boolean
  code: string
  createdAt: string
  builtin: boolean
}

// --- 端点 ---

export async function listGroups(): Promise<GroupView[]> {
  return (await http.get('/gateway/apps')).data
}

export async function createApp(input: AppInput): Promise<AppDetail> {
  return (await http.post('/gateway/apps', input, LONG)).data
}

export async function updateApp(id: string, input: { name: string; description?: string }): Promise<App> {
  return (await http.put(`/gateway/apps/${id}`, input, LONG)).data
}

export async function deleteApp(id: string): Promise<void> {
  await http.delete(`/gateway/apps/${id}`, LONG)
}

export async function getApp(id: string): Promise<AppDetail> {
  return (await http.get(`/gateway/apps/${id}`)).data
}

export async function listAppServices(appId: string): Promise<ServiceItem[]> {
  return (await http.get(`/gateway/apps/${appId}/services`)).data
}

export async function createService(appId: string, input: ServiceInput): Promise<Service> {
  return (await http.post(`/gateway/apps/${appId}/services`, input, LONG)).data
}

/** 无应用上下文创建服务:不填 appId 则归属「默认」。 */
export async function createServiceDefault(input: ServiceInput): Promise<Service> {
  return (await http.post('/gateway/services', input, LONG)).data
}

export async function getService(id: string): Promise<Service> {
  return (await http.get(`/gateway/services/${id}`)).data
}

export async function updateService(id: string, input: ServiceInput): Promise<Service> {
  return (await http.put(`/gateway/services/${id}`, input, LONG)).data
}

export async function deleteService(id: string): Promise<void> {
  await http.delete(`/gateway/services/${id}`, LONG)
}

export async function stopService(id: string): Promise<void> {
  await http.post(`/gateway/services/${id}/stop`, null, LONG)
}

export async function startService(id: string): Promise<void> {
  await http.post(`/gateway/services/${id}/start`, null, LONG)
}

/** 重启该服务的代理(仅 caddy 侧重新生成+load)。 */
export async function restartService(id: string): Promise<void> {
  await http.post(`/gateway/services/${id}/restart`, null, LONG)
}

export async function listFragments(): Promise<FragmentView[]> {
  return (await http.get('/gateway/fragments')).data
}

export async function createFragment(input: {
  name: string; description?: string; code: string;
  defaultEnabled?: boolean; defaultHidden?: boolean;
}): Promise<FragmentView> {
  return (await http.post('/gateway/fragments', input, LONG)).data
}

export async function updateFragment(id: string, input: {
  name: string; description?: string; code: string;
  defaultEnabled?: boolean; defaultHidden?: boolean;
}): Promise<FragmentView> {
  return (await http.put(`/gateway/fragments/${id}`, input, LONG)).data
}

export async function deleteFragment(id: string): Promise<void> {
  await http.delete(`/gateway/fragments/${id}`, LONG)
}

export async function gatewayHealth(): Promise<{ reachable: boolean; upstreams: Record<string, string> }> {
  return (await http.get('/gateway/health')).data
}

export async function gatewayVersion(): Promise<{ version: string; reachable: boolean }> {
  return (await http.get('/gateway/version')).data
}

// --- 网关端口(协议端口记录,ADR-026) ---

export interface PortBinding {
  protocol: string
  description: string
  ports: number[]
  /** L4 网络：tcp | udp | both（http/https 恒 tcp）。 */
  network?: string
  enabled: boolean
  builtin: boolean
  createdAt: string
}

export function listPorts(): Promise<PortBinding[]> {
  return http.get('/gateway/ports').then((r) => r.data)
}

export function createPort(inp: {
  protocol: string; description: string; ports: number[]; network?: string; enabled?: boolean;
}): Promise<PortBinding> {
  return http.post('/gateway/ports', inp).then((r) => r.data)
}

export function updatePort(protocol: string, inp: {
  description: string; ports: number[]; network?: string; enabled?: boolean;
}): Promise<PortBinding> {
  return http.put(`/gateway/ports/${encodeURIComponent(protocol)}`, { ...inp, protocol }).then((r) => r.data)
}

export function deletePort(protocol: string): Promise<void> {
  return http.delete(`/gateway/ports/${encodeURIComponent(protocol)}`)
}

export function restartPorts(): Promise<void> {
  return http.post('/gateway/ports/restart', null, LONG).then((r) => r.data)
}