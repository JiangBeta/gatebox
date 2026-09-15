import http from './http'

/** mosdns 运行状态（进程态 + 配置解析出的路径/端口）。 */
export interface MosdnsStatus {
  installed: boolean
  state: string
  healthy: boolean
  version: string
  latest: string
  updateAvailable: boolean
  listen: string
  apiAddr: string
  configPath: string
  configExists: boolean
  hostsPath: string
  logFile: string
  cacheTag: string
}

/** 基础设置（对应后端 mosdns.Settings）。 */
export interface MosdnsSettings {
  listen: string
  logLevel: string
  localDns: string[]
  remoteDns: string[]
  cache: boolean
  cacheSize: number
}

/** 一条内网解析记录。 */
export interface MosdnsHost {
  domain: string
  ips: string[]
}

export interface MosdnsConfig {
  content: string
  path: string
  configured: boolean
}

export async function getStatus(): Promise<MosdnsStatus> {
  return (await http.get('/mosdns/status')).data
}

export async function getSettings(): Promise<MosdnsSettings> {
  return (await http.get('/mosdns/settings')).data
}

/** 依基础设置重新生成 config.yaml（会覆盖手工编辑）。 */
export async function saveSettings(s: MosdnsSettings): Promise<MosdnsSettings> {
  return (await http.put('/mosdns/settings', s)).data
}

export async function getConfig(): Promise<MosdnsConfig> {
  return (await http.get('/mosdns/config')).data
}

export async function saveConfig(content: string): Promise<{ path: string }> {
  return (await http.put('/mosdns/config', { content })).data
}

export async function listHosts(): Promise<MosdnsHost[]> {
  return (await http.get('/mosdns/hosts')).data
}

export async function saveHosts(hosts: MosdnsHost[]): Promise<MosdnsHost[]> {
  return (await http.put('/mosdns/hosts', { hosts })).data
}

export async function getLogs(): Promise<{ content: string; path: string }> {
  return (await http.get('/mosdns/logs')).data
}

export async function clearLogs(): Promise<void> {
  await http.delete('/mosdns/logs')
}

export async function flushCache(): Promise<void> {
  await http.post('/mosdns/flush')
}
