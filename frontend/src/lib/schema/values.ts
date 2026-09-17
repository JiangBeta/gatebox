import type { ConfigField, ConfigFieldChild } from './types'

/** 表单值（UI 中间态）：数组项带稳定 id，object 带 presence 标记。 */
export type FormValue = unknown

export interface ArrayItemValue {
  id: string
  value: FormValue
}

/** object 的 presence 标记（区分「未配置」与「配置为空对象」）。 */
export const OBJECT_PRESENT = '__present'

let seq = 0
export function nextId(): string {
  seq += 1
  return `fv${seq}`
}

function isPlainObject(v: unknown): v is Record<string, unknown> {
  return !!v && typeof v === 'object' && !Array.isArray(v)
}

/** 由配置对象构造表单值。默认值只用于展示，不在此物化。 */
export function createFormValues(
  fields: ConfigField[],
  config: Record<string, unknown> = {},
): Record<string, FormValue> {
  const out: Record<string, FormValue> = {}
  for (const f of fields) out[f.key] = createFieldValue(f, config[f.key])
  return out
}

function createFieldValue(f: ConfigFieldChild, raw: unknown): FormValue {
  switch (f.type) {
    case 'array': {
      const arr = Array.isArray(raw) ? raw : []
      return arr.map((v) => ({ id: nextId(), value: f.item ? createFieldValue(f.item, v) : v }))
    }
    case 'object': {
      const obj = isPlainObject(raw) ? raw : undefined
      const out: Record<string, unknown> = {}
      for (const c of f.fields ?? []) out[c.key] = createFieldValue(c, obj ? obj[c.key] : undefined)
      out[OBJECT_PRESENT] = obj !== undefined
      return out
    }
    case 'switch':
      return typeof raw === 'boolean' ? raw : false
    default:
      return raw === undefined || raw === null ? '' : raw
  }
}

/** 表单值 → 配置对象（跳过空值；空数组仅 required 保留；空对象按 presence/required）。 */
export function serializeFormValues(
  fields: ConfigField[],
  values: Record<string, FormValue>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {}
  for (const f of fields) {
    const sv = serializeFieldValue(f, values[f.key])
    if (sv !== undefined) out[f.key] = sv
  }
  return out
}

function serializeFieldValue(f: ConfigFieldChild, v: FormValue): unknown {
  switch (f.type) {
    case 'array': {
      const arr = Array.isArray(v) ? (v as ArrayItemValue[]) : []
      const out = arr
        .map((it) => (f.item ? serializeFieldValue(f.item, it.value) : it.value))
        .filter((x) => x !== undefined && x !== '')
      if (out.length === 0) return f.required ? [] : undefined
      return out
    }
    case 'object': {
      const obj = isPlainObject(v) ? v : {}
      const present = obj[OBJECT_PRESENT] === true
      const out: Record<string, unknown> = {}
      for (const c of f.fields ?? []) {
        const sv = serializeFieldValue(c, obj[c.key])
        if (sv !== undefined) out[c.key] = sv
      }
      if (Object.keys(out).length === 0) {
        return f.required || present ? {} : undefined
      }
      return out
    }
    case 'switch':
      return v === true
    case 'number': {
      if (v === '' || v === undefined || v === null) return undefined
      const n = Number(v)
      return Number.isNaN(n) ? v : n
    }
    default:
      if (v === '' || v === undefined || v === null) return undefined
      return v
  }
}
