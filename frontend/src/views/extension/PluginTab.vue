<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Table, Button, Tag, message, Space } from 'ant-design-vue'
import {
  disablePlugin,
  enablePlugin,
  installPlugin,
  listPlugins,
  removePlugin,
  type PluginView,
} from '../../api/plugins'

const [messageApi, contextHolder] = message.useMessage()
const rows = ref<PluginView[]>([])
const loading = ref(false)
const busy = ref<string>('')

const columns = [
  { title: '名称', dataIndex: 'name', width: 170 },
  { title: 'ID', dataIndex: 'id', width: 110 },
  { title: '形态', key: 'kind', width: 120 },
  { title: '版本', dataIndex: 'version', width: 90 },
  { title: '说明', dataIndex: 'summary' },
  { title: '状态', key: 'state', width: 100 },
  { title: '操作', key: 'action', width: 280 },
]

const KIND_LABEL: Record<string, string> = {
  'caddy-module': 'Caddy 模块',
  process: '独立进程',
  'config-only': '配置注入',
}

const STATE_LABEL: Record<string, string> = {
  available: '未安装',
  installed: '已安装',
  enabled: '已启用',
  disabled: '已停用',
  error: '异常',
}

async function load() {
  loading.value = true
  try {
    rows.value = await listPlugins()
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function act(row: PluginView, fn: (id: string) => Promise<PluginView>, ok: string) {
  busy.value = row.id
  try {
    const v = await fn(row.id)
    Object.assign(row, v)
    messageApi.success(ok)
  } catch (e) {
    messageApi.error((e as Error).message)
    await load()
  } finally {
    busy.value = ''
  }
}

function stateColor(s: string) {
  switch (s) {
    case 'enabled':
      return 'green'
    case 'installed':
      return 'blue'
    case 'disabled':
      return 'orange'
    case 'error':
      return 'red'
    default:
      return 'default'
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px">
    <Button @click="load" :loading="loading">刷新</Button>
  </div>
  <Table :columns="columns" :data-source="rows" :loading="loading" row-key="id" size="middle" :pagination="false">
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'kind'">
        <Tag>{{ KIND_LABEL[record.kind] || record.kind }}</Tag>
      </template>
      <template v-else-if="column.key === 'state'">
        <Tag :color="stateColor(record.state)">{{ STATE_LABEL[record.state] || record.state }}</Tag>
      </template>
      <template v-else-if="column.key === 'action'">
        <Space>
          <Button
            v-if="record.state === 'available' || record.state === 'error'"
            size="small"
            type="primary"
            :loading="busy === record.id"
            @click="act(record, installPlugin, '已安装')"
          >
            安装
          </Button>
          <Button
            v-if="record.state === 'installed' || record.state === 'disabled'"
            size="small"
            :loading="busy === record.id"
            @click="act(record, enablePlugin, '已启用')"
          >
            启用
          </Button>
          <Button
            v-if="record.state === 'enabled'"
            size="small"
            :loading="busy === record.id"
            @click="act(record, disablePlugin, '已停用')"
          >
            停用
          </Button>
          <Button
            v-if="record.state !== 'available'"
            size="small"
            danger
            :loading="busy === record.id"
            @click="act(record, removePlugin, '已卸载')"
          >
            卸载
          </Button>
        </Space>
      </template>
    </template>
  </Table>
</template>
