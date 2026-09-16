<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { Table, Button, Tag, message, Space, Tooltip, Modal, Descriptions, DescriptionsItem, Typography } from 'ant-design-vue'
import {
  ArrowUpOutlined, BuildOutlined, DeleteOutlined, EyeOutlined, PauseCircleOutlined,
  PlayCircleOutlined, ReloadOutlined,
} from '@ant-design/icons-vue'
import {
  buildVariant, checkComponents, getVariant, listComponents, restartComponent, startComponent,
  stopComponent, uninstallComponent, upgradeComponent, type ComponentInfo, type VariantInfo,
} from '../../api/components'
import { toolRoute } from '../../utils/toolRoutes'

const router = useRouter()
const [messageApi, contextHolder] = message.useMessage()
const rows = ref<ComponentInfo[]>([])
const loading = ref(false)
const checking = ref(false)
const busy = ref<string>('')

const columns = [
  { title: '名称', dataIndex: 'Name' },
  { title: 'ID', dataIndex: 'ID', width: 140 },
  { title: '形态', key: 'kind', width: 120 },
  { title: '当前版本', key: 'current', width: 140 },
  { title: '最新版本', key: 'latest', width: 140 },
  { title: '状态', key: 'status', width: 100 },
  { title: '操作', key: 'action', width: 240 },
]

const KIND_LABEL: Record<string, string> = {
  core: '核心',
  'caddy-module': 'Caddy 模块',
  process: '独立进程',
  'config-only': '配置注入',
}

// 组件页仅展示已安装项。
const visibleRows = computed(() => rows.value.filter((r) => r.installed))

// 配方变体：展示当前特征并集的变体键，并支持触发按需构建（ADR-038）。
const variantModal = ref<{ open: boolean; info: VariantInfo | null }>({ open: false, info: null })
const variantBusy = ref(false)

async function openVariant(row: ComponentInfo) {
  try {
    const info = await getVariant(row.ID)
    variantModal.value = { open: true, info }
  } catch (e) {
    // 组件的变体接口始终可用；出错多为网络问题。
    messageApi.error((e as Error).message)
  }
}

function openBuildPage() {
  if (variantModal.value.info?.buildUrl) window.open(variantModal.value.info.buildUrl, '_blank')
}

async function doBuildVariant() {
  if (!variantModal.value.info) return
  variantBusy.value = true
  try {
    const r = await buildVariant(variantModal.value.info.component)
    if (r.mode === 'dispatch') messageApi.success(r.message)
    else messageApi.warning(r.message)
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    variantBusy.value = false
  }
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

async function onCheckAll() {
  checking.value = true
  try {
    rows.value = await checkComponents()
    const n = rows.value.filter((r) => r.updateAvailable).length
    messageApi.success(n ? `${n} 个组件有新版本` : '全部组件已是最新')
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    checking.value = false
  }
}

async function act(row: ComponentInfo, fn: (id: string) => Promise<ComponentInfo>, ok: string) {
  busy.value = row.ID
  try {
    await fn(row.ID)
    messageApi.success(ok)
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    busy.value = ''
    await load()
    window.dispatchEvent(new Event('gatebox:components-changed'))
  }
}

function onView(row: ComponentInfo) {
  router.push(toolRoute(row.ID))
}

const STATUS_LABEL: Record<string, string> = {
  running: '运行中',
  stopped: '已停止',
  error: '有故障',
}

function statusText(s: string) {
  return STATUS_LABEL[s] || '有故障'
}

function statusColor(s: string) {
  if (s === 'running') return 'green'
  if (s === 'error') return 'red'
  return 'default'
}

function canRun(row: ComponentInfo) {
  return row.Capabilities?.includes('runnable') ?? false
}

// 重启：托管进程用 runnable；配置类核心组件（如 caddy）用 restartable。
function canRestart(row: ComponentInfo) {
  return canRun(row) || (row.Capabilities?.includes('restartable') ?? false)
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; justify-content: flex-end">
    <Space>
      <Button @click="onCheckAll" :loading="checking">检查更新</Button>
      <Button @click="load" :loading="loading">刷新</Button>
    </Space>
  </div>
  <Table :columns="columns" :data-source="visibleRows" :loading="loading" row-key="ID" size="middle" :pagination="false">
    <template #bodyCell="{ column, record }">
      <template v-if="column.key === 'kind'">
        <Tag>{{ KIND_LABEL[record.Kind] || record.Kind }}</Tag>
      </template>
      <template v-else-if="column.key === 'current'">{{ record.current || '—' }}</template>
      <template v-else-if="column.key === 'latest'">
        <span>{{ record.latest || '—' }}</span>
        <Tag v-if="record.updateAvailable" color="red" style="margin-left: 6px">有更新</Tag>
      </template>
      <template v-else-if="column.key === 'status'">
        <Tag :color="statusColor(record.status?.State)">{{ statusText(record.status?.State) }}</Tag>
      </template>
      <template v-else-if="column.key === 'action'">
        <Space :size="4">
          <Tooltip title="配方变体">
            <Button type="text" size="small" @click="openVariant(record)">
              <template #icon><BuildOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip title="升级">
            <Button
              type="text"
              size="small"
              :disabled="!record.updateAvailable"
              :loading="busy === record.ID"
              @click="act(record, upgradeComponent, '升级完成')"
            >
              <template #icon><ArrowUpOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip title="启动">
            <Button
              type="text"
              size="small"
              :disabled="record.Tier === 'core' || !canRun(record) || record.status?.State === 'running'"
              :loading="busy === record.ID"
              @click="act(record, startComponent, '已启动')"
            >
              <template #icon><PlayCircleOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip title="停止">
            <Button
              type="text"
              size="small"
              :disabled="record.Tier === 'core' || !canRun(record) || record.status?.State !== 'running'"
              :loading="busy === record.ID"
              @click="act(record, stopComponent, '已停止')"
            >
              <template #icon><PauseCircleOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip title="重启">
            <Button
              type="text"
              size="small"
              :disabled="!canRestart(record) || record.status?.State !== 'running'"
              :loading="busy === record.ID"
              @click="act(record, restartComponent, '已重启')"
            >
              <template #icon><ReloadOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip title="查看">
            <Button type="text" size="small" @click="onView(record)">
              <template #icon><EyeOutlined /></template>
            </Button>
          </Tooltip>
          <Tooltip title="卸载">
            <Button
              type="text"
              size="small"
              danger
              :disabled="!record.Removable"
              :loading="busy === record.ID"
              @click="act(record, uninstallComponent, '已卸载')"
            >
              <template #icon><DeleteOutlined /></template>
            </Button>
          </Tooltip>
        </Space>
      </template>
    </template>
  </Table>

  <Modal
    :open="variantModal.open"
    title="组件配方变体"
    :footer="null"
    @cancel="variantModal.open = false"
  >
    <template v-if="variantModal.info">
      <Descriptions :column="1" size="small" bordered>
        <DescriptionsItem label="组件">{{ variantModal.info.component }}</DescriptionsItem>
        <DescriptionsItem label="版本">{{ variantModal.info.version || '—' }}</DescriptionsItem>
        <DescriptionsItem label="特征并集">
          {{ variantModal.info.features.length ? variantModal.info.features.join('、') : '（无）' }}
        </DescriptionsItem>
        <DescriptionsItem label="变体键">
          <Typography.Text code>{{ variantModal.info.key }}</Typography.Text>
        </DescriptionsItem>
      </Descriptions>
      <Space style="margin-top: 16px">
        <Button type="primary" :loading="variantBusy" @click="doBuildVariant">
          {{ variantModal.info.runnable ? '触发按需构建' : '触发构建（需配置令牌，否则给出手动链接）' }}
        </Button>
        <Button v-if="variantModal.info.buildUrl" @click="openBuildPage">打开发布仓库构建页</Button>
      </Space>
    </template>
  </Modal>
</template>
