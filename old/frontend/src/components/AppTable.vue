<script setup lang="ts">
import { computed } from 'vue'
import { Table } from 'ant-design-vue'
import type { TableProps } from 'ant-design-vue'

interface Props {
  columns: TableProps['columns']
  dataSource: any[]
  loading?: boolean
  rowKey?: string | ((record: any) => string)
  pagination?: TableProps['pagination']
  scroll?: { x?: number | string; y?: number | string }
  size?: 'small' | 'middle' | 'large'
  bordered?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  loading: false,
  rowKey: 'id',
  pagination: () => ({
    pageSize: 20,
    showSizeChanger: true,
    showQuickJumper: true,
    showTotal: (total: number) => `共 ${total} 条`,
  }),
  size: 'middle',
  bordered: false,
})

const emit = defineEmits<{
  (e: 'change', pagination: any, filters: any, sorter: any): void
  (e: 'rowClick', record: any): void
}>()

const tableScroll = computed(() => {
  if (props.scroll) {
    return {
      x: props.scroll.x,
      y: props.scroll.y,
    }
  }
  return undefined
})

function handleChange(pagination: any, filters: any, sorter: any) {
  emit('change', pagination, filters, sorter)
}

function handleRowClick(record: any) {
  emit('rowClick', record)
}
</script>

<template>
  <Table
    :columns="columns"
    :data-source="dataSource"
    :loading="loading"
    :row-key="rowKey"
    :pagination="pagination"
    :scroll="tableScroll"
    :size="size"
    :bordered="bordered"
    @change="handleChange"
  >
    <template #bodyCell="{ column, record }">
      <slot name="bodyCell" :column="column" :record="record" />
    </template>
  </Table>
</template>
