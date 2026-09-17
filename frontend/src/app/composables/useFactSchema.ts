import { ref, watch, type Ref } from 'vue'
import { getFactSchema } from '@/api/schema'
import type { FactSchema } from '@/lib/schema'

/** 拉取事实配置契约（/api/v1/schema/{factKind}）。 */
export function useFactSchema(factKind: Ref<string>, provider?: Ref<string | undefined>) {
  const schema = ref<FactSchema | null>(null)
  const loading = ref(false)
  const error = ref<string | null>(null)

  async function load() {
    loading.value = true
    error.value = null
    try {
      schema.value = await getFactSchema(factKind.value, provider?.value)
    } catch (e) {
      error.value = e instanceof Error ? e.message : String(e)
    } finally {
      loading.value = false
    }
  }

  watch([factKind, provider ?? ref(undefined)], load, { immediate: true })
  return { schema, loading, error, reload: load }
}
