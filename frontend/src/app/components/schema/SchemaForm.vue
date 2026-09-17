<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import type { ConfigField, FieldOption } from '@/lib/schema'
import { createFormValues, serializeFormValues, validateFormValues } from '@/lib/schema'
import SchemaField from './SchemaField.vue'
import SchemaAdvancedSection from './SchemaAdvancedSection.vue'

const props = defineProps<{
  fields: ConfigField[]
  modelValue?: Record<string, unknown>
  referenceOptions?: Record<string, FieldOption[]>
}>()
const emit = defineEmits<{
  (e: 'update:modelValue', v: Record<string, unknown>): void
  (e: 'validity', errors: string[]): void
}>()

const values = ref(createFormValues(props.fields, props.modelValue ?? {}))
const regular = computed(() => props.fields.filter((f) => !f.advanced))
const advanced = computed(() => props.fields.filter((f) => f.advanced))
const advancedOpen = ref(advanced.value.some((f) => (props.modelValue ?? {})[f.key] !== undefined))

watch(
  values,
  () => {
    const serialized = serializeFormValues(props.fields, values.value)
    emit('update:modelValue', serialized)
    emit('validity', validateFormValues(props.fields, values.value))
  },
  { deep: true },
)

watch(
  () => props.modelValue,
  (nv) => {
    const current = serializeFormValues(props.fields, values.value)
    if (JSON.stringify(current) !== JSON.stringify(nv ?? {})) {
      values.value = createFormValues(props.fields, nv ?? {})
    }
  },
)
</script>

<template>
  <a-form layout="vertical">
    <SchemaField
      v-for="f in regular"
      :key="f.key"
      :field="f"
      :reference-options="referenceOptions"
      v-model="values[f.key]"
    />
    <SchemaAdvancedSection v-if="advanced.length > 0" v-model:open="advancedOpen">
      <SchemaField
        v-for="f in advanced"
        :key="f.key"
        :field="f"
        :reference-options="referenceOptions"
        v-model="values[f.key]"
      />
    </SchemaAdvancedSection>
  </a-form>
</template>
