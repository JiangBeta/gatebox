import { computed, ref, watch, type Ref } from 'vue'
import {
  getCatalog,
  getKindSpec,
  type Catalog,
  type CatalogInner,
  type CatalogTypeRow,
  type KindSpec,
  type SpecField,
} from '@/api/objects'
import { toConfigFields } from './specToFields'
import type { ConfigField, FieldOption } from '@/lib/schema'

/** 类型目录里 dynamic select 字段的候选项来源（按 kind）。键是 CatalogInner 的 JSON 字段名。 */
const TYPE_FIELD_SOURCE: Record<string, keyof CatalogInner> = {
  service: 'serviceTypes',
  middleware: 'middlewareTypes',
  entrypoint: 'entrypointProtocols',
}

/** 该 kind 里「选类型」那个字段的键。 */
function typeFieldKey(kind: string): string | null {
  if (kind === 'entrypoint') return 'protocol'
  if (kind === 'service' || kind === 'middleware') return 'type'
  return null
}

/**
 * 拉一个对象类型的表单契约。
 *
 * dynamic select 的候选项由类型目录补齐：未安装组件的类型**仍然出现**但置灰并加
 * 「[安装]」后缀——与 typespec 的 dynamic 语义一致（看得见、选不了、去装），
 * 也免得用户以为系统没这项功能。
 */
export function useKindSpec(kind: Ref<string>, type: Ref<string | undefined> = ref(undefined)) {
  const spec = ref<KindSpec | null>(null)
  const catalog = ref<Catalog | null>(null)
  const fields = ref<ConfigField[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  function decorate(list: ConfigField[]): ConfigField[] {
    const source = TYPE_FIELD_SOURCE[kind.value]
    const rows = ((catalog.value?.catalog?.[source] as CatalogTypeRow[] | undefined) ?? [])
    if (!source || !rows.length) return list
    if (!rows.length) return list
    const typeKey = typeFieldKey(kind.value)
    const opts: FieldOption[] = rows.map((r) => ({
      label: r.installed ? r.label : `${r.label} [安装]`,
      value: r.id,
      disabled: !r.installed,
    }))
    return list.map((f) => {
      if (f.type !== 'select' || f.key !== typeKey) return f
      // 模型里已显式写了 options 的字段不覆盖（模型优先于目录）。
      if (f.options?.length) return f
      return { ...f, options: opts }
    })
  }

  async function load() {
    loading.value = true
    error.value = null
    try {
      // 目录变化不频繁，但没有它就没有「未安装置灰」信息，故先取一次。
      if (!catalog.value) catalog.value = await getCatalog()
      spec.value = await getKindSpec(kind.value, type.value || undefined)
      fields.value = decorate(toConfigFields(effective(spec.value)))
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  /**
   * 取某个类型的完整字段集（基础 schema + 该类型的参数）。
   *
   * 用于「选了类型才出现参数」的表单：middleware.basic_auth 的 users、
   * service.file_server 的 root 都是按 type 展开的（后端 ExpandType 语义）。
   */
  async function fieldsForType(typeID?: string): Promise<ConfigField[]> {
    if (!typeID) return fields.value
    const sp = await getKindSpec(kind.value, typeID)
    return decorate(toConfigFields(effective(sp)))
  }

  /** 接口给了 type 就用展开后的 fields，否则退回基础 schema。 */
  function effective(sp: KindSpec): SpecField[] {
    return sp.fields?.length ? sp.fields : (sp.schema ?? [])
  }

  /** 后端已实现的 kind 清单（判断引用字段的目标类型是否存在）。 */
  const kinds = computed<string[]>(() => catalog.value?.kinds ?? [])

  watch([kind, type], load, { immediate: true })

  return { spec, catalog, kinds, fields, fieldsForType, loading, error, reload: load }
}
