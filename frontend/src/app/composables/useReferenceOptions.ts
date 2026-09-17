import { ref, watch, type Ref } from 'vue'
import type { FieldOption } from '@/lib/schema'
import { listDomains } from '@/api/domains'
import { listCredentials } from '@/api/credentials'
import { listFragments, listGroups } from '@/api/gateway'

export type ReferenceOption = FieldOption

type Loader = () => Promise<ReferenceOption[]>

const loaders: Record<string, Loader> = {
  // 域名行按 rootDomain 名称引用（Service.domains[].rootDomain），故 value 用 name。
  domain: async () => (await listDomains()).map((d) => ({ label: d.name, value: d.name })),
  credential: async () => (await listCredentials()).map((c) => ({ label: c.name, value: c.id })),
  fragment: async () => (await listFragments()).map((f) => ({ label: f.name, value: f.id })),
  app: async () =>
    (await listGroups())
      .filter((g) => g.source === 'app')
      .map((g) => ({ label: g.name, value: g.id })),
}

/** 按 reference.types 懒加载引用选项；未知类型返回空（组件回退自由文本）。 */
export function useReferenceOptions(types: Ref<string[]>) {
  const options = ref<Record<string, ReferenceOption[]>>({})

  async function load(type: string) {
    const loader = loaders[type]
    if (!loader || options.value[type]) return
    try {
      options.value = { ...options.value, [type]: await loader() }
    } catch {
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

  return { options }
}
