<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Table, Button, Tag, message, Space } from 'ant-design-vue'
import { checkComponent, listComponents, upgradeComponent, type ComponentInfo } from '../../api/components'

const [messageApi, contextHolder] = message.useMessage()
const rows = ref<ComponentInfo[]>([])
const loading = ref(false)
const busy = ref<string>('')

const columns = [
  { title: '名称', dataIndex: 'Name' },
  { title: 'ID', dataIndex: 'ID', width: 120 },
  { title: '形态', key: 'kind', width: 120 },
  { title: '档', key: 'tier', width: 80 },
  { title: '当前版本', key: 'current', width: 140 },
  { title: '最新版本', key: 'latest', width: 170 },
  { title: '状态', key: 'status', width: 110 },
  { title: '管理方式', key: 'provision', width: 110 },
  { title: '操作', key: 'action', width: 200 },
]

const KIND_LABEL: Record<string, string> = {
  core: '核心',
  'caddy-module': 'Caddy 模块',
  process: '独立进程',
  'config-only': '配置注入',
}

async function load() {
  loading.value = true
  try {
    rows.value = await listComponents()
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

async function onUpgrade(row: ComponentInfo) {
  busy.value = row.ID
  try {
    const info = await upgradeComponent(row.ID)
    Object.assign(row, info)
    messageApi.success('升级完成')
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    busy.value = ''
  }
}

function statusColor(s: string) {
  if (s === 'running') return 'green'
  if (s === 'error') return 'red'
  if (s === 'stopped') return 'default'
  return 'orange'
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px">
    <Button @click="load" :loading="loading">刷新</Button>
  </div>
  <Table :columns="columns" :data-source="rows" :loading="loading" row-key="ID" size="middle" :pagination="false">
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'kind'">
        <Tag>{{ KIND_LABEL[record.Kind] || record.Kind }}</Tag>
      </template>
      <template v-else-if="column.key === 'tier'">
        <Tag :color="record.Tier === 'core' ? 'blue' : 'default'">
          {{ record.Tier === 'core' ? '核心' : '可选' }}
        </Tag>
      </template>
      <template v-else-if="column.key === 'current'">{{ record.current || '—' }}</template>
      <template v-else-if="column.key === 'latest'">
        <span>{{ record.latest || '—' }}</span>
        <Tag v-if="record.updateAvailable" color="red" style="margin-left: 6px">有更新</Tag>
      </template>
      <template v-else-if="column.key === 'status'">
        <Tag :color="statusColor(record.status?.State)">{{ record.status?.State || 'unknown' }}</Tag>
      </template>
      <template v-else-if="column.key === 'provision'">
        <Tag v-if="record.Provision === 'attached'">系统托管</Tag>
        <span v-else>GateBox</span>
      </template>
      <template v-else-if="column.key === 'action'">
        <Space>
          <Button size="small" :loading="busy === record.ID" @click="onCheck(record)">检查更新</Button>
          <Button
            size="small"
            type="primary"
            :disabled="record.Upgrade !== 'replace'"
            :loading="busy === record.ID"
            @click="onUpgrade(record)"
          >
            升级
          </Button>
        </Space>
      </template>
    </template>
  </Table>
</template>
