<script setup lang="ts">
import type { ConfigField, FieldOption } from '@/lib/schema'
import SchemaArrayField from './SchemaArrayField.vue'
import SchemaObjectField from './SchemaObjectField.vue'
import SchemaReferenceField from './SchemaReferenceField.vue'

const props = defineProps<{ field: ConfigField; referenceOptions?: Record<string, FieldOption[]> }>()
const model = defineModel<unknown>()

function refOptions(): FieldOption[] {
  const type = props.field.reference?.types?.[0]
  return type ? (props.referenceOptions?.[type] ?? []) : []
}
</script>

<template>
  <a-form-item :label="field.label" :required="field.required" :extra="field.description">
    <SchemaArrayField
      v-if="field.type === 'array'"
      v-model="model as never"
      :field="field"
      :reference-options="referenceOptions"
    />
    <SchemaObjectField
      v-else-if="field.type === 'object'"
      v-model="model as never"
      :field="field"
      :reference-options="referenceOptions"
    />
    <SchemaReferenceField
      v-else-if="field.type === 'reference'"
      v-model="model as never"
      :field="field"
      :options="refOptions()"
    />
    <a-switch v-else-if="field.type === 'switch'" v-model:checked="model as never" />
    <a-input-number
      v-else-if="field.type === 'number'"
      v-model:value="model as never"
      :placeholder="field.placeholder"
      style="width: 100%"
    />
    <a-select
      v-else-if="field.type === 'select'"
      v-model:value="model as never"
      :options="field.options"
      :placeholder="field.placeholder"
      allow-clear
      style="width: 100%"
    />
    <a-textarea
      v-else-if="field.type === 'textarea'"
      v-model:value="model as never"
      :placeholder="field.placeholder"
      :rows="3"
    />
    <a-input-password
      v-else-if="field.type === 'password'"
      v-model:value="model as never"
      :placeholder="field.placeholder"
    />
    <a-input v-else v-model:value="model as never" :placeholder="field.placeholder" />
  </a-form-item>
</template>
