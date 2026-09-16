<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Table, Button, Tag, message, Space, Tooltip, Radio, Modal, Alert, Typography } from 'ant-design-vue'
import { DeleteOutlined, DownloadOutlined, EyeOutlined } from '@ant-design/icons-vue'
import { installStore, listStore, removeStore, type StoreItem } from '../../api/store'
import { toolRoute } from '../../utils/toolRoutes'

const router = useRouter()
const [messageApi, contextHolder] = message.useMessage()
const rows = ref<StoreItem[]>([])
const loading = ref(false)
const busy = ref<string>('')
const filter = ref<'all' | 'installed' | 'available'>('all')

const columns = [
  { title: '名称', dataIndex: 'name', width: 180 },
  { title: 'ID', dataIndex: 'id', width: 120 },
  { title: '标签', key: 'tags', width: 170 },
  { title: '版本', dataIndex: 'version', width: 90 },
  { title: '说明', dataIndex: 'summary' },
  { title: '状态', key: 'state', width: 100 },
  { title: '操作', key: 'action', width: 140 },
]

const visibleRows = computed(() => {
  if (filter.value === 'installed') return rows.value.filter((r) => r.installed)
  if (filter.value === 'available') return rows.value.filter((r) => !r.installed)
  return rows.value
})

async function load() {
  loading.value = true
  try {
    rows.value = await listStore()
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function act(row: StoreItem, fn: (id: string) => Promise<StoreItem>, ok: string) {
  busy.value = row.id
  try {
    await fn(row.id)
    messageApi.success(ok)
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    busy.value = ''
    await load()
    window.dispatchEvent(new Event('gatebox:components-changed'))
  }
}

function onView(row: StoreItem) {
  router.push(toolRoute(row.id))
}

// 权限授予：安装前展示插件声明的权限并要求确认（ADR-039 §2）。
const permModal = ref<{ open: boolean; row: StoreItem | null }>({ open: false, row: null })

function permLines(row: StoreItem): string[] {
  const out: string[] = []
  for (const p of row.permissions || []) {
    if (p.api?.length) out.push(`API 访问：${p.api.join(', ')}`)
    if (p.filesystem) {
      for (const [mode, paths] of Object.entries(p.filesystem)) {
        out.push(`文件系统 ${mode}：${paths.join(', ')}`)
      }
    }
    if (p.network?.length) out.push(`网络：${p.network.join(', ')}`)
  }
  return out
}

function onInstall(row: StoreItem) {
  if (!row.permissions?.length) {
    void act(row, installStore, '已安装')
    return
  }
  permModal.value = { open: true, row }
}

function confirmInstall() {
  const row = permModal.value.row
  permModal.value.open = false
  if (row) void act(row, installStore, '已安装')
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; justify-content: space-between; align-items: center">
    <Radio.Group v-model:value="filter" button-style="solid">
      <Radio.Button value="all">全部插件</Radio.Button>
      <Radio.Button value="installed">已安装</Radio.Button>
      <Radio.Button value="available">未安装</Radio.Button>
    </Radio.Group>
    <Button @click="load" :loading="loading">刷新</Button>
  </div>
  <Table :columns="columns" :data-source="visibleRows" :loading="loading" row-key="id" size="middle" :pagination="false">
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'tags'">
        <Tag v-for="t in record.tags" :key="t">{{ t }}</Tag>
      </template>
      <template v-else-if="column.key === 'state'">
        <Tag :color="record.installed ? 'green' : 'default'">{{ record.installed ? '已安装' : '未安装' }}</Tag>
      </template>
      <template v-else-if="column.key === 'action'">
        <Space :size="4">
          <Tooltip title="查看">
            <Button type="text" size="small" @click="onView(record)">
              <template #icon><EyeOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip v-if="!record.installed" title="安装">
            <Button
              type="text"
              size="small"
              :loading="busy === record.id"
              @click="onInstall(record)"
            >
              <template #icon><DownloadOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip v-else title="卸载">
            <Button
              type="text"
              size="small"
              danger
              :loading="busy === record.id"
              @click="act(record, removeStore, '已卸载')"
            >
              <template #icon><DeleteOutlined /></template>
            </Button>
          </Tooltip>
        </Space>
      </template>
    </template>
  </Table>

  <Modal
    :open="permModal.open"
    :title="`安装「${permModal.row?.name || ''}」需要以下权限`"
    ok-text="同意并安装"
    cancel-text="取消"
    @ok="confirmInstall"
    @cancel="permModal.open = false"
  >
    <Alert
      type="warning"
      show-icon
      message="插件制品在设备上执行代码，且后端以 GateBox 同权限运行"
      style="margin-bottom: 12px"
    />
    <ul style="margin: 0; padding-left: 20px">
      <li v-for="(line, i) in permModal.row ? permLines(permModal.row) : []" :key="i">
        <Typography.Text code>{{ line }}</Typography.Text>
      </li>
    </ul>
  </Modal>
</template>
