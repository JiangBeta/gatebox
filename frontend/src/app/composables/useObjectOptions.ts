import { ref, watch, type Ref } from 'vue'
import { listObjects, type V41Object } from '@/api/objects'
import type { FieldOption } from '@/lib/schema'

/**
 * V4.1 引用候选项：按 `reference.types` 里的 kind 拉对象列表，值用 id
 * （ADR-043 §2：引用一律存 id，显示名只在渲染层出现）。
 */
const cache = new Map<string, FieldOption[]>()

/** 显示标签：优先 name/label，其次 key，最后 id（对象可以没有名字字段）。 */
export function displayName(o: V41Object): string {
  const s = o.spec ?? {}
  for (const k of ['name', 'label', 'title']) {
    const v = s[k]
    if (typeof v === 'string' && v) return v
  }
  return o.key || o.id
}

async function optionsOf(kind: string): Promise<FieldOption[]> {
  const hit = cache.get(kind)
  if (hit) return hit
  const list = await listObjects(kind)
  const opts = list.map((o) => ({ label: `${displayName(o)}（${o.id}）`, value: o.id }))
  cache.set(kind, opts)
  return opts
}

/** 写操作后清缓存：下次打开下拉会重新拉对象列表。 */
export function invalidate(): void {
  cache.clear()
}

/** 引用下拉的候选。写操作后可调 invalidate() 刷新。 */
export function useObjectOptions(types: Ref<string[]>) {
  const options = ref<Record<string, FieldOption[]>>({})

  async function load(type: string) {
    if (options.value[type]) return
    try {
      const opts = await optionsOf(type)
      options.value = { ...options.value, [type]: opts }
    } catch {
      // 对象不存在 / 接口异常：回退自由文本（SchemaReferenceField 自带输入框）。
      options.value = { ...options.value, [type]: [] }
    }
  }

  watch(
    types,
    (ts) => {
      for (const t of ts) void load(t)
    },
    { immediate: true, deep: true },
  )

  return { options, invalidate: () => cache.clear() }
}
