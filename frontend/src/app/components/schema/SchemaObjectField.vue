<script setup lang="ts">
import type { ConfigField, FieldOption } from '@/lib/schema'
import SchemaField from './SchemaField.vue'

const props = defineProps<{ field: ConfigField; referenceOptions?: Record<string, FieldOption[]> }>()
const model = defineModel<Record<string, unknown>>({ default: () => ({}) })

const childRefOptions = props.referenceOptions
</script>

<template>
  <div class="schema-object">
    <SchemaField
      v-for="child in props.field.fields ?? []"
      :key="child.key"
      :field="child"
      :reference-options="childRefOptions"
      v-model="model[child.key]"
    />
  </div>
</template>

<style scoped>
.schema-object {
  padding-left: 12px;
  border-left: 2px solid rgba(0, 0, 0, 0.06);
}
</style>
