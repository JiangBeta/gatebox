import http from './http'

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