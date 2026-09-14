<script setup lang="ts">
import { ref, onMounted, h, computed } from 'vue'
import { statCell } from '../../utils/cell'
import {
  Table, Tag, Button, Space, Modal, Drawer, Input, Progress, Tooltip,
  Typography, Upload, Alert, message,
} from 'ant-design-vue'
import {
  listImages, removeImage, loadImage, dockerInfo, wsURL,
  type ImageView, type PullProgress,
} from '../../api/docker'
import RegistryModal from '../../components/RegistryModal.vue'

const [messageApi, contextHolder] = message.useMessage()

const images = ref<ImageView[]>([])
const loading = ref(true)
const loadError = ref('')
const infoArch = ref('')

const removeTarget = ref<ImageView | null>(null)
const removeBusy = ref(false)

// 拉取弹层状态
const pullShow = ref(false)
const pullName = ref('')
const pullTag = ref('')
const pullArch = ref('')
const pullActive = ref(false)
const pullPercent = ref(0)
const pullStatus = ref('')
const pullByteWeighted = ref(true)
let pullSocket: WebSocket | null = null

// 导入弹层(多文件拖拽上传)
const importShow = ref(false)
// 仓库管理弹层
const registryShow = ref(false)

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

const totalSize = computed(() => images.value.reduce((a, i) => a + i.size, 0))

async function load() {
  loading.value = true
  try {
    images.value = await listImages()
    loadError.value = ''
  } catch (e: any) {
    loadError.value = e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

function renderName(row: ImageView) {
  const name = row.names[0] || '<无标签>'
  const dangling = row.names.length === 0
  return h('div', { style: 'display: flex; flex-direction: column; gap: 2px; min-width: 0' }, [
    h('div', { style: 'display: flex; align-items: center; gap: 6px; min-width: 0' }, [
      h('span', { style: (dangling ? 'font-weight: 400; color: #aaa;' : 'font-weight: 600;') + 'overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, name),
      // 硬编码样式 span 代替 Tag:rc-table 单元格内避免组件型子级(见 utils/cell.ts)
      ...(dangling ? [h('span', { style: 'border:1px solid #d9d9d9;color:#999;padding:0 6px;border-radius:4px;font-size:12px' }, '悬空')] : []),
    ]),
    h('span', { style: 'color: #888; font-size: 12px' },
      row.names.length > 1 ? `+${row.names.length - 1} 个标签` : (row.digests[0]?.split('@')[1]?.slice(0, 12) || '')),
  ])
}

function renderUsage(row: ImageView) {
  if (!row.inUse) return h(Tag, { bordered: false }, { default: () => '未使用' })
  return h(Tooltip, { title: '被容器使用: ' + (row.containers || []).join(', ') }, {
    default: () => h(Tag, { color: 'warning', bordered: false }, { default: () => '使用中' }),
  })
}

const columns = computed(() => [
  { title: '名称', dataIndex: 'name', key: 'name', width: 240, customRender: ({ record }: { record: ImageView }) => renderName(record) },
  { title: '架构', dataIndex: 'arch', key: 'arch', width: 120, customRender: ({ record }: { record: ImageView }) => statCell(record.arch || '-') },
  { title: '大小', dataIndex: 'size', key: 'size', width: 90, customRender: ({ record }: { record: ImageView }) => statCell(fmtBytes(record.size)) },
  { title: '是否使用', key: 'usage', width: 100, customRender: ({ record }: { record: ImageView }) => renderUsage(record) },
  { title: '创建时间', dataIndex: 'createdAt', key: 'createdAt', width: 132, customRender: ({ record }: { record: ImageView }) => statCell(fmtTime(record.createdAt)) },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right' as const,
    customRender: ({ record }: { record: ImageView }) =>
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
])

async function doRemove() {
  const row = removeTarget.value
  if (!row) return
  removeBusy.value = true
  try {
    await removeImage(row.names[0] || row.id)
    messageApi.success('镜像已删除')
    removeTarget.value = null
    await load()
  } catch (e: any) {
    messageApi.error('删除失败 — ' + e.message)
  } finally {
    removeBusy.value = false
  }
}

// --- 拉取 ---

function openPull() {
  pullName.value = ''
  pullTag.value = ''
  pullArch.value = infoArch.value
  pullPercent.value = 0
  pullStatus.value = ''
  pullByteWeighted.value = true
  pullActive.value = false
  pullShow.value = true
}

function pullRef(): string {
  const name = pullName.value.trim()
  if (!name) return ''
  const tag = pullTag.value.trim()
  return tag ? `${name}:${tag}` : name
}

function startPull() {
  const ref = pullRef()
  if (!ref) {
    messageApi.warning('请填写镜像名')
    return
  }
  if (pullActive.value) return
  pullActive.value = true
  pullPercent.value = 0
  pullStatus.value = '正在连接…'

  pullSocket = new WebSocket(wsURL('/docker/images/pull', {
    ref,
    platform: pullArch.value.trim(),
  }))
  pullSocket.onmessage = (ev) => {
    const p: PullProgress = JSON.parse(ev.data)
    if (p.error) {
      messageApi.error('拉取失败 — ' + p.error)
      closePull()
      return
    }
    pullPercent.value = p.percent
    pullStatus.value = p.status
    pullByteWeighted.value = p.byteWeighted
    if (p.done) {
      messageApi.success('镜像拉取完成')
      closePull()
      load()
    }
  }
  pullSocket.onerror = () => {
    messageApi.error('拉取连接失败')
    closePull()
  }
  pullSocket.onclose = () => {
    if (pullActive.value) {
      pullActive.value = false
    }
  }
}

function closePull() {
  pullActive.value = false
  if (pullSocket) {
    pullSocket.onmessage = null
    pullSocket.onclose = null
    pullSocket.close()
    pullSocket = null
  }
}

// --- 导入(多文件拖拽上传) ---

// Upload 的 custom-request:接管默认上传逻辑,每个文件调一次 loadImage。
function customRequest(opts: {
  file: File
  onSuccess?: () => void
  onError?: () => void
}) {
  const f = opts.file
  if (!f) {
    opts.onError?.()
    return
  }
  loadImage(f)
    .then(() => {
      opts.onSuccess?.()
      load()
    })
    .catch(() => opts.onError?.())
}

onMounted(async () => {
  await load()
  try {
    infoArch.value = (await dockerInfo()).imagePlatform
  } catch {
    /* 架构默认值失败不影响列表 */
  }
})
</script>

<template>
  <contextHolder />
  <Alert v-if="loadError" type="error" :show-icon="true" :style="{ marginBottom: '12px' }">
    {{ loadError }}
  </Alert>

  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px">
    <div style="font-size: 13px; color: #666">
      共 {{ images.length }} 个镜像 · 合计 {{ fmtBytes(totalSize) }}
    </div>
    <Space>
      <Button size="small" @click="importShow = true">导入镜像</Button>
      <Button size="small" @click="registryShow = true">仓库管理</Button>
      <Button size="small" type="primary" @click="openPull">拉取镜像</Button>
    </Space>
  </div>

  <Table
    :columns="columns"
    :data-source="images"
    :loading="loading"
    :row-key="(record: ImageView) => record.id"
    :scroll="{ x: 792 }"
    size="small"
  />

  <!-- 拉取镜像 -->
  <Drawer
    :open="pullShow"
    placement="right"
    :width="480"
    :mask-closable="!pullActive"
    @after-visible-change="(visible: boolean) => { if (!visible) closePull() }"
    @close="pullShow = false"
  >
    <template #title>
      <span class="dw-drawer-title">拉取镜像</span>
    </template>
    <div style="display: flex; flex-direction: column; gap: 14px">
      <div>
        <div class="dw-label">镜像名 <span class="dw-required">*</span></div>
        <Input v-model:value="pullName" placeholder="nginx 或 registry.example.com/foo" :disabled="pullActive" />
      </div>
      <div style="display: flex; gap: 12px">
        <div style="flex: 1">
          <div class="dw-label">版本（留空为 latest）</div>
          <Input v-model:value="pullTag" placeholder="latest" :disabled="pullActive" />
        </div>
        <div style="flex: 1">
          <div class="dw-label">架构</div>
          <Input v-model:value="pullArch" placeholder="linux/amd64" :disabled="pullActive" />
        </div>
      </div>

      <div v-if="pullActive || pullPercent > 0" style="display: flex; flex-direction: column; gap: 6px">
        <Progress
          :percent="Math.round(pullPercent)"
          :stroke-color="'#1677ff'"
        />
        <Typography.Text type="secondary" style="font-size: 12px">
          {{ pullStatus || '完成' }}
          <template v-if="!pullByteWeighted && !pullStatus">（按层数估算）</template>
        </Typography.Text>
      </div>
    </div>
    <template #footer>
      <div class="dw-footer">
        <Button :disabled="pullActive" @click="pullShow = false">关闭</Button>
        <Button v-if="pullActive" @click="closePull">终止</Button>
        <Button v-else type="primary" @click="startPull">拉取</Button>
      </div>
    </template>
  </Drawer>

  <!-- 导入镜像(多文件拖拽上传) -->
  <Drawer
    :open="importShow"
    placement="right"
    :width="560"
    @close="importShow = false"
  >
    <template #title>
      <span class="dw-drawer-title">导入镜像</span>
    </template>
    <Upload
      multiple
      :max-count="10"
      accept=".tar,.tar.gz,.tgz"
      :custom-request="customRequest"
      @change="load"
    >
      <div style="padding: 24px 0; text-align: center">
        <div style="font-size: 15px; margin-bottom: 8px">点击或拖拽 .tar 归档到此处</div>
        <Typography.Text type="secondary" style="font-size: 12px">最多 10 个,支持 docker save 导出的 tar 包</Typography.Text>
      </div>
    </Upload>
  </Drawer>

  <!-- 仓库管理 -->
  <RegistryModal v-model:show="registryShow" />

  <!-- 删除确认 -->
  <Modal
    :open="!!removeTarget"
    title="删除镜像"
    :ok-text="'确认删除'"
    :cancel-text="'取消'"
    :confirm-loading="removeBusy"
    @ok="doRemove"
    @cancel="removeTarget = null"
    @close="removeTarget = null"
  >
    确定删除镜像 <b>{{ removeTarget?.names[0] || removeTarget?.id }}</b> 吗？此操作不可撤销。
  </Modal>
</template>
