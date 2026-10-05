<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import { listObjects, type V41Object } from '@/api/objects'
import ObjectKindPage from '@/app/components/objects/ObjectKindPage.vue'

/**
 * 根域名对象（V4.1 `domain` kind）。
 *
 * 与「域名管理」tab 的区别：那个页管的是证书/DNS 凭证（旧模型），
 * 这里管的是**被路由引用**的根域名对象 —— 路由的 `roots` 存的就是这里的 id。
 * 迁移时旧 domain 的 credentialId 没跟着搬过来（dns-credential 尚未对象化），
 * 所以本页的 DNS 凭证字段会被抽屉明确说明为「本版本还不支持」而不是给个空下拉。
 */
const domains = ref<V41Object[]>([])
const routeCount = ref<Record<string, number>>({})

async function loadRefs() {
  try {
    const [doms, routes] = await Promise.all([listObjects('domain'), listObjects('route')])
    domains.value = doms
    const counter: Record<string, number> = {}
    for (const r of routes) {
      for (const root of (r.spec?.roots ?? []) as unknown[]) {
        const id = String(root)
        counter[id] = (counter[id] ?? 0) + 1
      }
    }
    routeCount.value = counter
  } catch {
    /* 引用计数拿不到就不显示这一列，不阻塞页面 */
  }
}

const columns: ColumnType<V41Object>[] = [
  {
    title: '根域名',
    dataIndex: 'name',
    key: 'name',
    ellipsis: true,
    customRender: ({ record }) => String(record.spec?.name ?? record.key),
  },
  {
    title: '备注',
    dataIndex: 'remark',
    key: 'remark',
    ellipsis: true,
    customRender: ({ record }) => String(record.spec?.remark ?? '') || '—',
  },
  {
    title: '被引用',
    key: 'refs',
    width: 90,
    customRender: ({ record }) => {
      const n = routeCount.value[record.id] ?? 0
      return n === 0 ? '未被引用' : `${n} 条路由`
    },
  },
]

onMounted(loadRefs)

const pageKey = computed(() => 'domain')
</script>

<template>
  <ObjectKindPage
    :key="pageKey"
    kind="domain"
    title="根域名对象"
    description="路由引用的是这里的 id；二级域名不建实体，直接写在路由上。DNS 凭证字段本版本暂不可用（迁移报告里列出了待重选的旧凭证）。"
    :extra-columns="columns"
  />
</template>