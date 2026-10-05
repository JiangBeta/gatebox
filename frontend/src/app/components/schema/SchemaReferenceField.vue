<script setup lang="ts">
import { computed } from 'vue'
import type { ConfigField, FieldOption } from '@/lib/schema'

const model = defineModel<string>({ default: '' })
const props = defineProps<{ field: ConfigField; options?: FieldOption[] }>()

const prefix = computed(() => props.field.reference?.prefix ?? '')
const allowInvert = computed(() => props.field.reference?.allowInvert ?? false)
const hasOptions = computed(() => (props.options?.length ?? 0) > 0)
const targetHint = computed(() => (props.field.reference?.types ?? []).join('/'))

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
    <!--
      没有候选对象时**不能**退回自由文本输入：那等于允许手敲一个裸 id，
      会写出悬空引用（引用了不存在的对象），而且用户根本不知道该敲什么。
    -->
    <a-input
      v-else
      :value="tag"
      disabled
      :placeholder="`暂无可选的 ${targetHint || '目标对象'}（尚未创建或该类型界面未开放）`"
      style="flex: 1"
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
