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
  requires?: { gatebox?: string; components?: string[] }
  contributions?: { nav?: { path: string; label: string }[]; page?: Record<string, string> }
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
