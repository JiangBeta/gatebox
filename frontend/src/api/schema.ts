import http from './http'
import type { FactSchema } from '@/lib/schema'

/**
 * 事实配置契约（v4，docs/v4/L1-03-schema.md）。
 * provider 仅对 factKind=credential 有意义，用于追加供应商字段。
 */
export async function getFactSchema(factKind: string, provider?: string): Promise<FactSchema> {
  const params = provider ? { provider } : undefined
  return (await http.get(`/schema/${factKind}`, { params })).data
}
