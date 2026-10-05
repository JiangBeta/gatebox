import { describe, expect, it } from 'vitest'
import type { SpecField } from '@/api/objects'
import { toConfigFields } from './specToFields'
import { createFormValues, serializeFormValues } from '@/lib/schema'

/**
 * V4.1 契约：typespec 字段 → 表单字段 → 提交值。
 *
 * 这条链上最容易出错的是「引用数组」：表单里是 [{id, value}] 的 UI 结构，
 * 提交必须是 id 字符串数组（后端 checkRef 只认 string）。所以这里直接断言
 * 往返结果，而不是只断言字段映射。
 */
const routeFields: SpecField[] = [
  { key: 'name', label: '名称', type: 'text', required: true },
  {
    key: 'roots',
    label: '域名',
    type: 'array',
    required: true,
    item: { key: 'item', label: '域名', type: 'reference', reference: { types: ['domain'] } },
  },
  {
    key: 'middlewares',
    label: '中间件',
    type: 'array',
    item: { key: 'item', label: '中间件', type: 'reference', reference: { types: ['middleware'] } },
  },
  { key: 'port', label: '端口', type: 'number', default: 80 },
]

describe('specToFields', () => {
  it('保留必填/默认值/reference 契约', () => {
    const out = toConfigFields(routeFields)
    const name = out.find((f) => f.key === 'name')
    expect(name?.required).toBe(true)
    const roots = out.find((f) => f.key === 'roots')
    expect(roots?.type).toBe('array')
    expect(roots?.item?.type).toBe('reference')
    expect(roots?.item?.reference?.types).toEqual(['domain'])
    expect(out.find((f) => f.key === 'port')?.default).toBe(80)
  })

  it('未知字段类型不渲染（返回 null 被过滤），不整页崩', () => {
    const out = toConfigFields([
      { key: 'ok', label: '好', type: 'text' },
      { key: 'weird', label: '怪', type: 'duration-ish' } as unknown as SpecField,
    ])
    expect(out.map((f) => f.key)).toEqual(['ok'])
  })

  it('引用数组往返后是 id 字符串数组，而不是 [{id,value}]', () => {
    const fields = toConfigFields(routeFields)
    const values = createFormValues(fields, { name: 'api', roots: ['example.com', 'a.test'] })
    // 表单态：数组是 UI 结构。
    expect(Array.isArray(values.roots)).toBe(true)
    expect((values.roots as { id: string; value: unknown }[])[0]).toHaveProperty('id')

    const spec = serializeFormValues(fields, values)
    expect(spec.roots).toEqual(['example.com', 'a.test'])
  })

  it('空的可选数组不落进 spec（后端把 undefined 与 [] 语义不同）', () => {
    const fields = toConfigFields(routeFields)
    const values = createFormValues(fields, { name: 'api', roots: ['example.com'] })
    const spec = serializeFormValues(fields, values)
    expect(spec.middlewares).toBeUndefined()
  })
})