import http from './http'

export interface StoreItem {
  id: string
  name: string
  kind: string
  source: string
  tags: string[]
  version: string
  summary: string
  installed: boolean
  status?: string
  upgradeable: boolean
}

export async function listStore(): Promise<StoreItem[]> {
  return (await http.get('/store')).data
}

export async function installStore(id: string): Promise<StoreItem> {
  return (await http.post(`/store/${id}/install`)).data
}

export async function removeStore(id: string): Promise<StoreItem> {
  return (await http.delete(`/store/${id}`)).data
}
