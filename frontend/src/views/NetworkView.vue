<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Row, Col, Card, Tag, Empty, Button, message } from 'ant-design-vue'
import { listComponents, checkComponent, type ComponentInfo } from '../api/components'

const [messageApi, contextHolder] = message.useMessage()
const items = ref<ComponentInfo[]>([])
const loading = ref(false)
const busy = ref('')

const WANTED = ['mosdns', 'tailscale', 'ddns-go', 'flame']

async function load() {
  loading.value = true
  try {
    const all = await listComponents()
    items.value = all.filter((c) => WANTED.includes(c.ID))
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function onCheck(row: ComponentInfo) {
  busy.value = row.ID
  try {
    const info = await checkComponent(row.ID)
    Object.assign(row, info)
    if (info.error) messageApi.warning(`检查失败：${info.error}`)
    else messageApi.success(`最新版本：${info.latest || '未知'}`)
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    busy.value = ''
  }
}

function stateColor(s: string) {
  if (s === 'running') return 'green'
  if (s === 'error') return 'red'
  return 'default'
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px">
    <Button @click="load" :loading="loading">刷新</Button>
  </div>
  <Row :gutter="[16, 16]">
    <Col v-for="c in items" :key="c.ID" :xs="24" :sm="12" :md="8">
      <Card :title="c.Name" size="small">
        <div style="display: flex; justify-content: space-between; align-items: center">
          <Tag :color="stateColor(c.status?.State)">{{ c.status?.State || 'unknown' }}</Tag>
          <span style="color: #888; font-size: 12px">
            当前 {{ c.current || '—' }}
            <template v-if="c.latest"> / 最新 {{ c.latest }}</template>
          </span>
        </div>
        <div style="margin-top: 8px; color: #888; font-size: 12px">
          {{ c.status?.Message || '内网 DNS / 组网 / 导航 组件（P4 接入深度待定）' }}
        </div>
        <div style="margin-top: 12px">
          <Button size="small" :loading="busy === c.ID" @click="onCheck(c)">检查更新</Button>
        </div>
      </Card>
    </Col>
    <Col v-if="!loading && items.length === 0" :span="24">
      <Empty description="未发现网络相关组件" />
    </Col>
  </Row>
</template>
