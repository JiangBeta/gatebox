import type { ConfigField, FieldOption, ReferenceSpec } from '@/lib/schema'
import type { SpecField } from '@/api/objects'

/**
 * 类型目录字段（`/specs/{kind}/fields`）→ 表单契约（`lib/schema` 的 ConfigField）。
 *
 * 为什么不直接给 SchemaForm 喂 typespec 的字段：表单引擎（校验、数组项 id、
 * 高级区折叠、只读 status 区）已经围绕 ConfigField 写好且有测试，
 * 这里做一次性适配，比让引擎同时认两套字段形状便宜得多。
 */
export function toConfigFields(fields: SpecField[]): ConfigField[] {
  return fields.map((f) => toConfigField(f)).filter((f): f is ConfigField => f !== null)
}

function toConfigField(f: SpecField): ConfigField | null {
  const type = mapType(f.type)
  if (!type) return null
  const out: ConfigField = {
    key: f.key,
    label: f.label || f.key,
    type,
  }
  if (f.required) out.required = true
  if (f.default !== undefined) out.default = f.default
  if (f.placeholder) out.placeholder = f.placeholder
  if (f.description) out.description = f.description
  else if (f.note) out.description = f.note
  if (f.advanced) out.advanced = true
  if (f.options?.length) out.options = f.options as FieldOption[]
  if (f.pattern) out.pattern = f.pattern
  if (f.minLength) out.minLength = f.minLength
  if (f.reference?.types?.length) {
    const ref: ReferenceSpec = { types: f.reference.types }
    out.reference = ref
  }
  if (f.item) out.item = toConfigField(f.item) ?? undefined
  if (f.fields?.length) out.fields = toConfigFields(f.fields)
  return out
}

/** typespec 的字段类型 → 表单控件类型。未知类型返回 null（该字段不渲染）。 */
function mapType(t: string): ConfigField['type'] | null {
  switch (t) {
    case 'text':
      return 'text'
    case 'textarea':
      return 'textarea'
    case 'password':
      return 'password'
    case 'number':
      return 'number'
    case 'switch':
      return 'switch'
    case 'select':
      return 'select'
    case 'reference':
      return 'reference'
    case 'array':
      return 'array'
    case 'object':
      return 'object'
    default:
      return null
  }
}
