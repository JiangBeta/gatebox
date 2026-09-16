import http from './http'

export interface DNSCredential {
  id: string
  provider: string
  name: string
  fields: Record<string, string>
  createdAt: string
}

// DNS 凭证供应商（由后端扩展注册表聚合，可经 dns-provider 插件扩展，ADR-039）。
export interface DNSProviderField {
  name: string
  label?: string
  type?: string
  required?: boolean
  secret?: boolean
}
export interface DNSProviderSpec {
  id: string
  label?: string
  acmeHook?: string
  fields?: DNSProviderField[]
  envMap?: Record<string, string>
}

export const listCredentials = () => http.get<DNSCredential[]>('/credentials').then((r) => r.data)
export const listProviders = () => http.get<DNSProviderSpec[]>('/credentials/providers').then((r) => r.data)
export const createCredential = (c: { provider: string; name: string; fields: Record<string, string> }) =>
  http.post<DNSCredential>('/credentials', c).then((r) => r.data)
export const updateCredential = (id: string, c: { provider: string; name: string; fields: Record<string, string> }) =>
  http.put<DNSCredential>(`/credentials/${id}`, c).then((r) => r.data)
export const deleteCredential = (id: string) => http.delete(`/credentials/${id}`)
export const verifyCredential = (c: { provider: string; fields: Record<string, string> }) =>
  http.post<{ ok: boolean; message: string }>('/credentials/verify', c).then((r) => r.data)
