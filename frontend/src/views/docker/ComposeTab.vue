<script setup lang="ts">
import { ref, onMounted, h, computed } from 'vue'
import {
  NDataTable, NTag, NButton, NSpace, NModal, NDrawer, NAlert, NText, NCheckbox, NProgress, useMessage,
} from 'naive-ui'
import {
  listCompose, downCompose, restartCompose, restoreCompose, deleteCompose, adoptCompose, wsURL,
  type ComposeView, type DeployProgress,
} from '../../api/docker'
import ComposeEditorModal from '../../components/ComposeEditorModal.vue'

const message = useMessage()

const projects = ref<ComposeView[]>([])
const loading = ref(true)
const loadError = ref('')

const editorShow = ref(false)
const editorProject = ref<string | null>(null)
const editorReadOnly = ref(false)

const downTarget = ref<ComposeView | null>(null)
const adoptTarget = ref<ComposeView | null>(null)
const deleteTarget = ref<ComposeView | null>(null)
const deleteData = ref(false)
const deleteVolumes = ref(false)
const busy = ref<Record<string, boolean>>({})

// 部署进度弹层
const deployShow = ref(false)
const deployTarget = ref<ComposeView | null>(null)
const deployLines = ref<DeployProgress[]>([])
const deployError = ref('')
const deploying = ref(false)
const deployDone = ref(false)
let deploySocket: WebSocket | null = null

function fmtTime(s?: string): string {
  if (!s) return '-'
  const d = new Date(s)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

async function load() {
  loading.value = true
  try {
    projects.value = await listCompose()
    loadError.value = ''
  } catch (e: any) {
    loadError.value = e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

function openCreate() {
  editorProject.value = null
  editorReadOnly.value = false
  editorShow.value = true
}

function openEdit(row: ComposeView) {
  editorProject.value = row.projectName
  editorReadOnly.value = false
  editorShow.value = true
}

function openView(row: ComposeView) {
  editorProject.value = row.projectName
  editorReadOnly.value = true
  editorShow.value = true
}

async function act(row: ComposeView, fn: (p: string) => Promise<void>, label: string) {
  busy.value = { ...busy.value, [row.projectName]: true }
  try {
    await fn(row.projectName)
    message.success(`${row.displayName}:${label}成功`)
    await load()
  } catch (e: any) {
    message.error(`${row.displayName}:${label}失败 — ${e.message}`)
  } finally {
    const next = { ...busy.value }
    delete next[row.projectName]
    busy.value = next
  }
}

function confirmDown(row: ComposeView) {
  downTarget.value = row
}

async function doDown() {
  const row = downTarget.value
  if (!row) return
  downTarget.value = null
  await act(row, downCompose, '停止')
}

async function doAdopt() {
  const row = adoptTarget.value
  if (!row) return
  adoptTarget.value = null
  await act(row, adoptCompose, '接管')
}

function confirmDelete(row: ComposeView) {
  deleteTarget.value = row
  deleteData.value = false
  deleteVolumes.value = false
}

async function doDelete() {
  const row = deleteTarget.value
  if (!row) return
  deleteTarget.value = null
  busy.value = { ...busy.value, [row.projectName]: true }
  try {
    await deleteCompose(row.projectName, { removeData: deleteData.value, removeVolumes: deleteVolumes.value })
    message.success(`${row.displayName}:已删除`)
    await load()
  } catch (e: any) {
    message.error(`${row.displayName}:删除失败 — ${e.message}`)
  } finally {
    const next = { ...busy.value }
    delete next[row.projectName]
    busy.value = next
  }
}

function startDeploy(row: ComposeView) {
  deployTarget.value = row
  deployLines.value = []
  deployError.value = ''
  deploying.value = true
  deployDone.value = false
  deployShow.value = true

  deploySocket = new WebSocket(wsURL(`/docker/compose/${row.projectName}/deploy`))
  deploySocket.onmessage = (ev) => {
    const p: DeployProgress = JSON.parse(ev.data)
    if (p.error) {
      deployError.value = p.error
      deploying.value = false
      return
    }
    deployLines.value.push(p)
    if (p.done) {
      deployDone.value = true
      deploying.value = false
      message.success(`${row.displayName}:部署完成`)
      load()
    }
  }
  deploySocket.onerror = () => {
    deployError.value = '部署连接失败'
    deploying.value = false
  }
}

function closeDeploy() {
  if (deploySocket) {
    deploySocket.onmessage = null
    deploySocket.close()
    deploySocket = null
  }
  deploying.value = false
  deployShow.value = false
}

function renderSource(row: ComposeView) {
  const tags = []
  if (row.source === 'managed') {
    tags.push(h(NTag, { size: 'tiny', type: 'success', bordered: false }, { default: () => '托管' }))
  } else {
    tags.push(h(NTag, { size: 'tiny', type: 'info', bordered: false }, { default: () => '外部' }))
  }
  if (!row.deployed) {
    tags.push(h(NTag, { size: 'tiny', bordered: false, style: 'margin-left: 4px' }, { default: () => '未部署' }))
  }
  return h('span', { style: 'display: inline-flex; align-items: center; gap: 4px' }, tags)
}

function renderStatus(row: ComposeView) {
  if (!row.deployed) return h(NText, { depth: 3 }, { default: () => '未部署' })
  if (row.status === 'running') return h(NTag, { size: 'small', type: 'success', bordered: false }, { default: () => '运行中' })
  return h(NTag, { size: 'small', bordered: false }, { default: () => row.status || '已停止' })
}

const columns = computed(() => [
  { title: '应用名', key: 'displayName', minWidth: 160, render: (row: ComposeView) => h('span', { style: 'font-weight: 600' }, row.displayName) },
  { title: 'projectName', key: 'projectName', minWidth: 140, render: (row: ComposeView) => h(NText, { depth: 3 }, { default: () => row.projectName }) },
  { title: '来源', key: 'source', width: 110, render: (row: ComposeView) => renderSource(row) },
  {
    title: '服务',
    key: 'count',
    width: 70,
    render: (row: ComposeView) => (row.deployed ? `${row.runningCount}/${row.totalCount}` : '-'),
  },
  { title: '状态', key: 'status', width: 90, render: (row: ComposeView) => renderStatus(row) },
  { title: '上次部署', key: 'lastDeployedAt', width: 132, render: (row: ComposeView) => fmtTime(row.lastDeployedAt) },
  {
    title: '操作',
    key: 'actions',
    width: 260,
    fixed: 'right' as const,
    render: (row: ComposeView) => {
      const isBusy = !!busy.value[row.projectName]
      const btns: any[] = []
      // 部署:未部署或托管项目都可一键 up 回盘上 YAML
      if (!row.deployed || row.source === 'managed') {
        btns.push(h(NButton, { size: 'tiny', type: 'primary', ghost: true, loading: isBusy, onClick: () => startDeploy(row) }, { default: () => '部署' }))
      }
      if (row.deployed && row.status === 'running') {
        btns.push(h(NButton, { size: 'tiny', loading: isBusy, onClick: () => confirmDown(row) }, { default: () => '停止' }))
        btns.push(h(NButton, { size: 'tiny', loading: isBusy, onClick: () => act(row, restartCompose, '重启') }, { default: () => '重启' }))
      }
      if (row.editable) {
        btns.push(h(NButton, { size: 'tiny', onClick: () => openEdit(row) }, { default: () => '编辑' }))
      } else if (row.deployed) {
        if (row.source === 'external') {
          btns.push(h(NButton, { size: 'tiny', type: 'warning', ghost: true, onClick: () => (adoptTarget.value = row) }, { default: () => '接管' }))
        }
        btns.push(h(NButton, { size: 'tiny', onClick: () => openView(row) }, { default: () => '查看' }))
      }
      if (row.source === 'managed') {
        btns.push(h(NButton, { size: 'tiny', type: 'error', ghost: true, onClick: () => confirmDelete(row) }, { default: () => '删除' }))
      }
      return h(NSpace, { size: 4, wrap: false }, { default: () => btns })
    },
  },
])

onMounted(load)
</script>

<template>
  <n-alert v-if="loadError" type="error" :show-icon="true" style="margin-bottom: 12px">
    {{ loadError }}
  </n-alert>

  <div style="display: flex; justify-content: flex-end; margin-bottom: 12px">
    <n-button size="small" type="primary" @click="openCreate">+ 创建应用</n-button>
  </div>

  <n-data-table
    :columns="columns"
    :data="projects"
    :loading="loading"
    :row-key="(row: ComposeView) => row.projectName"
    :scroll-x="962"
    size="small"
  />

  <ComposeEditorModal
    v-model:show="editorShow"
    :project="editorProject"
    :read-only="editorReadOnly"
    @saved="load"
  />

  <!-- 停止确认 -->
  <n-modal
    :show="!!downTarget"
    preset="dialog"
    type="warning"
    title="停止项目"
    positive-text="确认停止"
    negative-text="取消"
    @positive-click="doDown"
    @negative-click="downTarget = null"
    @close="downTarget = null"
  >
    确定停止 <b>{{ downTarget?.displayName }}</b> 吗？将删除该项目的所有容器（数据卷保留）。
  </n-modal>

  <!-- 接管确认 -->
  <n-modal
    :show="!!adoptTarget"
    preset="dialog"
    type="warning"
    title="接管外部项目"
    positive-text="确认接管"
    negative-text="取消"
    @positive-click="doAdopt"
    @negative-click="adoptTarget = null"
    @close="adoptTarget = null"
  >
    <div style="display: flex; flex-direction: column; gap: 8px">
      <span>接管 <b>{{ adoptTarget?.displayName }}</b> 后即可在 GateBox 中编辑其 compose 文件。</span>
      <n-text depth="3" style="font-size: 12px">
        注意：保存时将重写该文件，原始注释与格式会丢失。若该文件在 git 仓库中，建议先提交。
      </n-text>
    </div>
  </n-modal>

  <!-- 删除确认 -->
  <n-modal
    :show="!!deleteTarget"
    preset="dialog"
    type="error"
    title="删除项目"
    positive-text="确认删除"
    negative-text="取消"
    @positive-click="doDelete"
    @negative-click="deleteTarget = null"
    @close="deleteTarget = null"
  >
    <div style="display: flex; flex-direction: column; gap: 10px">
      <span>确定删除 <b>{{ deleteTarget?.displayName }}</b> 吗？将停止并删除其所有容器。</span>
      <n-checkbox v-model:checked="deleteData">
        同时删除数据目录 <n-text type="error" style="font-size: 12px">appData/{{ deleteTarget?.projectName }}</n-text>
      </n-checkbox>
      <n-checkbox v-model:checked="deleteVolumes">
        同时删除关联的命名卷（<n-text type="error" style="font-size: 12px">数据不可恢复</n-text>）
      </n-checkbox>
    </div>
  </n-modal>

  <!-- 部署进度 -->
  <n-drawer
    v-model:show="deployShow"
    placement="right"
    width="min(600px, 100vw)"
    :mask-closable="!deploying"
    @after-leave="closeDeploy"
  >
    <template #header>
      <span style="font-size: 15px; font-weight: 600">部署 {{ deployTarget?.displayName || '' }}</span>
    </template>
    <div style="display: flex; flex-direction: column; gap: 10px; max-height: 360px; overflow: auto">
      <n-progress
        v-if="deploying"
        type="line"
        :percentage="100"
        :processing="true"
        :indicator-placement="'inside'"
      />
      <div v-for="(l, i) in deployLines" :key="i" style="font-size: 12px; font-family: monospace; line-height: 1.6">
        <n-tag size="tiny" :type="l.status === 'Done' ? 'success' : l.status === 'Error' ? 'error' : 'default'" :bordered="false" style="margin-right: 6px">
          {{ l.status }}
        </n-tag>
        <span>{{ l.id }}</span>
        <span style="color: #888; margin-left: 6px">{{ l.text }}</span>
      </div>
      <n-text v-if="deployError" type="error" style="font-size: 12px">{{ deployError }}</n-text>
      <n-text v-if="deployDone" type="success" style="font-size: 13px">✓ 部署完成</n-text>
    </div>
    <template #footer>
      <n-button size="small" :disabled="deploying" @click="closeDeploy">关闭</n-button>
    </template>
  </n-drawer>
</template>
