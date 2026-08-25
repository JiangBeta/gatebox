<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  NDataTable, NTag, NButton, NSpace, NModal, NInput, NSelect, NSwitch, NText,
  NDescriptions, NDescriptionsItem, NEmpty, useMessage,
} from 'naive-ui'
import {
  listNetworks, createNetwork, inspectNetwork, removeNetwork,
  type NetworkView, type NetworkDetail,
} from '../../api/docker'

const message = useMessage()

const networks = ref<NetworkView[]>([])
const loading = ref(true)
const loadError = ref('')

const createShow = ref(false)
const createBusy = ref(false)
const createName = ref('')
const createDriver = ref('bridge')
const createInternal = ref(false)
const createAttachable = ref(false)

const detail = ref<NetworkDetail | null>(null)
const detailShow = ref(false)

const removeTarget = ref<NetworkView | null>(null)
const removeBusy = ref(false)

const driverOptions = [
  { label: 'bridge', value: 'bridge' },
  { label: 'host', value: 'host' },
  { label: 'overlay', value: 'overlay' },
  { label: 'macvlan', value: 'macvlan' },
  { label: 'ipvlan', value: 'ipvlan' },
  { label: 'none', value: 'none' },
]

function fmtTime(s: string): string {
  if (!s) return '-'
  const d = new Date(s)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function load() {
  loading.value = true
  try {
    networks.value = await listNetworks()
    loadError.value = ''
  } catch (e: any) {
    loadError.value = e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

const columns = [
  { title: '名称', key: 'name', minWidth: 160 },
  {
    title: '驱动',
    key: 'driver',
    width: 100,
    render: (row: NetworkView) => h(NTag, { size: 'small', bordered: false }, { default: () => row.driver }),
  },
  {
    title: 'IPv4',
    key: 'subnet',
    width: 170,
    render: (row: NetworkView) =>
      h('div', { style: 'display: flex; flex-direction: column; line-height: 1.3' }, [
        h('span', row.subnet || '-'),
        row.internal
          ? h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => '隔离外部访问' })
          : h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => row.gateway || '' }),
      ]),
  },
  { title: '创建时间', key: 'createdAt', width: 132, render: (row: NetworkView) => fmtTime(row.createdAt) },
  {
    title: '操作',
    key: 'actions',
    width: 120,
    fixed: 'right' as const,
    render: (row: NetworkView) =>
      h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'tiny', onClick: () => openDetail(row) }, { default: () => '查看' }),
          h(NButton, {
            size: 'tiny',
            type: 'error',
            ghost: true,
            // 内置网络(bridge/host/none)由 docker 维护,删除没有意义且易误伤
            disabled: ['bridge', 'host', 'none'].includes(row.name),
            onClick: () => (removeTarget.value = row),
          }, { default: () => '删除' }),
        ],
      }),
  },
]

function openCreate() {
  createName.value = ''
  createDriver.value = 'bridge'
  createInternal.value = false
  createAttachable.value = false
  createShow.value = true
}

async function doCreate() {
  if (!createName.value.trim()) {
    message.warning('请填写网络名')
    return
  }
  createBusy.value = true
  try {
    await createNetwork({
      name: createName.value.trim(),
      driver: createDriver.value,
      internal: createInternal.value,
      attachable: createAttachable.value,
    })
    message.success('网络已创建')
    createShow.value = false
    await load()
  } catch (e: any) {
    message.error('创建失败 — ' + e.message)
  } finally {
    createBusy.value = false
  }
}

async function openDetail(row: NetworkView) {
  try {
    detail.value = await inspectNetwork(row.id)
    detailShow.value = true
  } catch (e: any) {
    message.error('读取详情失败 — ' + e.message)
  }
}

async function doRemove() {
  const row = removeTarget.value
  if (!row) return
  removeBusy.value = true
  try {
    await removeNetwork(row.id)
    message.success('网络已删除')
    removeTarget.value = null
    await load()
  } catch (e: any) {
    message.error('删除失败 — ' + e.message)
  } finally {
    removeBusy.value = false
  }
}

onMounted(load)
</script>

<template>
  <n-alert v-if="loadError" type="error" :show-icon="true" style="margin-bottom: 12px">
    {{ loadError }}
  </n-alert>

  <div style="display: flex; justify-content: flex-end; margin-bottom: 12px">
    <n-button size="small" type="primary" @click="openCreate">+ 创建网络</n-button>
  </div>

  <n-data-table
    :columns="columns"
    :data="networks"
    :loading="loading"
    :row-key="(row: NetworkView) => row.id"
    :scroll-x="682"
    size="small"
  />

  <!-- 创建网络 -->
  <n-modal
    v-model:show="createShow"
    preset="card"
    title="创建网络"
    style="width: 440px"
  >
    <div style="display: flex; flex-direction: column; gap: 14px">
      <div>
        <n-text depth="3" style="font-size: 12px">名称</n-text>
        <n-input v-model:value="createName" placeholder="my-network" />
      </div>
      <div>
        <n-text depth="3" style="font-size: 12px">驱动</n-text>
        <n-select v-model:value="createDriver" :options="driverOptions" />
      </div>
      <div style="display: flex; align-items: center; justify-content: space-between">
        <div>
          <div>隔离外部访问</div>
          <n-text depth="3" style="font-size: 12px">内部网络,容器无法访问外网</n-text>
        </div>
        <n-switch v-model:value="createInternal" />
      </div>
      <div style="display: flex; align-items: center; justify-content: space-between">
        <div>
          <div>允许手动附加容器</div>
          <n-text depth="3" style="font-size: 12px">允许 docker network connect 手动接入</n-text>
        </div>
        <n-switch v-model:value="createAttachable" />
      </div>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button type="primary" :loading="createBusy" @click="doCreate">创建</n-button>
        <n-button @click="createShow = false">取消</n-button>
      </n-space>
    </template>
  </n-modal>

  <!-- 网络详情 -->
  <n-modal v-model:show="detailShow" preset="card" title="网络详情" style="width: 560px">
    <n-descriptions v-if="detail" :column="2" label-placement="left" bordered size="small">
      <n-descriptions-item label="名称">{{ detail.name }}</n-descriptions-item>
      <n-descriptions-item label="驱动">{{ detail.driver }}</n-descriptions-item>
      <n-descriptions-item label="子网">{{ detail.subnet || '-' }}</n-descriptions-item>
      <n-descriptions-item label="网关">{{ detail.gateway || '-' }}</n-descriptions-item>
      <n-descriptions-item label="作用域">{{ detail.scope }}</n-descriptions-item>
      <n-descriptions-item label="隔离外部访问">{{ detail.internal ? '是' : '否' }}</n-descriptions-item>
    </n-descriptions>

    <div style="margin-top: 16px">
      <n-text depth="2" style="font-size: 13px; font-weight: 600">已连接容器（{{ detail?.containers.length || 0 }}）</n-text>
      <n-empty v-if="!detail?.containers.length" description="无容器连接" size="small" style="margin-top: 8px" />
      <div v-else style="margin-top: 8px; display: flex; flex-direction: column; gap: 4px">
        <div
          v-for="c in detail.containers"
          :key="c.name"
          style="display: flex; justify-content: space-between; font-size: 13px"
        >
          <span>{{ c.name }}</span>
          <n-text depth="3" style="font-size: 12px">{{ c.ipv4 }}</n-text>
        </div>
      </div>
    </div>
  </n-modal>

  <!-- 删除确认 -->
  <n-modal
    :show="!!removeTarget"
    preset="dialog"
    type="error"
    title="删除网络"
    positive-text="确认删除"
    negative-text="取消"
    :loading="removeBusy"
    @positive-click="doRemove"
    @negative-click="removeTarget = null"
    @close="removeTarget = null"
  >
    确定删除网络 <b>{{ removeTarget?.name }}</b> 吗？
  </n-modal>
</template>
