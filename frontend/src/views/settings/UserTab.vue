<script setup lang="ts">
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import type { V41Object } from '@/api/objects'
import ObjectKindPage from '@/app/components/objects/ObjectKindPage.vue'

/**
 * 账户（V4.1 `user` kind）。
 *
 * 与「插件/组件」里的用户不是一回事：这里管的是 **basic_auth 中间件引用的账户**
 * （model/user.yaml），密码走 writeOnly：只写、bcrypt、不回显、不进对象文本。
 * 中间件的「用户」多选就是从这里挑 id。
 */
const columns: ColumnType<V41Object>[] = [
  {
    title: '用户名',
    dataIndex: 'name',
    key: 'name',
    ellipsis: true,
    customRender: ({ record }) => String(record.spec?.name ?? record.key),
  },
  {
    title: '显示名',
    dataIndex: 'displayName',
    key: 'displayName',
    ellipsis: true,
    customRender: ({ record }) => String(record.spec?.displayName ?? '') || '—',
  },
  {
    title: '启用',
    key: 'enabled',
    width: 80,
    customRender: ({ record }) => (record.spec?.enabled === false ? '否' : '是'),
  },
  {
    title: '密码',
    key: 'password',
    width: 90,
    // 后端只回 passwordSet 布尔值，永不回显哈希本身。
    customRender: ({ record }) => (record.status?.passwordSet ? '已设置' : '未设置'),
  },
  {
    title: '被引用',
    key: 'refs',
    width: 100,
    customRender: ({ record }) => {
      const n = Number(record.status?.refCount ?? 0)
      return n > 0 ? `${n} 处中间件` : '—'
    },
  },
]
</script>

<template>
  <ObjectKindPage
    kind="user"
    title="账户"
    description="basic_auth 中间件引用的账户。密码只写不读：保存时 bcrypt 落库，编辑时留空表示不改。"
    :extra-columns="columns"
  />
</template>