<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ArrayItemValue, ConfigField, FieldOption } from '@/lib/schema'
import { nextId } from '@/lib/schema'
import SchemaField from './SchemaField.vue'

const props = defineProps<{ field: ConfigField; referenceOptions?: Record<string, FieldOption[]> }>()
const model = defineModel<ArrayItemValue[]>({ default: () => [] })
const refOptions = props.referenceOptions

const items = computed(() => model.value ?? [])
const isObjectItem = computed(() => props.field.item?.type === 'object')

function addItem() {
  const item = props.field.item
  let value: unknown = ''
  if (item?.type === 'object') {
    const obj: Record<string, unknown> = {}
    for (const c of item.fields ?? []) obj[c.key] = c.type === 'switch' ? false : ''
    value = obj
  }
  model.value = [...items.value, { id: nextId(), value }]
}

function removeItem(id: string) {
  model.value = items.value.filter((it) => it.id !== id)
}

const collapsed = ref<Record<string, boolean>>({})
function toggle(id: string) {
  collapsed.value = { ...collapsed.value, [id]: !collapsed.value[id] }
}

function summary(item: ArrayItemValue): string {
  const fields = props.field.item?.summaryFields ?? []
  const obj = item.value as Record<string, unknown>
  const keys = fields.length > 0 ? fields : (props.field.item?.fields ?? []).slice(0, 2).map((f) => f.key)
  const parts = keys.map((k) => obj?.[k]).filter((v) => v !== undefined && v !== '' && v !== null)
  return parts.length > 0 ? parts.map((v) => String(v)).join(' · ') : '未配置'
}
</script>

<template>
  <div class="schema-array">
    <div v-for="item in items" :key="item.id" class="schema-array__row">
      <template v-if="isObjectItem">
        <div class="schema-array__head">
          <a-button type="text" size="small" @click="toggle(item.id)">
            {{ collapsed[item.id] ? '展开' : '收起' }}
          </a-button>
          <span class="schema-array__summary">{{ summary(item) }}</span>
          <a-button type="text" danger size="small" @click="removeItem(item.id)">删除</a-button>
        </div>
        <div v-show="!collapsed[item.id]">
          <SchemaField
            v-for="child in props.field.item?.fields ?? []"
            :key="child.key"
            :field="child"
            :reference-options="refOptions"
            v-model="(item.value as Record<string, unknown>)[child.key]"
          />
        </div>
      </template>
      <div v-else class="schema-array__scalar">
        <SchemaField
          :field="{ ...(props.field.item ?? { type: 'text' }), key: 'value', label: props.field.item?.label ?? '' }"
          v-model="item.value"
        />
        <a-button type="text" danger size="small" @click="removeItem(item.id)">删除</a-button>
      </div>
    </div>
    <a-button type="dashed" block @click="addItem">+ 新增</a-button>
  </div>
</template>

<style scoped>
.schema-array__row {
  margin-bottom: 8px;
  padding: 8px;
  border: 1px solid rgba(0, 0, 0, 0.08);
  border-radius: 6px;
}
.schema-array__head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.schema-array__summary {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgba(0, 0, 0, 0.45);
}
.schema-array__scalar {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
