import http from './http'

export interface ComponentStatus {
  State: string
  Healthy: boolean
  Message: string
}

export interface ComponentInfo {
  ID: string
  Name: string
  Kind: string
  Tier: string
  Provision: string
  Runtime: string
  Upgrade: string
  Capabilities: string[]
  DefaultEnabled: boolean
  Removable: boolean
  Bundled: boolean
  current: string
  latest: string
  updateAvailable: boolean
  status: ComponentStatus
  repo?: string
}

export async function listComponents(): Promise<ComponentInfo[]> {
  return (await http.get('/components')).data
}

export async function checkComponent(id: string): Promise<ComponentInfo & { error?: string }> {
  return (await http.post(`/components/${id}/check`)).data
}

export async function upgradeComponent(id: string): Promise<ComponentInfo> {
  return (await http.post(`/components/${id}/upgrade`)).data
}
