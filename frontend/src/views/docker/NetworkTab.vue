<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { statCell } from '../../utils/cell'
import {
  Table, Tag, Button, Space, Modal, Drawer, Input, Select, Switch, Typography,
  Descriptions, Empty, Alert, message,
} from 'ant-design-vue'
import {
  listNetworks, createNetwork, inspectNetwork, removeNetwork,
  type NetworkView, type NetworkDetail,
} from '../../api/docker'

const [messageApi, contextHolder] = message.useMessage()

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
  { title: '名称', dataIndex: 'name', key: 'name', width: 160 },
  {
    title: '驱动',
    dataIndex: 'driver',
    key: 'driver',
    width: 100,
    customRender: ({ record }: { record: NetworkView }) => h(Tag, { size: 'small' }, { default: () => record.driver }),
  },
  {
    title: 'IPv4',
    key: 'subnet',
    width: 170,
    customRender: ({ record }: { record: NetworkView }) =>
      h('div', { style: 'display: flex; flex-direction: column; line-height: 1.3' }, [
        h('span', record.subnet || '-'),
        h('span', { style: 'color: #888; font-size: 12px' }, record.internal ? '隔离外部访问' : (record.gateway || '')),
      ]),
  },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 132, customRender: ({ record }: { record: NetworkView }) => statCell(fmtTime(record.createdAt)) },
  {
    title: '操作',
    key: 'actions',
    width: 120,
    fixed: 'right' as const,
    customRender: ({ record }: { record: NetworkView }) =>
      h(Space, { size: 4 }, {
        default: () => [
          h(Button, { size: 'small', onClick: () => openDetail(record) }, { default: () => '查看' }),
          h(Button, {
            size: 'small',
            danger: true,
            ghost: true,
            // 内置网络(bridge/host/none)由 docker 维护,删除没有意义且易误伤
            disabled: ['bridge', 'host', 'none'].includes(record.name),
            onClick: () => (removeTarget.value = record),
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
    messageApi.warning('请填写网络名')
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
    messageApi.success('网络已创建')
    createShow.value = false
    await load()
  } catch (e: any) {
    messageApi.error('创建失败 — ' + e.message)
  } finally {
    createBusy.value = false
  }
}

async function openDetail(row: NetworkView) {
  try {
    detail.value = await inspectNetwork(row.id)
    detailShow.value = true
  } catch (e: any) {
    messageApi.error('读取详情失败 — ' + e.message)
  }
}

async function doRemove() {
  const row = removeTarget.value
  if (!row) return
  removeBusy.value = true
  try {
    await removeNetwork(row.id)
    messageApi.success('网络已删除')
    removeTarget.value = null
    await load()
  } catch (e: any) {
    messageApi.error('删除失败 — ' + e.message)
  } finally {
    removeBusy.value = false
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <Alert v-if="loadError" type="error" :show-icon="true" :style="{ marginBottom: '12px' }">
    {{ loadError }}
  </Alert>

  <div style="display: flex; justify-content: flex-end; margin-bottom: 12px">
    <Button size="small" type="primary" @click="openCreate">+ 创建网络</Button>
  </div>

  <Table
    :columns="columns"
    :data-source="networks"
    :loading="loading"
    :row-key="(record: NetworkView) => record.id"
    :scroll="{ x: 682 }"
    size="small"
  />

  <!-- 创建网络 -->
  <Drawer
    :open="createShow"
    placement="right"
    :width="440"
    @close="createShow = false"
  >
    <template #title>
      <span class="dw-drawer-title">创建网络</span>
    </template>
      <div style="display: flex; flex-direction: column; gap: 14px">
      <div>
        <div class="dw-label">名称 <span class="dw-required">*</span></div>
        <Input v-model:value="createName" placeholder="my-network" />
      </div>
      <div>
        <div class="dw-label">驱动</div>
        <Select v-model:value="createDriver" :options="driverOptions" />
      </div>
      <div style="display: flex; align-items: center; justify-content: space-between">
        <div>
          <div style="font-size: 14px; color: #000">隔离外部访问</div>
          <div class="dw-desc">内部网络,容器无法访问外网</div>
        </div>
        <Switch v-model:checked="createInternal" />
      </div>
      <div style="display: flex; align-items: center; justify-content: space-between">
        <div>
          <div style="font-size: 14px; color: #000">允许手动附加容器</div>
          <div class="dw-desc">允许 docker network connect 手动接入</div>
        </div>
        <Switch v-model:checked="createAttachable" />
      </div>
      </div>
    <template #footer>
      <div class="dw-footer">
        <Button @click="createShow = false">取消</Button>
        <Button type="primary" :loading="createBusy" @click="doCreate">创建</Button>
      </div>
    </template>
  </Drawer>

  <!-- 网络详情 -->
  <Drawer
    :open="detailShow"
    placement="right"
    :width="560"
    @close="detailShow = false"
  >
    <template #title>
      <span class="dw-drawer-title">网络详情</span>
    </template>
      <div>
        <Descriptions v-if="detail" :column="2" bordered size="small">
      <Descriptions.Item label="名称">{{ detail.name }}</Descriptions.Item>
      <Descriptions.Item label="驱动">{{ detail.driver }}</Descriptions.Item>
      <Descriptions.Item label="子网">{{ detail.subnet || '-' }}</Descriptions.Item>
      <Descriptions.Item label="网关">{{ detail.gateway || '-' }}</Descriptions.Item>
      <Descriptions.Item label="作用域">{{ detail.scope }}</Descriptions.Item>
      <Descriptions.Item label="隔离外部访问">{{ detail.internal ? '是' : '否' }}</Descriptions.Item>
    </Descriptions>

    <div style="margin-top: 16px">
      <div class="dw-section">已连接容器（{{ detail?.containers.length || 0 }}）</div>
      <Empty v-if="!detail?.containers.length" description="无容器连接" :style="{ marginTop: '8px' }" />
      <div v-else style="margin-top: 8px; display: flex; flex-direction: column; gap: 4px">
        <div
          v-for="c in detail.containers"
          :key="c.name"
          style="display: flex; justify-content: space-between; font-size: 13px"
        >
          <span>{{ c.name }}</span>
          <Typography.Text type="secondary" style="font-size: 12px">{{ c.ipv4 }}</Typography.Text>
        </div>
      </div>
    </div>
      </div>
  </Drawer>

  <!-- 删除确认 -->
  <Modal
    :open="!!removeTarget"
    title="删除网络"
    :ok-text="'确认删除'"
    :cancel-text="'取消'"
    :confirm-loading="removeBusy"
    @ok="doRemove"
    @cancel="removeTarget = null"
    @close="removeTarget = null"
  >
    确定删除网络 <b>{{ removeTarget?.name }}</b> 吗？
  </Modal>
</template>
