import { describe, expect, it } from 'vitest'
import type { ConfigField } from './types'
import { createFormValues, serializeFormValues } from './values'
import { validateFormValues } from './validate'

const fields: ConfigField[] = [
  { key: 'name', label: '名称', type: 'text', required: true },
  { key: 'enabled', label: '启用', type: 'switch' },
  {
    key: 'domains',
    label: '域名',
    type: 'array',
    item: {
      type: 'object',
      fields: [
        { key: 'rootDomain', label: '根域', type: 'text', required: true },
        { key: 'port', label: '端口', type: 'number' },
      ],
    },
  },
  {
    key: 'tls',
    label: 'TLS',
    type: 'object',
    fields: [{ key: 'mode', label: '模式', type: 'text' }],
  },
]

describe('schema engine', () => {
  it('往返保持已配置字段，且不物化未触碰的默认值', () => {
    const config = { name: 'api', enabled: true, domains: [{ rootDomain: 'neob.cn', port: 8443 }] }
    const values = createFormValues(fields, config)
    const out = serializeFormValues(fields, values)
    expect(out).toEqual(config)
  })

  it('数组项 id 在序列化时被剥离', () => {
    const values = createFormValues(fields, { domains: [{ rootDomain: 'a.cn' }] })
    const arr = values.domains as { id: string; value: unknown }[]
    expect(arr[0].id).toBeTruthy()
    const out = serializeFormValues(fields, values)
    expect(out.domains).toEqual([{ rootDomain: 'a.cn' }])
  })

  it('空对象默认省略；显式 presence 时保留', () => {
    const empty = serializeFormValues(fields, createFormValues(fields, {}))
    expect(empty.tls).toBeUndefined()

    const present = createFormValues(fields, { tls: {} })
    expect((present.tls as Record<string, unknown>).__present).toBe(true)
    expect(serializeFormValues(fields, present).tls).toEqual({})
  })

  it('空字符串与空数组不落配置（布尔显式）', () => {
    const values = createFormValues(fields, {})
    const out = serializeFormValues(fields, values)
    expect(out).toEqual({ enabled: false })
  })

  it('校验必填与数字', () => {
    const values = createFormValues(fields, {})
    expect(validateFormValues(fields, values)).toContain('名称 必填')

    const ok = createFormValues(fields, { name: 'x', domains: [{ rootDomain: 'a.cn', port: 'abc' }] })
    expect(validateFormValues(fields, ok)).toContain('端口 必须是数字')
  })
})
