<script setup lang="ts">
import { ref, onMounted, h, computed } from 'vue'
import { statCell } from '../../utils/cell'
import {
  Table, Tag, Button, Space, Modal, Drawer, Typography, Tooltip, Input, Select, Popover, Alert, Empty, message,
} from 'ant-design-vue'
import { listVolumes, removeVolume, pruneVolumes, createVolume, type VolumeView } from '../../api/docker'

const [messageApi, contextHolder] = message.useMessage()

const volumes = ref<VolumeView[]>([])
const loading = ref(true)
const loadError = ref('')
const warnings = ref<string[]>([])

const removeTarget = ref<VolumeView | null>(null)
const removeBusy = ref(false)

const pruneShow = ref(false)
const pruneBusy = ref(false)
const pruneResult = ref<{ deleted: string[]; reclaimed: number } | null>(null)

const createShow = ref(false)
const createBusy = ref(false)
const createName = ref('')
const createDriver = ref('local')

const driverOptions = [
  { label: 'local', value: 'local' },
  { label: 'nfs', value: 'nfs' },
]

function fmtBytes(n: number): string {
  if (!n) return '-'
  const mib = n / 1048576
  if (mib >= 1024) return (mib / 1024).toFixed(2) + ' GiB'
  return mib.toFixed(1) + ' MiB'
}

function fmtTime(s: string): string {
  if (!s) return '-'
  const d = new Date(s)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

const unusedCount = computed(() => volumes.value.filter((v) => !v.inUse).length)

async function load() {
  loading.value = true
  try {
    const res = await listVolumes()
    volumes.value = res.volumes
    warnings.value = res.warnings || []
    loadError.value = ''
  } catch (e: any) {
    loadError.value = e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

function renderUsage(row: VolumeView) {
  if (!row.inUse) return h(Tag, { size: 'small' }, { default: () => '未使用' })
  return h(Tooltip, { title: '被容器使用: ' + (row.containers || []).join(', ') }, {
    default: () => h(Tag, { size: 'small', color: 'warning' }, { default: () => '使用中' }),
  })
}

const columns = [
  {
    title: '名称',
    dataIndex: 'displayName',
    key: 'displayName',
    width: 160,
    customRender: ({ record }: { record: VolumeView }) =>
      h('div', { style: 'font-weight: 500; overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, record.displayName || '—'),
  },
  {
    title: 'ID',
    dataIndex: 'name',
    key: 'name',
    // 卷 ID 即卷 name(长哈希)。显示前 8 位,mouse hover 弹出框展示完整 ID,
    // 交互效仿「容器 → 端口」列(蓝色虚线下划线 + popover)。
    width: 120,
    customRender: ({ record }: { record: VolumeView }) =>
      h(Popover, { trigger: 'hover', placement: 'right' }, {
        default: () => h('span', {
          style: 'cursor: default; color: #1677ff; border-bottom: 1px dashed #1677ff; font-family: monospace',
        }, record.name.slice(0, 8)),
        content: () => h('div', { style: 'font-family: monospace' }, record.name),
      }),
  },
  {
    title: '驱动',
    dataIndex: 'driver',
    key: 'driver',
    width: 90,
    customRender: ({ record }: { record: VolumeView }) => h(Tag, { size: 'small' }, { default: () => record.driver }),
  },
  {
    title: '挂载点',
    dataIndex: 'mountpoint',
    key: 'mountpoint',
    width: 240,
    customRender: ({ record }: { record: VolumeView }) =>
      h(Popover, { trigger: 'hover', placement: 'right' }, {
        default: () => h('span', { style: 'color: #888; font-size: 12px; font-family: monospace' }, record.mountpoint),
        content: () => h('div', record.mountpoint),
      }),
  },
  { title: '是否使用', key: 'usage', width: 100, customRender: ({ record }: { record: VolumeView }) => renderUsage(record) },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 132, customRender: ({ record }: { record: VolumeView }) => statCell(fmtTime(record.createdAt)) },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right' as const,
    customRender: ({ record }: { record: VolumeView }) =>
      h(Tooltip, { title: record.inUse ? '被容器使用: ' + (record.containers || []).join(', ') : '' }, {
        default: () =>
          h(Button, {
            size: 'small',
            danger: true,
            ghost: true,
            disabled: record.inUse,
            onClick: () => (removeTarget.value = record),
          }, { default: () => '删除' }),
      }),
  },
]

async function doRemove() {
  const row = removeTarget.value
  if (!row) return
  removeBusy.value = true
  try {
    await removeVolume(row.name)
    messageApi.success('卷已删除')
    removeTarget.value = null
    await load()
  } catch (e: any) {
    messageApi.error('删除失败 — ' + e.message)
  } finally {
    removeBusy.value = false
  }
}

async function doPrune() {
  pruneBusy.value = true
  try {
    pruneResult.value = await pruneVolumes()
    messageApi.success('清理完成')
    await load()
  } catch (e: any) {
    messageApi.error('清理失败 — ' + e.message)
  } finally {
    pruneBusy.value = false
  }
}

function openCreate() {
  createName.value = ''
  createDriver.value = 'local'
  createShow.value = true
}

async function doCreate() {
  if (!createName.value.trim()) {
    messageApi.warning('请填写卷名')
    return
  }
  createBusy.value = true
  try {
    await createVolume({ name: createName.value.trim(), driver: createDriver.value })
    messageApi.success('卷已创建')
    createShow.value = false
    await load()
  } catch (e: any) {
    messageApi.error('创建失败 — ' + e.message)
  } finally {
    createBusy.value = false
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <Alert v-if="loadError" type="error" :show-icon="true" :style="{ marginBottom: '12px' }">
    {{ loadError }}
  </Alert>
  <Alert v-if="warnings.length" type="warning" :show-icon="true" :style="{ marginBottom: '12px' }">
    {{ warnings.join('; ') }}
  </Alert>

  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px">
    <div style="font-size: 13px; color: #666">
      共 {{ volumes.length }} 个卷 · {{ unusedCount }} 个未使用
    </div>
    <Space>
      <Button size="small" type="default" ghost danger :disabled="unusedCount === 0" @click="pruneShow = true">
        清理未使用
      </Button>
      <Button size="small" type="primary" @click="openCreate">+ 创建卷</Button>
    </Space>
  </div>

  <Table
    :columns="columns"
    :data-source="volumes"
    :loading="loading"
    :row-key="(record: VolumeView) => record.name"
    :scroll="{ x: 940 }"
    size="small"
  />

  <!-- 清理未使用确认 -->
  <Modal
    :open="pruneShow"
    title="清理未使用的卷"
    :ok-text="'确认清理'"
    :cancel-text="'取消'"
    :confirm-loading="pruneBusy"
    @ok="doPrune"
    @cancel="pruneShow = false"
    @close="pruneShow = false"
  >
    <div style="display: flex; flex-direction: column; gap: 10px">
      <span>
        将删除全部 <b>{{ unusedCount }}</b> 个未被任何容器使用的卷，释放被占用的磁盘空间。
      </span>
      <Typography.Text type="secondary" style="font-size: 12px">
        孤儿卷是 HomeLab 磁盘被吃满的常见原因。已使用中的卷不会被删除。
      </Typography.Text>
      <div v-if="pruneResult" style="font-size: 13px">
        上次清理：删除 {{ pruneResult.deleted.length }} 个，释放 {{ fmtBytes(pruneResult.reclaimed) }}
      </div>
    </div>
  </Modal>

  <!-- 创建卷 -->
  <Drawer
    :open="createShow"
    placement="right"
    :width="440"
    @close="createShow = false"
  >
    <template #title>
      <span class="dw-drawer-title">创建卷</span>
    </template>
      <div style="display: flex; flex-direction: column; gap: 14px">
      <div>
        <div class="dw-label">卷名 <span class="dw-required">*</span></div>
        <Input v-model:value="createName" placeholder="my-volume" />
      </div>
      <div>
        <div class="dw-label">驱动</div>
        <Select v-model:value="createDriver" :options="driverOptions" />
      </div>
      </div>
    <template #footer>
      <div class="dw-footer">
        <Button @click="createShow = false">取消</Button>
        <Button type="primary" :loading="createBusy" @click="doCreate">创建</Button>
      </div>
    </template>
  </Drawer>

  <!-- 删除确认 -->
  <Modal
    :open="!!removeTarget"
    title="删除卷"
    :ok-text="'确认删除'"
    :cancel-text="'取消'"
    :confirm-loading="removeBusy"
    @ok="doRemove"
    @cancel="removeTarget = null"
    @close="removeTarget = null"
  >
    确定删除卷 <b>{{ removeTarget?.name }}</b> 吗？卷内数据将丢失，且不可恢复。
  </Modal>
</template>
