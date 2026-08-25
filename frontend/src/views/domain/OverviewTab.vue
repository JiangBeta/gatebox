<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { NGrid, NGridItem, NCard, NStatistic, NDataTable, NTag } from 'naive-ui'
import { overview, type OverviewResponse } from '../../api/domains'

const data = ref<OverviewResponse>({
  domainCount: 0,
  credentialCount: 0,
  providerCount: 0,
  certTotal: 0,
  certExpiringSoon: 0,
  certExpired: 0,
  domains: [],
})

const certStatusMap: Record<string, { type: 'success' | 'warning' | 'error' | 'default'; label: string }> = {
  success: { type: 'success', label: '成功' },
  expiring: { type: 'warning', label: '将过期' },
  expired: { type: 'error', label: '已过期' },
  unissued: { type: 'default', label: '未申请' },
}

function formatTime(s: string) {
  if (!s) return '-'
  return s.replace('T', ' ').slice(0, 19)
}

const columns = [
  { title: '域名', key: 'name' },
  {
    title: '证书',
    key: 'certStatus',
    render: (row: any) => {
      const m = certStatusMap[row.certStatus] || certStatusMap.unissued
      return h(NTag, { type: m.type, size: 'small' }, { default: () => m.label })
    },
  },
  { title: '二级域名', key: 'subdomainCount' },
  { title: '创建时间', key: 'createdAt', render: (row: any) => formatTime(row.createdAt) },
  { title: '最近签发时间', key: 'lastIssuedAt', render: (row: any) => formatTime(row.lastIssuedAt) },
]

async function load() {
  data.value = await overview()
}
onMounted(load)
</script>

<template>
  <n-grid :cols="6" :x-gap="12" :y-gap="12">
    <n-grid-item>
      <n-card><n-statistic label="域名数量" :value="data.domainCount" /></n-card>
    </n-grid-item>
    <n-grid-item>
      <n-card><n-statistic label="证书总数" :value="data.certTotal" /></n-card>
    </n-grid-item>
    <n-grid-item>
      <n-card><n-statistic label="即将过期" :value="data.certExpiringSoon" /></n-card>
    </n-grid-item>
    <n-grid-item>
      <n-card><n-statistic label="已过期" :value="data.certExpired" /></n-card>
    </n-grid-item>
    <n-grid-item>
      <n-card><n-statistic label="DNS 供应商" :value="data.providerCount" /></n-card>
    </n-grid-item>
    <n-grid-item>
      <n-card><n-statistic label="DNS 凭证" :value="data.credentialCount" /></n-card>
    </n-grid-item>
  </n-grid>

  <n-data-table :columns="columns" :data="data.domains" style="margin-top: 16px" />
</template>
