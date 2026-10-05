import { parse, stringify } from 'yaml'
import type { ConfigField } from './types'
import { OBJECT_PRESENT, createFormValues, serializeFormValues, type FormValue } from './values'

/**
 * 对象编辑器的 YAML 面与表单面的互转（V4.1 抽屉「左表单 / 右 YAML」）。
 *
 * 为什么需要这一层：两种编辑面表达的是**同一个对象**（ADR-043 §5「双面同源」）。
 * YAML 面不是「查看 JSON」的只读窗——它可直接编辑并保存，所以必须能无损地
 * 映射回表单值；否则用户在 YAML 里删掉一个必填字段，前端却因为表单侧还留着
 * 旧值而把它又写回去，看着像「保存没生效」。
 *
 * 唯一的真相是对象的 spec（后端返回的源字段），两侧都从它派生：
 *   表单 ← createFormValues(fields, spec)
 *   YAML  ← stringify(spec)
 * 反向编辑（YAML → spec → 表单值）走 yamlToFormValues，同样以 spec 为准。
 */

export interface YamlParseOk {
  ok: true
  value: Record<string, unknown>
}
export interface YamlParseErr {
  ok: false
  error: string
}
export type YamlParseResult = YamlParseOk | YamlParseErr

/** stringifyObjectYaml 把对象源字段转成 YAML 文本。 */
export function stringifyObjectYaml(spec: Record<string, unknown>): string {
  return stringify(spec)
}

/**
 * parseObjectYaml 解析 YAML 文本。
 *
 * 顶层必须是「键 → 值」映射：顶层数组或标量无法与表单字段对齐，
 * 判为非法而不是勉强接受（接受会让用户以为改的东西生效了）。
 */
export function parseObjectYaml(text: string): YamlParseResult {
  if (!text.trim()) return { ok: true, value: {} }
  let raw: unknown
  try {
    raw = parse(text)
  } catch (e) {
    return { ok: false, error: e instanceof Error ? e.message : String(e) }
  }
  if (raw === null || raw === undefined) return { ok: true, value: {} }
  if (typeof raw !== 'object' || Array.isArray(raw)) {
    return { ok: false, error: '顶层必须是「键: 值」映射' }
  }
  return { ok: true, value: raw as Record<string, unknown> }
}

/** formValuesToYaml 把表单值序列化成 YAML 文本（提交前的等价文本）。 */
export function formValuesToYaml(
  fields: ConfigField[],
  values: Record<string, FormValue>,
): string {
  return stringifyObjectYaml(serializeFormValues(fields, values))
}

export interface YamlToFormResult {
  ok: boolean
  value?: Record<string, FormValue>
  error?: string
}

/**
 * yamlToFormValues 用 YAML 文本回填表单值。
 *
 * 先解析出 spec 再走 createFormValues——和从对象载入时同一条路径，
 * 所以 array/object 字段拿到的是同样的 UI 结构（数组项带 id，object 带
 * presence 标记），不会因为「从 YAML 来」就少一层包装。
 *
 * YAML 不合法时返回 ok=false 且**不带 value**：调用方据此停在 YAML 面并提示，
 * 不能悄悄回退到旧表单值——那等于把用户刚敲的内容丢掉且不告诉他。
 */
export function yamlToFormValues(
  fields: ConfigField[],
  text: string,
): YamlToFormResult {
  const parsed = parseObjectYaml(text)
  if (!parsed.ok) return { ok: false, error: parsed.error }
  return { ok: true, value: createFormValues(fields, parsed.value) }
}

/**
 * applyYamlToFields 把 YAML 文本解析成待提交的 spec（跳回表单的入口）。
 *
 * 与 yamlToFormValues 分离：YAML 面保存时不必先构造表单值再序列化回去
 * （那样会让 YAML 里写的 number 变成字符串），直接用解析结果当 spec。
 */
export function applyYamlToFields(
  _fields: ConfigField[],
  text: string,
): { ok: true; spec: Record<string, unknown> } | { ok: false; error: string } {
  const parsed = parseObjectYaml(text)
  if (!parsed.ok) return { ok: false, error: parsed.error }
  return { ok: true, spec: parsed.value }
}

/**
 * isEmptySpecFull 判断 spec 是否「一个键都没有」，用来决定 YAML 面是否提示。
 *
 * 排除 OBJECT_PRESENT：那是表单内部标记，不算用户填的内容。
 */
export function isEmptySpec(spec: Record<string, unknown>): boolean {
  return Object.keys(spec).every((k) => k === OBJECT_PRESENT)
}

export { OBJECT_PRESENT }