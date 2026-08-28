// Compose 编排的 Form ↔ YAML 转换算法(docs §7.2)。
//
// 核心设计:无损透传优先级——导出时先展开 _rawConfigs 作为基础对象,
// 再覆盖写入 UI 识别的标准字段。这样 UI 能覆盖旧值,而 ulimits/sysctls 等
// 未识别的高级参数被完整保留。四项转换算法:
//   1. Caddy label 双向转换  2. build 智能简写  3. healthcheck 规范化  4. 无损透传。
//
// 依赖 `yaml` 包(非 js-yaml),仅在本模块 import。

import { parse, stringify } from 'yaml'

export interface CaddyRoute {
  domain?: string
  path?: string
  port?: string // 指向端口(表单用,不落 label,见 ADR-017 §2)
  customDirectives?: string[]
}

export interface HealthcheckConfig {
  test?: string
  interval?: string
  timeout?: string
  retries?: number
  start_period?: string
}

export interface NetworkRef {
  name: string
  aliases?: string[]
  ipv4?: string
}

export interface ServiceConfig {
  // 基础
  image?: string
  container_name?: string
  restart?: string
  pid?: string // 关联宿主机 PID → pid: "host"
  hostname?: string // 容器主机名(可选,不默认下发,ADR-017)
  ports?: string[]
  environment?: Record<string, string>
  volumes?: string[]
  networks?: NetworkRef[]
  // 高级
  devices?: string[]
  network_mode?: string
  user?: string
  command?: string
  entrypoint?: string
  cap_add?: string[]
  extra_hosts?: string[] // 关联宿主机网络 → extra_hosts: ["host:ip"]
  logging?: { driver: string; options?: Record<string, string> }
  healthcheck?: HealthcheckConfig
  build?: string | { context: string; dockerfile?: string; args?: Record<string, string> }
  labels?: Record<string, string>
  depends_on?: string[]
  deploy?: { replicas?: number }
  // 资源限制(单机非 swarm,ADR-017 §5):shm_size/cpus/mem_limit/gpus
  shm_size?: string
  cpus?: string
  mem_limit?: string
  gpus?: string
  // 从 labels 提取的 caddy 路由
  caddyRoutes?: CaddyRoute[]
  // 兜底桶:未识别字段(_rawConfigs)与非 caddy 原生 label(_rawLabels)
  _rawConfigs?: Record<string, any>
  _rawLabels?: Record<string, string>
}

export interface ComposeState {
  services: Record<string, ServiceConfig>
  networks?: Record<string, any>
  volumes?: Record<string, any>
  _raw?: Record<string, any> // top-level 未识别字段(version, x-*, ...)
}

/** UI 识别的服务字段。不在其中的键一律进 _rawConfigs(无损兜底)。 */
const KNOWN_KEYS = new Set([
  'image', 'container_name', 'restart', 'pid', 'hostname', 'ports', 'environment', 'volumes',
  'networks', 'devices', 'extra_hosts', 'network_mode', 'user', 'command', 'entrypoint',
  'cap_add', 'logging', 'healthcheck', 'build', 'labels', 'depends_on', 'deploy',
  'shm_size', 'cpus', 'mem_limit', 'gpus',
])

// --- YAML → State ---

export function parseCompose(yamlStr: string): ComposeState {
  const raw = parse(yamlStr) || {}
  const state: ComposeState = { services: {} }

  for (const [key, val] of Object.entries(raw)) {
    if (key === 'services') {
      if (val && typeof val === 'object') {
        for (const [name, svc] of Object.entries(val as Record<string, any>)) {
          state.services[name] = parseService(svc ?? {})
        }
      }
    } else if (key === 'networks' || key === 'volumes') {
      ;(state as any)[key] = val
    } else {
      state._raw = state._raw || {}
      state._raw[key] = val
    }
  }
  return state
}

function parseService(raw: Record<string, any>): ServiceConfig {
  const cfg: ServiceConfig = {}
  const rawConfigs: Record<string, any> = {}

  for (const [key, val] of Object.entries(raw)) {
    if (!KNOWN_KEYS.has(key)) {
      rawConfigs[key] = val
      continue
    }
    switch (key) {
      case 'image':
      case 'container_name':
      case 'restart':
      case 'pid':
      case 'hostname':
      case 'network_mode':
      case 'user':
        if (typeof val === 'string') (cfg as any)[key] = val
        else rawConfigs[key] = val
        break
      case 'shm_size':
      case 'cpus':
      case 'mem_limit':
      case 'gpus':
        // 资源字段:数字(cpus: 2)或字符串("64m")都归一为字符串
        if (typeof val === 'string' || typeof val === 'number') (cfg as any)[key] = String(val)
        else rawConfigs[key] = val
        break
      case 'ports':
        cfg.ports = parsePorts(val)
        if (cfg.ports === null) { rawConfigs.ports = val; delete cfg.ports }
        break
      case 'volumes':
      case 'devices':
      case 'cap_add':
      case 'extra_hosts':
      case 'depends_on':
        cfg[key] = stringArray(val)
        break
      case 'networks':
        cfg.networks = parseNetworks(val)
        break
      case 'environment':
        cfg.environment = parseEnvironment(val)
        if (cfg.environment === null) { rawConfigs.environment = val; delete cfg.environment }
        break
      case 'command':
      case 'entrypoint':
        cfg[key] = parseCommand(val)
        break
      case 'logging':
        cfg.logging = val
        break
      case 'healthcheck':
        cfg.healthcheck = parseHealthcheck(val)
        break
      case 'build':
        cfg.build = parseBuild(val)
        break
      case 'labels':
        cfg.labels = val
        Object.assign(cfg, parseCaddyLabels(val))
        break
      case 'deploy':
        cfg.deploy = parseDeploy(val)
        break
    }
  }

  if (Object.keys(rawConfigs).length > 0) cfg._rawConfigs = rawConfigs
  return cfg
}

/** ports 支持短形式("8080:80")与长形式({target,published}),统一转短形式。 */
function parsePorts(val: any): string[] | null {
  if (!Array.isArray(val)) return null
  const out: string[] = []
  for (const p of val) {
    if (typeof p === 'string') {
      out.push(p)
    } else if (p && typeof p === 'object' && p.target) {
      // 长形式:published:target[/protocol]
      const host = p.published ?? ''
      let s = host ? `${host}:${p.target}` : `${p.target}`
      if (p.protocol && p.protocol !== 'tcp') s += `/${p.protocol}`
      if (p.host_ip) s = `${p.host_ip}:${s}`
      out.push(s)
    } else {
      return null // 无法识别的形式,整列交给 _rawConfigs
    }
  }
  return out
}

function stringArray(val: any): string[] {
  if (!Array.isArray(val)) return []
  return val.map((v) => (typeof v === 'string' ? v : stringify(v)?.trim() ?? '')).filter(Boolean)
}

function parseNetworks(val: any): NetworkRef[] {
  if (Array.isArray(val)) return val.map((v) => ({ name: String(v) }))
  if (val && typeof val === 'object') {
    return Object.entries(val).map(([name, cfg]: [string, any]) => ({
      name,
      aliases: cfg?.aliases ? (Array.isArray(cfg.aliases) ? cfg.aliases : [String(cfg.aliases)]) : undefined,
      ipv4: cfg?.ipv4_address ? String(cfg.ipv4_address) : undefined,
    }))
  }
  return []
}

function parseEnvironment(val: any): Record<string, string> | null {
  if (val == null) return {}
  if (Array.isArray(val)) {
    const out: Record<string, string> = {}
    for (const item of val) {
      if (typeof item !== 'string') return null
      const eq = item.indexOf('=')
      if (eq < 0) return null // "KEY" 无等号(从宿主继承),无法无损转对象,整列兜底
      out[item.slice(0, eq)] = item.slice(eq + 1)
    }
    return out
  }
  if (typeof val === 'object') {
    const out: Record<string, string> = {}
    for (const [k, v] of Object.entries(val)) out[k] = String(v)
    return out
  }
  return null
}

function parseCommand(val: any): string {
  if (typeof val === 'string') return val
  if (Array.isArray(val)) return val.map(String).join(' ')
  return ''
}

function parseHealthcheck(val: any): HealthcheckConfig {
  if (!val || typeof val !== 'object') return {}
  const out: HealthcheckConfig = {}
  if (val.test != null) {
    // 归一为字符串:["CMD", "curl", "-f", ...] → "curl -f ..."
    out.test = Array.isArray(val.test)
      ? val.test.filter((x: any) => x !== 'CMD' && x !== 'CMD-SHELL').map(String).join(' ')
      : String(val.test)
  }
  for (const k of ['interval', 'timeout', 'start_period'] as const) {
    if (val[k] != null) out[k] = String(val[k])
  }
  if (val.retries != null) out.retries = Number(val.retries)
  return out
}

/** build 简写:字符串 → {context, dockerfile:'Dockerfile'}。 */
function parseBuild(val: any): ServiceConfig['build'] {
  if (typeof val === 'string') return { context: val, dockerfile: 'Dockerfile' }
  return val
}

function parseDeploy(val: any): ServiceConfig['deploy'] {
  if (!val || typeof val !== 'object') return undefined
  const out: ServiceConfig['deploy'] = {}
  if (val.replicas != null) out.replicas = Number(val.replicas)
  return out
}

/** 拆 labels:caddy 前缀键重组为 caddyRoutes,非 caddy 键进 _rawLabels。 */
function parseCaddyLabels(labels: Record<string, string> | undefined): {
  caddyRoutes?: CaddyRoute[]
  _rawLabels?: Record<string, string>
} {
  if (!labels) return {}
  const caddyEntries: [number, string][] = []
  const rawLabels: Record<string, string> = {}

  for (const [k, v] of Object.entries(labels)) {
    if (k === 'caddy') {
      caddyEntries.push([0, v])
    } else if (k.startsWith('caddy_')) {
      const n = parseInt(k.slice(6), 10)
      if (!Number.isNaN(n)) caddyEntries.push([n, v])
      else rawLabels[k] = v
    } else if (k.startsWith('caddy.')) {
      rawLabels[k] = v
    } else {
      rawLabels[k] = v
    }
  }

  caddyEntries.sort((a, b) => a[0] - b[0])
  const caddyRoutes = caddyEntries.map(([, v]) => parseCaddyValue(v))

  const out: { caddyRoutes?: CaddyRoute[]; _rawLabels?: Record<string, string> } = {}
  if (caddyRoutes.length > 0) out.caddyRoutes = caddyRoutes
  if (Object.keys(rawLabels).length > 0) out._rawLabels = rawLabels
  return out
}

/** 解析单个 caddy label 值:"domain" / "domain /path" / "domain {...高级指令}". */
function parseCaddyValue(v: string): CaddyRoute {
  const tokens = v.trim().split(/\s+/)
  const route: CaddyRoute = {}
  if (tokens.length > 0) route.domain = tokens[0]
  if (tokens.length > 1 && tokens[1].startsWith('/')) route.path = tokens[1]
  const rest = tokens.slice(route.path ? 2 : 1)
  if (rest.length > 0) route.customDirectives = rest
  return route
}

// --- State → YAML ---

export function serializeCompose(state: ComposeState): string {
  const out: Record<string, any> = { ...(state._raw || {}) }

  out.services = {}
  for (const [name, cfg] of Object.entries(state.services)) {
    out.services[name] = serializeService(cfg)
  }
  if (state.networks != null) out.networks = state.networks
  if (state.volumes != null) out.volumes = state.volumes

  return stringify(out, { indent: 2 })
}

function serializeService(cfg: ServiceConfig): Record<string, any> {
  // 算法 4:先展开 _rawConfigs 作为基础,再覆盖标准字段
  const out: Record<string, any> = { ...(cfg._rawConfigs || {}) }

  if (cfg.image) out.image = cfg.image
  if (cfg.container_name) out.container_name = cfg.container_name
  if (cfg.restart) out.restart = cfg.restart
  if (cfg.pid) out.pid = cfg.pid
  if (cfg.hostname) out.hostname = cfg.hostname
  if (cfg.ports?.length) out.ports = cfg.ports
  if (cfg.environment && Object.keys(cfg.environment).length > 0) out.environment = cfg.environment
  if (cfg.volumes?.length) out.volumes = cfg.volumes
  if (cfg.networks?.length) out.networks = serializeNetworks(cfg.networks)

  if (cfg.devices?.length) out.devices = cfg.devices
  if (cfg.network_mode) out.network_mode = cfg.network_mode
  if (cfg.user) out.user = cfg.user
  if (cfg.command) out.command = cfg.command
  if (cfg.entrypoint) out.entrypoint = cfg.entrypoint
  if (cfg.cap_add?.length) out.cap_add = cfg.cap_add
  if (cfg.extra_hosts?.length) out.extra_hosts = cfg.extra_hosts
  if (cfg.logging) out.logging = cfg.logging
  if (cfg.healthcheck && Object.keys(cfg.healthcheck).length > 0) out.healthcheck = serializeHealthcheck(cfg.healthcheck)
  if (cfg.build) out.build = serializeBuild(cfg.build)
  if (cfg.depends_on?.length) out.depends_on = cfg.depends_on
  if (cfg.deploy?.replicas != null) out.deploy = { replicas: cfg.deploy.replicas }

  if (cfg.shm_size) out.shm_size = cfg.shm_size
  if (cfg.cpus) out.cpus = cfg.cpus
  if (cfg.mem_limit) out.mem_limit = cfg.mem_limit
  if (cfg.gpus) out.gpus = cfg.gpus

  // labels = _rawLabels(非 caddy 原生)+ caddy 路由(算法 1)
  const labels: Record<string, string> = { ...(cfg._rawLabels || {}) }
  Object.assign(labels, serializeCaddyLabels(cfg.caddyRoutes))
  if (Object.keys(labels).length > 0) out.labels = labels

  return out
}

/** 算法 1:首个路由用无后缀 caddy 键,后续 caddy_1、caddy_2 ... */
function serializeCaddyLabels(routes: CaddyRoute[] | undefined): Record<string, string> {
  if (!routes || routes.length === 0) return {}
  const out: Record<string, string> = {}
  routes.forEach((r, i) => {
    const parts = [r.domain]
    if (r.path) parts.push(r.path)
    if (r.customDirectives?.length) parts.push(...r.customDirectives)
    const val = parts.filter(Boolean).join(' ')
    out[i === 0 ? 'caddy' : `caddy_${i}`] = val
  })
  return out
}

/** 算法 3:healthcheck test 重组回 [testType, ...command.split(' ')]。 */
function serializeHealthcheck(hc: HealthcheckConfig): Record<string, any> {
  const out: Record<string, any> = {}
  if (hc.test) out.test = ['CMD', ...hc.test.split(/\s+/).filter(Boolean)]
  if (hc.interval) out.interval = hc.interval
  if (hc.timeout) out.timeout = hc.timeout
  if (hc.retries != null) out.retries = hc.retries
  if (hc.start_period) out.start_period = hc.start_period
  return out
}

/** 算法 2:仅当 dockerfile==='Dockerfile' 且 args 空时降级为纯字符串。 */
function serializeBuild(build: ServiceConfig['build']): any {
  if (typeof build === 'string') return build
  const { context, dockerfile, args } = build as any
  if (dockerfile === 'Dockerfile' && (!args || Object.keys(args).length === 0)) {
    return context
  }
  return build
}

/** 网络:全为简单名时输出数组,含别名/IPV4 时输出对象映射。 */
function serializeNetworks(nets: NetworkRef[]): any {
  const hasStructure = nets.some((n) => n.aliases?.length || n.ipv4)
  if (!hasStructure) return nets.map((n) => n.name)
  const out: Record<string, any> = {}
  for (const n of nets) {
    const cfg: Record<string, any> = {}
    if (n.aliases?.length) cfg.aliases = n.aliases
    if (n.ipv4) cfg.ipv4_address = n.ipv4
    out[n.name] = cfg
  }
  return out
}
