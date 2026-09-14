<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { Card, Col, Row, Statistic, Table, Tag } from 'ant-design-vue'
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
  { title: '域名', dataIndex: 'name', key: 'name' },
  {
    title: '证书',
    dataIndex: 'certStatus',
    key: 'certStatus',
    customRender: ({ record }: { record: any }) => {
      const m = certStatusMap[record.certStatus] || certStatusMap.unissued
      return h(Tag, { color: m.type, size: 'small' }, { default: () => m.label })
    },
  },
  { title: '二级域名', dataIndex: 'subdomainCount', key: 'subdomainCount' },
  {
    title: '创建时间',
    dataIndex: 'createdAt',
    key: 'createdAt',
    customRender: ({ record }: { record: any }) => formatTime(record.createdAt),
  },
  {
    title: '最近签发时间',
    dataIndex: 'lastIssuedAt',
    key: 'lastIssuedAt',
    customRender: ({ record }: { record: any }) => formatTime(record.lastIssuedAt),
  },
]

async function load() {
  data.value = await overview()
}
onMounted(load)
</script>

<template>
  <Row :gutter="[12, 12]">
    <Col :span="4">
      <Card><Statistic title="域名数量" :value="data.domainCount" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="证书总数" :value="data.certTotal" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="即将过期" :value="data.certExpiringSoon" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="已过期" :value="data.certExpired" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="DNS 供应商" :value="data.providerCount" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="DNS 凭证" :value="data.credentialCount" /></Card>
    </Col>
  </Row>

  <Table :columns="columns" :data-source="data.domains" style="margin-top: 16px" />
</template>
