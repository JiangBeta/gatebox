<script setup lang="ts">
import { ref, onMounted, h, computed } from 'vue'
import { statCell } from '../../utils/cell'
import {
  Table, Tag, Button, Space, Modal, Drawer, Alert, Checkbox, Progress, Tooltip, message, Typography,
} from 'ant-design-vue'
import {
  PlayCircleOutlined, PauseCircleOutlined, ReloadOutlined, EditOutlined, EyeOutlined,
  SwapOutlined, DeleteOutlined,
} from '@ant-design/icons-vue'
import {
  listCompose, downCompose, restartCompose, restoreCompose, deleteCompose, adoptCompose, syncCaddy, wsURL,
  type ComposeView, type DeployProgress,
} from '../../api/docker'
import ComposeEditorModal from '../../components/ComposeEditorModal.vue'

const [messageApi, contextHolder] = message.useMessage()

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

// 手动「同步到网关」(ADR-026 §7):把运行中容器 label 派生进 Caddyfile 并 POST /load。
const syncing = ref(false)
async function doSyncCaddy() {
  if (syncing.value) return
  syncing.value = true
  try {
    await syncCaddy()
    messageApi.success('已同步到网关')
  } catch (e: any) {
    messageApi.error('同步失败 — ' + e.message)
  } finally {
    syncing.value = false
  }
}
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
    messageApi.success(`${row.displayName}:${label}成功`)
    await load()
  } catch (e: any) {
    messageApi.error(`${row.displayName}:${label}失败 — ${e.message}`)
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
    messageApi.success(`${row.displayName}:已删除`)
    await load()
  } catch (e: any) {
    messageApi.error(`${row.displayName}:删除失败 — ${e.message}`)
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
      messageApi.success(`${row.displayName}:部署完成`)
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
    tags.push(h(Tag, { color: 'success' }, { default: () => '托管' }))
  } else {
    tags.push(h(Tag, { color: 'processing' }, { default: () => '外部' }))
  }
  if (!row.deployed) {
    tags.push(h(Tag, { style: 'margin-left: 4px' }, { default: () => '未部署' }))
  }
  return h('div', { style: 'display: inline-flex; align-items: center; gap: 4px' }, tags)
}

function renderStatus(row: ComposeView) {
  if (!row.deployed) return h('div', { style: 'color: #888' }, '未部署')
  if (row.status === 'running') return h(Tag, { color: 'success' }, { default: () => '运行中' })
  return h(Tag, {}, { default: () => row.status || '已停止' })
}

const columns = computed(() => [
  { title: '项目名称', dataIndex: 'projectName', key: 'projectName', width: 160, customRender: ({ record }: { record: ComposeView }) => h('div', { style: 'font-weight: 600' }, record.projectName || record.displayName) },
  { title: '来源', dataIndex: 'source', key: 'source', width: 110, customRender: ({ record }: { record: ComposeView }) => renderSource(record) },
  {
    title: '服务',
    dataIndex: 'count',
    key: 'count',
    width: 70,
    customRender: ({ record }: { record: ComposeView }) => statCell(record.deployed ? `${record.runningCount}/${record.totalCount}` : '-'),
  },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90, customRender: ({ record }: { record: ComposeView }) => renderStatus(record) },
  { title: '上次部署', dataIndex: 'lastDeployedAt', key: 'lastDeployedAt', width: 132, customRender: ({ record }: { record: ComposeView }) => statCell(fmtTime(record.lastDeployedAt)) },
  {
    title: '操作',
    key: 'actions',
    width: 220,
    fixed: 'right' as const,
    customRender: ({ record }: { record: ComposeView }) => {
      const isBusy = !!busy.value[record.projectName]
      const btns: any[] = []
      // tooltip 图标按钮:危险操作统一红色,其余按语义配色
      const actBtn = (icon: any, label: string, color: string, onClick: () => void) =>
        h(Tooltip, { title: label, trigger: 'hover' }, {
          default: () => h(Button, {
            size: 'small', type: 'text', shape: 'circle', disabled: isBusy, onClick,
            style: { color },
          }, { icon: () => h(icon) }),
        })
      // 部署:未部署或托管项目都可一键 up 回盘上 YAML
      if (!record.deployed || record.source === 'managed') {
        btns.push(actBtn(PlayCircleOutlined, '部署', '#52c41a', () => startDeploy(record)))
      }
      if (record.deployed && record.status === 'running') {
        btns.push(actBtn(PauseCircleOutlined, '停止', '#ff4d4f', () => confirmDown(record)))
        btns.push(actBtn(ReloadOutlined, '重启', '#ff4d4f', () => act(record, restartCompose, '重启')))
      }
      if (record.editable) {
        btns.push(actBtn(EditOutlined, '编辑', '#1677ff', () => openEdit(record)))
      } else if (record.deployed) {
        if (record.source === 'external') {
          btns.push(actBtn(SwapOutlined, '接管', '#faad14', () => (adoptTarget.value = record)))
        }
        btns.push(actBtn(EyeOutlined, '查看', '#8c8c8c', () => openView(record)))
      }
      if (record.source === 'managed') {
        btns.push(actBtn(DeleteOutlined, '删除', '#ff4d4f', () => confirmDelete(record)))
      }
      return h(Space, { size: 0, wrap: false }, { default: () => btns })
    },
  },
])

onMounted(load)
</script>

<template>
  <contextHolder />
  <Alert
    v-if="loadError"
    type="error"
    :show-icon="true"
    :style="{ marginBottom: '12px' }"
  >
    {{ loadError }}
  </Alert>

  <div style="display: flex; justify-content: flex-end; gap: 8px; margin-bottom: 12px">
    <Button :loading="syncing" @click="doSyncCaddy">
      <template #icon><SwapOutlined /></template>
      同步到网关
    </Button>
    <Button type="primary" @click="openCreate">+ 创建应用</Button>
  </div>

  <Table
    :columns="columns"
    :data-source="projects"
    :loading="loading"
    :row-key="(record: ComposeView) => record.projectName"
    :scroll="{ x: 790 }"
    size="small"
  />

  <ComposeEditorModal
    v-model:show="editorShow"
    :project="editorProject"
    :read-only="editorReadOnly"
    @saved="load"
  />

  <!-- 停止确认 -->
  <Modal
    :open="!!downTarget"
    title="停止项目"
    :ok-text="'确认停止'"
    :cancel-text="'取消'"
    @ok="doDown"
    @cancel="downTarget = null"
    @close="downTarget = null"
  >
    确定停止 <b>{{ downTarget?.displayName }}</b> 吗？将删除该项目的所有容器（数据卷保留）。
  </Modal>

  <!-- 接管确认 -->
  <Modal
    :open="!!adoptTarget"
    title="接管外部项目"
    :ok-text="'确认接管'"
    :cancel-text="'取消'"
    @ok="doAdopt"
    @cancel="adoptTarget = null"
    @close="adoptTarget = null"
  >
    <div style="display: flex; flex-direction: column; gap: 8px">
      <span>接管 <b>{{ adoptTarget?.displayName }}</b> 后即可在 GateBox 中编辑其 compose 文件。</span>
      <Typography.Text type="secondary" style="font-size: 12px">
        注意：保存时将重写该文件，原始注释与格式会丢失。若该文件在 git 仓库中，建议先提交。
      </Typography.Text>
    </div>
  </Modal>

  <!-- 删除确认 -->
  <Modal
    :open="!!deleteTarget"
    title="删除项目"
    :ok-text="'确认删除'"
    :cancel-text="'取消'"
    @ok="doDelete"
    @cancel="deleteTarget = null"
    @close="deleteTarget = null"
  >
    <div style="display: flex; flex-direction: column; gap: 10px">
      <span>确定删除 <b>{{ deleteTarget?.displayName }}</b> 吗？将停止并删除其所有容器。</span>
      <Checkbox v-model:checked="deleteData">
        同时删除数据目录 <Typography.Text type="danger" style="font-size: 12px">appData/{{ deleteTarget?.projectName }}</Typography.Text>
      </Checkbox>
      <Checkbox v-model:checked="deleteVolumes">
        同时删除关联的命名卷（<Typography.Text type="danger" style="font-size: 12px">数据不可恢复</Typography.Text>）
      </Checkbox>
    </div>
  </Modal>

  <!-- 部署进度 -->
  <Drawer
    :open="deployShow"
    :width="600"
    placement="right"
    :mask-closable="!deploying"
    @close="closeDeploy"
    @after-visible-change="(visible: boolean) => !visible && closeDeploy()"
  >
    <template #title>
      <span class="dw-drawer-title">部署 {{ deployTarget?.displayName || '' }}</span>
    </template>
    <div style="display: flex; flex-direction: column; gap: 10px">
      <Progress
        v-if="deploying"
        :percent="100"
        :status="'active'"
      />
      <div v-for="(l, i) in deployLines" :key="i" style="font-size: 12px; font-family: monospace; line-height: 1.6">
        <Tag
          :color="l.status === 'Done' ? 'success' : l.status === 'Error' ? 'error' : 'default'"
          style="margin-right: 6px"
        >
          {{ l.status }}
        </Tag>
        <span>{{ l.id }}</span>
        <span style="color: #8c8c8c; margin-left: 6px">{{ l.text }}</span>
      </div>
      <Typography.Text v-if="deployError" type="danger" style="font-size: 12px">{{ deployError }}</Typography.Text>
      <Typography.Text v-if="deployDone" type="success" style="font-size: 13px">✓ 部署完成</Typography.Text>
    </div>
    <template #footer>
      <div class="dw-footer">
        <Button :disabled="deploying" @click="closeDeploy">关闭</Button>
      </div>
    </template>
  </Drawer>
</template>
