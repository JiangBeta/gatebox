import { describe, expect, it } from 'vitest'
import type { Catalog } from '@/api/objects'

/**
 * 锁住 /catalog 的响应形状。
 *
 * 这层以前踩过坑：后端 JSON 是 `{catalog:{serviceTypes:…}, kinds:[…]}`（camelCase + 嵌套），
 * 前端却按 yaml 里的 snake_case 平铺读（`cat.service_types`），
 * 结果动态类型下拉全是空 —— 用户根本选不了服务/中间件类型，而且不报错。
 * 所以这里直接拿后端真实 JSON 断言映射结果。
 */
const manifest: Catalog = {
  catalog: {
    serviceTypes: [
      { id: 'reverse_proxy', label: '反向代理', from: 'caddy', installed: true },
      { id: 'l4_proxy', label: '四层转发', from: 'caddy-l4', installed: false },
    ],
    middlewareTypes: [
      { id: 'encode', label: '压缩', from: 'caddy', installed: true },
      { id: 'php_worker', label: 'PHP Worker', from: 'php-plugin', installed: false },
    ],
    entrypointProtocols: [{ id: 'https', label: 'HTTPS', from: 'caddy', installed: true }],
  },
  kinds: ['route', 'service', 'middleware', 'entrypoint', 'domain', 'host', 'user', 'variable'],
  groups: { 服务: ['route', 'service', 'middleware'], 域名: ['domain'] },
}

/** 与 useKindSpec.decorate 同源的映射（导不出内部函数，这里复刻一遍契约）。 */
function typeOptions(cat: Catalog, key: 'serviceTypes' | 'middlewareTypes' | 'entrypointProtocols') {
  return (cat.catalog?.[key] ?? []).map((r) => ({
    label: r.installed ? r.label : `${r.label} [安装]`,
    value: r.id,
    disabled: !r.installed,
  }))
}

describe('catalog 形状', () => {
  it('目录藏在 catalog 下且用 camelCase 键', () => {
    expect(typeOptions(manifest, 'serviceTypes')).toHaveLength(2)
    expect(typeOptions(manifest, 'middlewareTypes')).toHaveLength(2)
    expect(typeOptions(manifest, 'entrypointProtocols')).toHaveLength(1)
  })

  it('未安装的类型置灰而不是消失（看得见、选不了、去装）', () => {
    const opts = typeOptions(manifest, 'serviceTypes')
    expect(opts).toEqual([
      { label: '反向代理', value: 'reverse_proxy', disabled: false },
      { label: '四层转发 [安装]', value: 'l4_proxy', disabled: true },
    ])
  })

  it('kinds 用于判断引用目标类型是否存在', () => {
    // dns-credential 首批不存在 → domain.credentialId 的下拉必须被隐藏并说明原因，
    // 不能给一个永远空的控件。
    expect(manifest.kinds).toContain('domain')
    expect(manifest.kinds).not.toContain('dns-credential')
  })
})
