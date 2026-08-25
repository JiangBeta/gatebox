<script setup lang="ts">
import { ref, onMounted, h, computed } from 'vue'
import {
  NDataTable, NTag, NButton, NSpace, NModal, NInput, NProgress, NText,
  NTooltip, NEllipsis, NUpload, NUploadDragger, useMessage,
} from 'naive-ui'
import {
  listImages, removeImage, loadImage, dockerInfo, wsURL,
  type ImageView, type PullProgress,
} from '../../api/docker'
import RegistryModal from '../../components/RegistryModal.vue'

const message = useMessage()

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
      h(NEllipsis, {
        style: dangling ? 'font-weight: 400; color: #aaa' : 'font-weight: 600',
      }, { default: () => name }),
      dangling ? h(NTag, { size: 'tiny', bordered: false }, { default: () => '悬空' }) : null,
    ]),
    row.names.length > 1
      ? h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => `+${row.names.length - 1} 个标签` })
      : h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => row.digests[0]?.split('@')[1]?.slice(0, 12) || '' }),
  ])
}

function renderUsage(row: ImageView) {
  if (!row.inUse) return h(NTag, { size: 'small', bordered: false }, { default: () => '未使用' })
  return h(NTooltip, { trigger: 'hover' }, {
    trigger: () => h(NTag, { size: 'small', type: 'warning', bordered: false }, { default: () => '使用中' }),
    default: () => '被容器使用: ' + (row.containers || []).join(', '),
  })
}

const columns = computed(() => [
  { title: '名称', key: 'name', minWidth: 240, render: (row: ImageView) => renderName(row) },
  { title: '架构', key: 'arch', width: 120, render: (row: ImageView) => row.arch || '-' },
  { title: '大小', key: 'size', width: 90, render: (row: ImageView) => fmtBytes(row.size) },
  { title: '是否使用', key: 'usage', width: 100, render: (row: ImageView) => renderUsage(row) },
  { title: '创建时间', key: 'createdAt', width: 132, render: (row: ImageView) => fmtTime(row.createdAt) },
  {
    title: '操作',
    key: 'actions',
    width: 90,
    fixed: 'right' as const,
    render: (row: ImageView) =>
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
])

async function doRemove() {
  const row = removeTarget.value
  if (!row) return
  removeBusy.value = true
  try {
    await removeImage(row.names[0] || row.id)
    message.success('镜像已删除')
    removeTarget.value = null
    await load()
  } catch (e: any) {
    message.error('删除失败 — ' + e.message)
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
    message.warning('请填写镜像名')
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
      message.error('拉取失败 — ' + p.error)
      closePull()
      return
    }
    pullPercent.value = p.percent
    pullStatus.value = p.status
    pullByteWeighted.value = p.byteWeighted
    if (p.done) {
      message.success('镜像拉取完成')
      closePull()
      load()
    }
  }
  pullSocket.onerror = () => {
    message.error('拉取连接失败')
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

// NUpload 的 custom-request:接管默认上传逻辑,每个文件调一次 loadImage。
function customRequest(opts: {
  file: { file?: File | null }
  onFinish: () => void
  onError: () => void
}) {
  const f = opts.file.file
  if (!f) {
    opts.onError()
    return
  }
  loadImage(f)
    .then(() => {
      opts.onFinish()
      load()
    })
    .catch(() => opts.onError())
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
  <n-alert v-if="loadError" type="error" :show-icon="true" style="margin-bottom: 12px">
    {{ loadError }}
  </n-alert>

  <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px">
    <div style="font-size: 13px; color: #666">
      共 {{ images.length }} 个镜像 · 合计 {{ fmtBytes(totalSize) }}
    </div>
    <n-space>
      <n-button size="small" @click="importShow = true">导入镜像</n-button>
      <n-button size="small" @click="registryShow = true">仓库管理</n-button>
      <n-button size="small" type="primary" @click="openPull">拉取镜像</n-button>
    </n-space>
  </div>

  <n-data-table
    :columns="columns"
    :data="images"
    :loading="loading"
    :row-key="(row: ImageView) => row.id"
    :scroll-x="792"
    size="small"
  />

  <!-- 拉取镜像 -->
  <n-modal
    v-model:show="pullShow"
    preset="card"
    title="拉取镜像"
    style="width: 480px"
    :mask-closable="!pullActive"
    @after-leave="closePull"
  >
    <div style="display: flex; flex-direction: column; gap: 14px">
      <div>
        <n-text depth="3" style="font-size: 12px">镜像名</n-text>
        <n-input v-model:value="pullName" placeholder="nginx 或 registry.example.com/foo" :disabled="pullActive" />
      </div>
      <div style="display: flex; gap: 12px">
        <div style="flex: 1">
          <n-text depth="3" style="font-size: 12px">版本（留空为 latest）</n-text>
          <n-input v-model:value="pullTag" placeholder="latest" :disabled="pullActive" />
        </div>
        <div style="flex: 1">
          <n-text depth="3" style="font-size: 12px">架构</n-text>
          <n-input v-model:value="pullArch" placeholder="linux/amd64" :disabled="pullActive" />
        </div>
      </div>

      <div v-if="pullActive || pullPercent > 0" style="display: flex; flex-direction: column; gap: 6px">
        <n-progress
          type="line"
          :percentage="Math.round(pullPercent)"
          :indicator-placement="'inside'"
        />
        <n-text depth="3" style="font-size: 12px">
          {{ pullStatus || '完成' }}
          <template v-if="!pullByteWeighted && !pullStatus">（按层数估算）</template>
        </n-text>
      </div>
    </div>

    <template #footer>
      <n-space justify="end">
        <n-button v-if="pullActive" type="warning" @click="closePull">终止</n-button>
        <n-button v-else type="primary" @click="startPull">拉取</n-button>
        <n-button :disabled="pullActive" @click="pullShow = false">关闭</n-button>
      </n-space>
    </template>
  </n-modal>

  <!-- 导入镜像(多文件拖拽上传) -->
  <n-modal
    v-model:show="importShow"
    preset="card"
    title="导入镜像"
    style="width: 560px"
  >
    <n-upload
      multiple
      :max="10"
      accept=".tar,.tar.gz,.tgz"
      :custom-request="customRequest"
      @finish="load"
    >
      <n-upload-dragger>
        <div style="padding: 24px 0">
          <div style="font-size: 15px; margin-bottom: 8px">点击或拖拽 .tar 归档到此处</div>
          <n-text depth="3" style="font-size: 12px">最多 10 个,支持 docker save 导出的 tar 包</n-text>
        </div>
      </n-upload-dragger>
    </n-upload>
  </n-modal>

  <!-- 仓库管理 -->
  <RegistryModal v-model:show="registryShow" />

  <!-- 删除确认 -->
  <n-modal
    :show="!!removeTarget"
    preset="dialog"
    type="error"
    title="删除镜像"
    positive-text="确认删除"
    negative-text="取消"
    :loading="removeBusy"
    @positive-click="doRemove"
    @negative-click="removeTarget = null"
    @close="removeTarget = null"
  >
    确定删除镜像 <b>{{ removeTarget?.names[0] || removeTarget?.id }}</b> 吗？此操作不可撤销。
  </n-modal>
</template>
