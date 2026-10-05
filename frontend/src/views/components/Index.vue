<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import type { V41Object } from '@/api/objects'
import ComponentTab from '../extension/ComponentTab.vue'
import PluginTab from '../extension/PluginTab.vue'
import TopologyView from '@/modules/topology/views/Index.vue'
import ObjectKindPage from '@/app/components/objects/ObjectKindPage.vue'

/**
 * 组件（8091 原型首 tab 是「能力」；原型只画了这一面，
 * 其余三面沿用现有实现，别把插件/组件管理弄丢）。
 *
 * 能力 = `ability` 对象（模型自述的能力字典，catalog 只读，不给新建/编辑）。
 */
const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'ability')

const abilityColumns: ColumnType<V41Object>[] = [
  { title: 'ID', dataIndex: 'id', key: 'id', width: 160 },
  {
    title: '名称',
    dataIndex: 'label',
    key: 'label',
    width: 200,
    customRender: ({ record }) => String(record.spec?.label ?? record.key),
  },
  {
    title: '角色',
    dataIndex: 'role',
    key: 'role',
    width: 140,
    customRender: ({ record }) => String(record.spec?.role ?? '') || '—',
  },
  {
    title: '说明',
    dataIndex: 'description',
    key: 'description',
    ellipsis: true,
    customRender: ({ record }) => String(record.spec?.description ?? '') || '—',
  },
]
</script>

<template>
  <ComponentTab v-if="active === 'components'" />
  <PluginTab v-else-if="active === 'plugins'" />
  <TopologyView v-else-if="active === 'topology'" />
  <ObjectKindPage
    v-else
    kind="ability"
    title="能力字典"
    description="组件自述的能力（load / serve / start…），由类型目录下发，只读。"
    readonly
    :extra-columns="abilityColumns"
  />
</template>