import http from './http'

// 变量写操作可能触发 caddy 重载(ADR-035 §6),用长超时。
const LONG = { timeout: 300000 }

export interface GatewaySettings {
  caddyHTTPPort: number
  caddyHTTPSPort: number
  caddyHTTPSExtraPorts: number[]
  acmeBin?: string
  confFile: string
}

export async function getGatewaySettings(): Promise<GatewaySettings> {
  return (await http.get('/settings/gateway')).data
}

export async function updateGatewaySettings(input: {
  caddyHTTPPort: number
  caddyHTTPSPort: number
  caddyHTTPSExtraPorts: number[]
}): Promise<{ status: string; note: string }> {
  return (await http.put('/settings/gateway', input)).data
}

// --- 统一变量(网关 + 容器,ADR-035) ---

export interface Variable {
  key: string
  value: string
  description?: string
  createdAt: string
}

/** 系统变量描述符(只读、上下文派生)。 */
export interface SystemVariable {
  key: string
  placeholder: string // 引用写法:网关 <%KEY%> / 容器 ${KEY}
  value?: string
  description: string
  context: 'gateway' | 'container'
}

/** 迁移冲突项(同名异值 / 键名不合法)。 */
export interface VariableConflict {
  key: string
  reason: 'conflict' | 'invalid-key'
  gatewayValue?: string
  containerValue?: string
  description?: string
}

export interface VariableMigration {
  at: string
  migrated: number
  conflicts: VariableConflict[]
}

export async function listVariables(): Promise<Variable[]> {
  return (await http.get('/settings/variables')).data
}

export async function listSystemVariables(): Promise<SystemVariable[]> {
  return (await http.get('/settings/variables/system')).data
}

export async function getVariableMigration(): Promise<VariableMigration | null> {
  const res = await http.get('/settings/variables/migration')
  return res.status === 204 ? null : res.data
}

export async function clearVariableMigration(): Promise<void> {
  await http.delete('/settings/variables/migration')
}

export async function createVariable(key: string, value: string, description?: string): Promise<Variable> {
  return (await http.post('/settings/variables', { key, value, description }, LONG)).data
}

export async function updateVariable(key: string, value: string, description?: string): Promise<Variable> {
  return (await http.put(`/settings/variables/${encodeURIComponent(key)}`, { key, value, description }, LONG)).data
}

export async function deleteVariable(key: string): Promise<void> {
  await http.delete(`/settings/variables/${encodeURIComponent(key)}`, LONG)
}
