import { parse, stringify } from 'yaml'
import { describe, expect, it } from 'vitest'
import type { ConfigField } from './types'
import {
  parseObjectYaml,
  stringifyObjectYaml,
  yamlToFormValues,
  formValuesToYaml,
} from './objectYaml'

/** 测试用字段集：type 必填，故这里统一给 text。 */
const text = (key: string): ConfigField => ({ key, label: key, type: 'text' })

describe('stringifyObjectYaml', () => {
  it('输出可被 YAML 解析回来的等价结构', () => {
    const spec = { name: 'api', port: 8080, enabled: true, hosts: ['a', 'b'] }
    expect(parseObjectYaml(stringifyObjectYaml(spec))).toEqual({ ok: true, value: spec })
  })

  it('空对象出 {}，不留空行', () => {
    expect(stringifyObjectYaml({}).trim()).toBe('{}')
  })
})

describe('parseObjectYaml', () => {
  it('合法 YAML 返回对象', () => {
    expect(parseObjectYaml('a: 1\nb: two')).toEqual({ ok: true, value: { a: 1, b: 'two' } })
  })

  it('语法错误返回错误而不是抛异常', () => {
    const r = parseObjectYaml('a: [1, 2\nb: :')
    expect(r.ok).toBe(false)
    if (r.ok) throw new Error('期望解析失败')
    expect(r.error).toBeTruthy()
  })

  it('顶层不是对象（如数组或标量）判为非法', () => {
    // 对象编辑器的 YAML 面必须是「键值集合」：顶层数组无法与表单字段对齐，
    // 静默接受会让用户以为改的东西生效了。
    expect(parseObjectYaml('- 1\n- 2').ok).toBe(false)
    expect(parseObjectYaml('just a string').ok).toBe(false)
  })

  it('空文本视为空对象', () => {
    expect(parseObjectYaml('')).toEqual({ ok: true, value: {} })
  })
})

describe('formValuesToYaml', () => {
  it('把表单值序列化成 YAML 文本', () => {
    const yaml = formValuesToYaml([text('port')], { port: '8080' })
    expect(parseObjectYaml(yaml)).toEqual({ ok: true, value: { port: '8080' } })
  })
})

describe('yamlToFormValues', () => {
  it('从 YAML 文本回填表单值', () => {
    const got = yamlToFormValues([text('host'), text('port')], 'host: nas\nport: 22')
    expect(got.ok).toBe(true)
    expect(got.value?.host).toBe('nas')
    expect(got.value?.port).toBe(22)
  })

  it('非法 YAML 返回 ok=false 而不是静默清空表单', () => {
    // 关键：静默清空会让用户的编辑凭空消失，且没有任何提示。
    const got = yamlToFormValues([text('a')], 'a: [1,\nb: :')
    expect(got.ok).toBe(false)
    expect(got.value).toBeUndefined()
  })
})