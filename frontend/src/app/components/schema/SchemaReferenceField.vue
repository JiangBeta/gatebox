<script setup lang="ts">
import { computed } from 'vue'
import type { ConfigField, FieldOption } from '@/lib/schema'

const model = defineModel<string>({ default: '' })
const props = defineProps<{ field: ConfigField; options?: FieldOption[] }>()

const prefix = computed(() => props.field.reference?.prefix ?? '')
const allowInvert = computed(() => props.field.reference?.allowInvert ?? false)
const hasOptions = computed(() => (props.options?.length ?? 0) > 0)

const inverted = computed(() => (model.value ?? '').startsWith('!'))
const tag = computed(() => {
  let s = model.value ?? ''
  if (s.startsWith('!')) s = s.slice(1)
  if (prefix.value && s.startsWith(prefix.value)) s = s.slice(prefix.value.length)
  return s
})

function rebuild(inv: boolean, nextTag: string) {
  model.value = `${inv ? '!' : ''}${prefix.value}${nextTag}`
}
</script>

<template>
  <div class="schema-ref">
    <a-switch
      v-if="allowInvert"
      :checked="inverted"
      checked-children="取反"
      un-checked-children="正向"
      @change="(v: unknown) => rebuild(Boolean(v), tag)"
    />
    <a-select
      v-if="hasOptions"
      :value="tag || undefined"
      :options="options"
      show-search
      allow-clear
      :placeholder="field.placeholder || '选择引用'"
      style="flex: 1"
      @change="(v: unknown) => rebuild(inverted, String(v ?? ''))"
    />
    <a-input
      v-else
      :value="tag"
      :placeholder="field.placeholder"
      style="flex: 1"
      @change="(e: { target: { value: string } }) => rebuild(inverted, e.target.value)"
    />
  </div>
</template>

<style scoped>
.schema-ref {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
