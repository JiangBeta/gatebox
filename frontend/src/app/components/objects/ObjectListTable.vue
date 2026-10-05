<script setup lang="ts">
import { computed, h, ref, type VNode } from 'vue'
import { Empty, Table, Input } from 'ant-design-vue'
import { DeleteOutlined } from '@ant-design/icons-vue'
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import type { V41Object, ObjectState } from '@/api/objects'
import ObjectStateBadge from './ObjectStateBadge.vue'
import { displayName } from '@/app/composables/useObjectOptions'
import { opCell, OP_COLOR, type OpSpec } from '@/utils/cell'

/**
 * 对象册表格（V4.1 通用列表，ADR-043 §2 + 原型 listCols）。
 *
 * 列 = 名称 / 各 kind 的业务列 / 状态 / 操作。原型的通用列表没有「主键」列：
 * id 属于配置细节，放抽屉详情里看就够了，列表塞一列 20 字符的十六进制 id
 * 只是把真正要看的字段挤到横向滚动条外面（排查引用时可在抽屉里复制）。
 * 状态列常驻是因为「存了但没生效」是这代模型最常见的中间态，藏进详情等于让用户猜。
 */
const props = withDefaults(
  defineProps<{
    objects: V41Object[]
    loading?: boolean
    /** 额外列（各页按 kind 自定义，如路由的域名/入口点）。 */
    extraColumns?: ColumnType<V41Object>[]
    /** 额外行内操作（插在「删除」之前）。各页只给真有后端动作的项，别摆装饰按钮。 */
    extraOps?: (o: V41Object) => OpSpec[]
    /** 名称列渲染（默认 = displayName）。路由页要用「名称 + 引用服务副行」覆盖。 */
    nameRender?: (o: V41Object) => VNode
    /** 只读类型（catalog）不给删除入口：目录是模型下发的，删了等于改了别人的字典。 */
    readonly?: boolean
    rowKey?: (o: V41Object) => string
    emptyText?: string
  }>(),
  {
    loading: false,
    extraColumns: () => [],
    extraOps: undefined,
    nameRender: undefined,
    readonly: false,
    rowKey: (o: V41Object) => o.id,
    emptyText: '暂无对象',
  },
)

const emit = defineEmits<{ (e: 'edit', o: V41Object): void; (e: 'remove', o: V41Object): void }>()

const keyword = ref('')

const columns = computed<ColumnType<V41Object>[]>(() => [
  {
    title: '名称',
    dataIndex: 'id',
    key: 'name',
    ellipsis: true,
    customRender: ({ record }) =>
      props.nameRender
        ? props.nameRender(record)
        : h('div', {}, h('span', { class: 'row-link' }, displayName(record))),
  },
  ...props.extraColumns,
  {
    title: '状态',
    key: 'state',
    width: 120,
    customRender: ({ record }) =>
      h(ObjectStateBadge, {
        state: (record.status?.state as ObjectState) || undefined,
        error: typeof record.status?.error === 'string' ? record.status.error : undefined,
        pending: Boolean(record.status?.pendingOp),
      }),
  },
  {
    title: '操作',
    key: 'ops',
    width: 96,
    // 「编辑」不在这里（views.md §6）：点整行即编辑，操作列只留破坏性动作
    // 和各页显式给的 extraOps（那些是真能落地的动作，不是装饰）。
    customRender: ({ record }) =>
      opCell(
        ...(props.extraOps ? props.extraOps(record) : []),
        props.readonly
          ? null
          : {
              tip: '删除',
              icon: DeleteOutlined,
              color: OP_COLOR.delete,
              onClick: () => emit('remove', record),
            },
      ),
  },
])

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase()
  if (!kw) return props.objects
  return props.objects.filter((o) => {
    const hay = [o.id, o.key, displayName(o), JSON.stringify(o.spec ?? {})].join(' ').toLowerCase()
    return hay.includes(kw)
  })
})

/** 点整行即打开编辑抽屉（views.md §6：操作列不含「查看/编辑」）。 */
function customRow(record: V41Object) {
  return {
    onClick: () => emit('edit', record),
    style: { cursor: 'pointer' },
  }
}

</script>

<template>
  <div class="object-list gb-table">
    <div class="object-list-toolbar">
      <Input
        v-model:value="keyword"
        placeholder="搜索名称 / id / 字段"
        allow-clear
        class="object-list-search"
      />
      <slot name="toolbar" />
    </div>
    <Table
      :columns="columns"
      :data-source="filtered"
      :loading="loading"
      :row-key="rowKey"
      :pagination="{ pageSize: 20, showSizeChanger: true, size: 'small' }"
      size="small"
      :scroll="{ x: 'max-content' }"
      :custom-row="customRow"
    >
      <template #emptyText>
        <Empty :description="emptyText" />
      </template>
    </Table>
  </div>
</template>

<style scoped>
.object-list {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-sm);
}
.object-list-toolbar {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
}
.object-list-search {
  max-width: 280px;
}
</style>
