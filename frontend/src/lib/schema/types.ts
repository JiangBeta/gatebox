/**
 * 配置契约类型：镜像后端 component.ConfigField（docs/v4/L1-03-schema.md）。
 *
 * L1 手写；v3 §10 的 OpenAPI 生成落地后改为生成。
 */

export type ConfigType =
  | 'text'
  | 'password'
  | 'number'
  | 'select'
  | 'switch'
  | 'textarea'
  | 'array'
  | 'object'
  | 'reference'

export interface FieldOption {
  label: string
  value: string | number
}

export interface ReferenceSpec {
  types?: string[]
  prefix?: string
  allowInvert?: boolean
}

export interface ConfigField {
  key: string
  label: string
  type: ConfigType
  required?: boolean
  default?: unknown
  advanced?: boolean
  placeholder?: string
  description?: string
  docs?: string
  options?: FieldOption[]
  reference?: ReferenceSpec
  item?: ConfigFieldChild
  fields?: ConfigField[]
  summaryFields?: string[]
}

/** 数组元素/子字段：key/label 可省略（由外层生成）。 */
export interface ConfigFieldChild extends Omit<ConfigField, 'key' | 'label'> {
  key?: string
  label?: string
}

export interface FactSchema {
  factKind: string
  uiHint?: string
  fields: ConfigField[]
}
