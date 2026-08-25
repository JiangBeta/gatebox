<script setup lang="ts">
import { ref, onMounted, h, computed } from 'vue'
import {
  NDataTable, NTag, NButton, NSpace, NModal, NText, NTooltip, NEllipsis, NInput, NSelect, useMessage,
} from 'naive-ui'
import { listVolumes, removeVolume, pruneVolumes, createVolume, type VolumeView } from '../../api/docker'

const message = useMessage()

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
  if (!row.inUse) return h(NTag, { size: 'small', bordered: false }, { default: () => '未使用' })
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NTag, { size: 'small', type: 'warning', bordered: false }, { default: () => '使用中' }),
    default: () => '被容器使用: ' + (row.containers || []).join(', '),
  })
}

const columns = [
  { title: '名称', key: 'name', minWidth: 200, render: (row: VolumeView) => h(NEllipsis, null, { default: () => row.name }) },
  {
    title: '驱动',
    key: 'driver',
    width: 90,
    render: (row: VolumeView) => h(NTag, { size: 'small', bordered: false }, { default: () => row.driver }),
  },
  {
    title: '挂载点',
    key: 'mountpoint',
    minWidth: 220,
    render: (row: VolumeView) => h(NEllipsis, { depth: 3, style: 'font-size: 12px' }, { default: () => row.mountpoint }),
  },
  { title: '是否使用', key: 'usage', width: 100, render: (row: VolumeView) => renderUsage(row) },
  { title: '创建时间', key: 'createdAt', width: 132, render: (row: VolumeView) => fmtTime(row.createdAt) },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right' as const,
    render: (row: VolumeView) =>
      h(NTooltip, { trigger: 'hover', disabled: !row.inUse }, {
        trigger: () =>
          h(NButton, {
            size: 'tiny',
            type: 'error',
            ghost: true,
            disabled: row.inUse,
            onClick: () => (removeTarget.value = row),
          }, { default: () => '删除' }),
        default: () => '被容器使用: ' + (row.containers || []).join(', '),
      }),
  },
]

async function doRemove() {
  const row = removeTarget.value
  if (!row) return
  removeBusy.value = true
  try {
    await removeVolume(row.name)
    message.success('卷已删除')
    removeTarget.value = null
    await load()
  } catch (e: any) {
    message.error('删除失败 — ' + e.message)
  } finally {
    removeBusy.value = false
  }
}

async function doPrune() {
  pruneBusy.value = true
  try {
    pruneResult.value = await pruneVolumes()
    message.success('清理完成')
    await load()
  } catch (e: any) {
    message.error('清理失败 — ' + e.message)
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
    message.warning('请填写卷名')
    return
  }
  createBusy.value = true
  try {
    await createVolume({ name: createName.value.trim(), driver: createDriver.value })
    message.success('卷已创建')
    createShow.value = false
    await load()
  } catch (e: any) {
    message.error('创建失败 — ' + e.message)
  } finally {
    createBusy.value = false
  }
}

onMounted(load)
</script>

<template>
  <n-alert v-if="loadError" type="error" :show-icon="true" style="margin-bottom: 12px">
    {{ loadError }}
  </n-alert>
  <n-alert v-if="warnings.length" type="warning" :show-icon="true" style="margin-bottom: 12px">
    {{ warnings.join('; ') }}
  </n-alert>

  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px">
    <div style="font-size: 13px; color: #666">
      共 {{ volumes.length }} 个卷 · {{ unusedCount }} 个未使用
    </div>
    <n-space>
      <n-button size="small" type="warning" ghost :disabled="unusedCount === 0" @click="pruneShow = true">
        清理未使用
      </n-button>
      <n-button size="small" type="primary" @click="openCreate">+ 创建卷</n-button>
    </n-space>
  </div>

  <n-data-table
    :columns="columns"
    :data="volumes"
    :loading="loading"
    :row-key="(row: VolumeView) => row.name"
    :scroll-x="832"
    size="small"
  />

  <!-- 清理未使用确认 -->
  <n-modal
    v-model:show="pruneShow"
    preset="dialog"
    type="warning"
    title="清理未使用的卷"
    positive-text="确认清理"
    negative-text="取消"
    :loading="pruneBusy"
    @positive-click="doPrune"
  >
    <div style="display: flex; flex-direction: column; gap: 10px">
      <span>
        将删除全部 <b>{{ unusedCount }}</b> 个未被任何容器使用的卷，释放被占用的磁盘空间。
      </span>
      <n-text depth="3" style="font-size: 12px">
        孤儿卷是 HomeLab 磁盘被吃满的常见原因。已使用中的卷不会被删除。
      </n-text>
      <div v-if="pruneResult" style="font-size: 13px">
        上次清理：删除 {{ pruneResult.deleted.length }} 个，释放 {{ fmtBytes(pruneResult.reclaimed) }}
      </div>
    </div>
  </n-modal>

  <!-- 创建卷 -->
  <n-modal
    v-model:show="createShow"
    preset="card"
    title="创建卷"
    style="width: 440px"
  >
    <div style="display: flex; flex-direction: column; gap: 14px">
      <div>
        <n-text depth="3" style="font-size: 12px">卷名</n-text>
        <n-input v-model:value="createName" placeholder="my-volume" />
      </div>
      <div>
        <n-text depth="3" style="font-size: 12px">驱动</n-text>
        <n-select v-model:value="createDriver" :options="driverOptions" />
      </div>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button type="primary" :loading="createBusy" @click="doCreate">创建</n-button>
        <n-button @click="createShow = false">取消</n-button>
      </n-space>
    </template>
  </n-modal>

  <!-- 删除确认 -->
  <n-modal
    :show="!!removeTarget"
    preset="dialog"
    type="error"
    title="删除卷"
    positive-text="确认删除"
    negative-text="取消"
    :loading="removeBusy"
    @positive-click="doRemove"
    @negative-click="removeTarget = null"
    @close="removeTarget = null"
  >
    确定删除卷 <b>{{ removeTarget?.name }}</b> 吗？卷内数据将丢失，且不可恢复。
  </n-modal>
</template>
