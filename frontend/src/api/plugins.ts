import http from './http'

export interface PluginView {
  apiVersion: string
  kind: string
  id: string
  name: string
  version: string
  summary: string
  channel: string
  state: string
  message?: string
  requires?: {
    gatebox?: string
    extensionApi?: string
    components?: string[]
    os?: string[]
    arch?: string[]
  }
  artifacts?: { role: string; format?: string; url?: string; entry?: string }[]
  contributions?: {
    capabilities?: { point: string; data?: Record<string, any> }[]
    backend?: { point: string; for?: string; scope?: string }[]
    ui?: {
      nav?: { path: string; label: string }[]
      routes?: { path: string; label: string }[]
      slots?: { slot: string; from?: string }[]
      page?: Record<string, string>
    }
    data?: { subscribe?: string[] }
  }
}

export async function listPlugins(): Promise<PluginView[]> {
  return (await http.get('/plugins')).data
}

export async function installPlugin(id: string): Promise<PluginView> {
  return (await http.post(`/plugins/${id}/install`)).data
}

export async function enablePlugin(id: string): Promise<PluginView> {
  return (await http.post(`/plugins/${id}/enable`)).data
}

export async function disablePlugin(id: string): Promise<PluginView> {
  return (await http.post(`/plugins/${id}/disable`)).data
}

export async function removePlugin(id: string): Promise<PluginView> {
  return (await http.delete(`/plugins/${id}`)).data
}
