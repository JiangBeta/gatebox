import http from './http'

/** 扩展能力（ADR-036）。消费者只查表、不认插件身份。 */
export interface Capability {
  id: string
  point: string
  provider: string
  version?: string
  meta?: Record<string, any>
}

/** 协议类别（point=proxy-protocols 的 meta）。 */
export interface ProtocolClass {
  class: 'http' | 'non-http'
  label?: string
  protocols?: string[]
  networks?: string[]
  requiresPrimaryDomain?: boolean
}

export async function listCapabilities(point?: string): Promise<Capability[]> {
  return (await http.get('/capabilities', { params: point ? { point } : {} })).data
}

/** 是否已有提供者支持非 HTTP 协议（TCP/UDP 等能力型插件）。 */
export function hasNonHTTPProtocol(caps: Capability[]): boolean {
  return caps.some(
    (c) => c.point === 'proxy-protocols' && (c.meta as ProtocolClass | undefined)?.class === 'non-http',
  )
}
