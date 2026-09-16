<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Row, Col, Card, Tag, Empty, Button, message } from 'ant-design-vue'
import { checkComponent, listComponents, startComponent, stopComponent, type ComponentInfo } from '../api/components'

const route = useRoute()
const [messageApi, contextHolder] = message.useMessage()
const items = ref<ComponentInfo[]>([])
const loading = ref(false)
const busy = ref('')

// 服务页聚焦独立进程类组件（tailscale / flame）。
const focused = computed(() => route.query.component as string | undefined)

async function load() {
  loading.value = true
  try {
    const all = await listComponents()
    items.value = all.filter((c) => c.Kind === 'process')
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

async function onToggle(row: ComponentInfo) {
  busy.value = row.ID
  const running = row.status?.State === 'running'
  try {
    const info = running ? await stopComponent(row.ID) : await startComponent(row.ID)
    Object.assign(row, info)
    messageApi.success(running ? '已停止' : '已启动')
  } catch (e) {
    messageApi.error((e as Error).message)
    await load()
  } finally {
    busy.value = ''
  }
}

function stateColor(s: string) {
  if (s === 'running') return 'green'
  if (s === 'error') return 'red'
  return 'default'
}

function isFocused(id: string) {
  return focused.value === id
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
      <Card
        :title="c.Name"
        size="small"
        :style="isFocused(c.ID) ? { borderColor: '#1677ff', boxShadow: '0 0 0 2px rgba(22,119,255,0.15)' } : {}"
      >
        <div style="display: flex; justify-content: space-between; align-items: center">
          <Tag :color="stateColor(c.status?.State)">
            {{ c.installed ? (c.status?.State || 'unknown') : '未安装' }}
          </Tag>
          <span style="color: #888; font-size: 12px">
            当前 {{ c.current || '—' }}
            <template v-if="c.latest"> / 最新 {{ c.latest }}</template>
          </span>
        </div>
        <div style="margin-top: 8px; color: #888; font-size: 12px">
          {{ c.Summary || '独立进程组件' }}
        </div>
        <div style="margin-top: 12px">
          <Button size="small" :loading="busy === c.ID" @click="onCheck(c)">检查更新</Button>
          <Button
            v-if="c.installed"
            size="small"
            :type="c.status?.State === 'running' ? 'default' : 'primary'"
            :style="{ marginLeft: '8px' }"
            :loading="busy === c.ID"
            @click="onToggle(c)"
          >
            {{ c.status?.State === 'running' ? '停止' : '启动' }}
          </Button>
        </div>
      </Card>
    </Col>
    <Col v-if="!loading && items.length === 0" :span="24">
      <Empty description="未发现服务类组件" />
    </Col>
  </Row>
</template>
