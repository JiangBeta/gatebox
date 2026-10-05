import http from './http'
import type { Run } from '@/shared/observe'

/**
 * V4.1 对象层与类型目录的 API 客户端（ADR-043 §2/§5）。
 *
 * 页面不直接依赖具体 kind：列表/编辑器都由 `/specs/{kind}` 驱动的通用组件渲染
 * （见 app/components/objects），所以这里只有「形状」没有「业务」。
 */

/**
 * 对象的自动状态（status.state，ADR-043 §4）。
 *
 * 取值是**后端发出的中文**，不是英文枚举——后端 constants 直接用 model/*.yaml
 * 的 status.enum 值（已生效 / 未生效 / 证书告警 / 已停用 / 错误，
 * service 另有 运行中 / 未运行 / 依赖缺失）。尾部 `| string` 是为了容忍
 * 新增状态时前端不必同步改类型（旧英文值仍会显示原文，只是没有语义色）。
 */
export type ObjectState = string

export interface V41Object {
  kind: string
  id: string
  key: string
  spec: Record<string, unknown>
  /** 自动记录（只读）：state / error / skipReason / pendingOp / passwordSet … */
  status: Record<string, unknown>
  createdAt: string
  updatedAt: string
}

export interface FieldOption {
  label: string
  value: string
  disabled?: boolean
}

export interface FieldReference {
  types: string[]
}

/** 类型目录里一个字段的契约（与后端 typespec.Field 的 JSON 一致）。 */
export interface SpecField {
  key: string
  label: string
  type: string
  required?: boolean
  default?: unknown
  placeholder?: string
  description?: string
  note?: string
  advanced?: boolean
  dynamic?: boolean
  full?: boolean
  confirm?: boolean
  pattern?: string
  minLength?: number
  /** status 字段的属性：observed / derived / linked。 */
  kind?: string
  options?: FieldOption[]
  reference?: FieldReference
  item?: SpecField
  fields?: SpecField[]
}

/**
 * 步骤序列模板的一步（log.yaml `sequences[].steps[]`）。
 *
 * marker 是这一步在原始日志里的标志行：前端按 marker 顺序扫日志对账，
 * 命中的记实际时间/耗时，未命中的 optional 步记「跳过」。
 */
export interface SequenceStep {
  no: number
  step: string
  marker: string
  optional?: boolean
}

/** 步骤序列模板（log.yaml `sequences[]`）。不同流程用不同 sequence。 */
export interface Sequence {
  id: string
  label: string
  steps: SequenceStep[]
}

/** `/api/v1/specs/{kind}`。 */
export interface KindSpec {
  kind: string
  title: string
  group: string
  doc?: string
  /** 模型文件名（model/xxx.yaml 的文件名部分），页头展示用。 */
  source?: string
  list: boolean
  readonly?: boolean
  singleton?: boolean
  key: string
  schema: SpecField[]
  writeOnly?: SpecField[]
  status?: SpecField[]
  /** log 类型专有：步骤序列模板（其它 kind 没有此字段）。 */
  sequences?: Sequence[]
  fields: SpecField[]
  refs?: Array<{ Field: string; Types: string[]; Array?: boolean }>
  keyField?: string
}

export interface CatalogTypeRow {
  id: string
  label: string
  from: string
  installed: boolean
  builtin?: boolean
  description?: string
}

/**
 * 类型目录内部结构（后端 typespec.Catalog）。
 *
 * 注意 JSON 是 **camelCase**（serviceTypes / middlewareTypes / entrypointProtocols），
 * 与 yaml 里的 snake_case（service_types）不同 —— 这里是接口的权威形状。
 */
export interface CatalogInner {
  serviceTypes?: CatalogTypeRow[]
  middlewareTypes?: CatalogTypeRow[]
  entrypointProtocols?: CatalogTypeRow[]
  abilities?: unknown[]
}

/**
 * GET /catalog 的响应（后端 typespec.Manifest）：目录 + 对象 kind 清单。
 *
 * kinds 用来判断「某个引用字段指向的 kind 是否真的存在」：
 * domain.credentialId 指向 dns-credential，而该 kind 首批不存在，
 * 界面就该说明原因，而不是给一个永远空的下拉。
 */
export interface Catalog {
  catalog?: CatalogInner
  kinds?: string[]
  groups?: Record<string, string[]>
}

// --- 对象 CRUD ---

export async function listObjects(kind: string): Promise<V41Object[]> {
  const { data } = await http.get<V41Object[]>(`/objects/${kind}`)
  return data
}

export async function listAllObjects(): Promise<Record<string, V41Object[]>> {
  const { data } = await http.get<Record<string, V41Object[]>>('/objects')
  return data
}

export async function getObject(kind: string, id: string): Promise<V41Object> {
  const { data } = await http.get<V41Object>(`/objects/${kind}/${id}`)
  return data
}

/** 新建。password 等只写字段与 spec 平级传（后端会摘出来，不落对象文本）。 */
export async function createObject(
  kind: string,
  spec: Record<string, unknown>,
  writeOnly?: Record<string, unknown>,
): Promise<V41Object> {
  const { data } = await http.post<V41Object>(`/objects/${kind}`, { spec, ...(writeOnly ?? {}) })
  return data
}

export async function updateObject(
  kind: string,
  id: string,
  spec: Record<string, unknown>,
  writeOnly?: Record<string, unknown>,
): Promise<V41Object> {
  const { data } = await http.put<V41Object>(`/objects/${kind}/${id}`, { spec, ...(writeOnly ?? {}) })
  return data
}

/** 改主键：换 id 并级联改写全部引用（普通 PUT 改主键会被拒）。 */
export async function renameObject(kind: string, id: string, newKey: string): Promise<V41Object> {
  const { data } = await http.patch<V41Object>(`/objects/${kind}/${id}/key`, { key: newKey })
  return data
}

export async function deleteObject(kind: string, id: string, force = false): Promise<void> {
  await http.delete(`/objects/${kind}/${id}`, { params: force ? { force: 1 } : undefined })
}

export interface ObjectRef {
  from: string
  id: string
  field: string
  target: string
  value: string
}

export interface ObjectRefs {
  inbound: ObjectRef[]
  outbound: ObjectRef[]
}

export async function getObjectRefs(kind: string, id: string): Promise<ObjectRefs> {
  const { data } = await http.get<ObjectRefs>(`/objects/${kind}/${id}/refs`)
  return data
}

// --- 类型目录 ---

export async function getCatalog(): Promise<Catalog> {
  const { data } = await http.get<Catalog>('/catalog')
  return data
}

export async function getKindSpec(kind: string, type?: string): Promise<KindSpec> {
  const { data } = await http.get<KindSpec>(`/specs/${kind}`, { params: type ? { type } : undefined })
  return data
}

export interface KindFields {
  kind: string
  type?: string
  fields: SpecField[]
  status: SpecField[]
  /** 动态 select 的字段键（如 service.type / entrypoint.protocol）。 */
  typeField: string
}

export async function getKindFields(kind: string, type?: string): Promise<KindFields> {
  const { data } = await http.get<KindFields>(`/specs/${kind}/fields`, {
    params: type ? { type } : undefined,
  })
  return data
}

// --- 同步（落库 → 去抖同步 → Run → 回写状态，ADR-043 §4）---

/** 未生效对象数 + 链路是否启用。 */
export interface PendingSync {
  pending: number
  enabled: boolean
}

export async function getPendingSync(): Promise<PendingSync> {
  const { data } = await http.get<PendingSync>('/objects/sync/pending')
  return data
}

/** 立刻同步一次（不等去抖窗口），返回那条 Run。 */
export async function syncNow(): Promise<Run> {
  const { data } = await http.post<Run>('/objects/sync')
  return data
}
