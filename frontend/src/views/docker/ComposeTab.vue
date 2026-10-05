<script setup lang="ts">
import { ref, onMounted, h, computed } from 'vue'
import { useRouter } from 'vue-router'
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
import { listObjects, type V41Object } from '../../api/objects'
import ComposeEditorModal from '../../components/ComposeEditorModal.vue'
import GroupHeader from '../../app/components/GroupHeader.vue'
import HostBar from '../../app/components/HostBar.vue'
import { countByHost, useHostFilter } from '../../app/composables/useHostFilter'

const router = useRouter()
const [messageApi, contextHolder] = message.useMessage()

// 主机维度（V4.1）：编排本身要选部署到哪台主机，所以操作全在主机离线时禁用。
const { match: hostMatch, isOnline: hostIsOnline, setCounts, addressOf } = useHostFilter()

const projects = ref<ComposeView[]>([])
const shownProjects = computed(() => projects.value.filter((p) => hostMatch(p.host)))
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
    setCounts(countByHost(projects.value))
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

/** 组头的「应用商店」入口：新建编排前先去挑现成模板，省得从空白 compose 起步。 */
function gotoStore() {
  router.push('/store')
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

/**
 * renderHostCell 主机列：在线点 + 主机名 + 地址（原型 hostCell）。
 * 离线时不写「已停止」这类状态——主机不可达时容器状态是未知的，原型也这么要求。
 */
function renderHostCell(name?: string) {
  if (!name) return h('span', { style: 'color:#999' }, '—')
  const on = hostIsOnline(name)
  return h('div', { style: 'display:flex;align-items:center;gap:6px;min-width:0' }, [
    h('span', {
      style: `display:inline-block;width:8px;height:8px;border-radius:50%;flex:0 0 8px;background:${on ? '#52c41a' : '#ff4d4f'}`,
    }),
    h('div', { style: 'display:flex;flex-direction:column;line-height:1.25;min-width:0' }, [
      h('span', { style: 'font-weight:600' }, name),
      h('span', { style: 'color:#8b95a7;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap' },
        on ? addressOf(name) || '—' : '离线 · 状态未知'),
    ]),
  ])
}

/**
 * 「路由」列：这个编排对外暴露了哪些域名。
 *
 * 链路是 route.service → service.backend.deployment → 编排项目名。数据来自
 * 对象册里真实存在的路由/服务，没有就说「—」：把没配的路由画成有，比空着更危险。
 */
const deploymentRoutes = ref<Record<string, string[]>>({})
async function loadDeploymentRoutes() {
  try {
    const [routes, services] = await Promise.all([listObjects('route'), listObjects('service')])
    // 服务 id → 其 backend.deployment（= 编排项目名）
    const byDeployment: Record<string, string[]> = {}
    for (const s of services) {
      const dep = String((s.spec?.backend as Record<string, unknown> | undefined)?.deployment ?? '')
      if (!dep) continue
      ;(byDeployment[dep] ??= []).push(s.id)
    }
    const map: Record<string, string[]> = {}
    for (const r of routes) {
      const svcId = String(r.spec?.service ?? '')
      if (!svcId) continue
      const dep = Object.entries(byDeployment).find(([, ids]) => ids.includes(svcId))?.[0]
      if (!dep) continue
      const sub = String(r.spec?.subdomain ?? '')
      const roots = Array.isArray(r.spec?.roots) ? (r.spec.roots as unknown[]).map(String) : []
      const label = String(r.spec?.name ?? r.key ?? r.id)
      ;(map[dep] ??= []).push(roots.length ? `${sub ? `${sub}.` : ''}${label}（${roots.length} 个域名）` : label)
    }
    deploymentRoutes.value = map
  } catch {
    deploymentRoutes.value = {}
  }
}

function renderRoutes(row: ComposeView) {
  const list = deploymentRoutes.value[row.projectName] ?? []
  if (!list.length) return statCell('—', 'color:#999')
  const shown = list.slice(0, 2).map((t) => h('div', {}, h('span', { style: 'color:#1677ff' }, t)))
  if (list.length > 2) {
    shown.push(h(Tooltip, { title: list.join('\n') }, {
      default: () => h('span', { style: 'color:#1677ff;cursor:pointer' }, `+${list.length - 2}`),
    }))
  }
  return h('div', { style: 'display:flex;flex-direction:column;gap:2px' }, shown)
}

const columns = computed(() => [
  // 应用名 = 人类可读名（displayName），项目名 = 机器标识。原型把两者并排成两列：
  // 排障时看项目名，配路由/仓库时看应用名，合成一列等于逼用户自己猜哪个是哪。
  { title: '应用名', dataIndex: 'displayName', key: 'displayName', width: 150, customRender: ({ record }: { record: ComposeView }) => h('div', { style: 'font-weight: 600' }, record.displayName || record.projectName) },
  { title: '项目名', dataIndex: 'projectName', key: 'projectName', width: 150, customRender: ({ record }: { record: ComposeView }) => h('code', {}, record.projectName) },
  { title: '来源', dataIndex: 'source', key: 'source', width: 100, customRender: ({ record }: { record: ComposeView }) => renderSource(record) },
  { title: '主机', key: 'host', width: 130, customRender: ({ record }: { record: ComposeView }) => renderHostCell(record.host) },
  {
    title: '服务',
    dataIndex: 'count',
    key: 'count',
    width: 70,
    customRender: ({ record }: { record: ComposeView }) => statCell(record.deployed ? `${record.runningCount}/${record.totalCount}` : '-'),
  },
  { title: '状态', dataIndex: 'status', key: 'status', width: 90, customRender: ({ record }: { record: ComposeView }) => renderStatus(record) },
  { title: '路由', key: 'routes', width: 170, customRender: ({ record }: { record: ComposeView }) => renderRoutes(record) },
  { title: '上次部署', dataIndex: 'lastDeployedAt', key: 'lastDeployedAt', width: 132, customRender: ({ record }: { record: ComposeView }) => statCell(fmtTime(record.lastDeployedAt)) },
  {
    title: '操作',
    key: 'actions',
    width: 220,
    fixed: 'right' as const,
    customRender: ({ record }: { record: ComposeView }) => {
      const isBusy = !!busy.value[record.projectName]
      // 主机离线 → 部署/停止/重启全部禁用（agent 不可达，请求只会静默失败）
      const offline = !hostIsOnline(record.host)
      const btns: any[] = []
      // tooltip 图标按钮:危险操作统一红色,其余按语义配色
      const actBtn = (icon: any, label: string, color: string, onClick: () => void) =>
        h(Tooltip, { title: label, trigger: 'hover' }, {
          default: () => h(Button, {
            size: 'small', type: 'text', shape: 'circle', disabled: isBusy, onClick,
            style: { color },
          }, { icon: () => h(icon) }),
        })
      if (offline) {
        // 离线主机只有「查看」可用：读 YAML/配置不碰 daemon。
        if (record.deployed && !record.editable) {
          btns.push(actBtn(EyeOutlined, `主机 ${record.host} 离线，仅可查看配置`, '#8c8c8c', () => openView(record)))
        }
        return h(Space, { size: 0, wrap: false }, {
          default: () => [
            ...btns,
            h(Tooltip, { title: `主机 ${record.host} 离线（agent 不可达），部署类操作已禁用`, trigger: 'hover' }, {
              default: () => h('span', { style: 'color:#ff4d4f;font-size:12px;white-space:nowrap' }, '主机离线'),
            }),
          ],
        })
      }
      // 部署:未部署或托管项目都可一键 up 回盘上 YAML
      if (!record.deployed || record.source === 'managed') {
        btns.push(actBtn(PlayCircleOutlined, '部署', '#52c41a', () => startDeploy(record)))
      }
      if (record.deployed && record.status === 'running') {
        btns.push(actBtn(PauseCircleOutlined, '停止', '#fa8c16', () => confirmDown(record)))
        btns.push(actBtn(ReloadOutlined, '重启', '#13c2c2', () => act(record, restartCompose, '重启')))
      }
      if (record.editable) {
        btns.push(actBtn(EditOutlined, '编辑', '#722ed1', () => openEdit(record)))
      } else if (record.deployed) {
        if (record.source === 'external') {
          btns.push(actBtn(SwapOutlined, '接管', '#fa8c16', () => (adoptTarget.value = record)))
        }
        btns.push(actBtn(EyeOutlined, '查看', '#8c8c8c', () => openView(record)))
      }
      // 托管项目、以及已接管(可编辑)的外部项目都可删除(外部=停止并从 GateBox 移出,不动其文件)。
      if (record.source === 'managed' || record.editable) {
        btns.push(actBtn(DeleteOutlined, '删除', '#ef4444', () => confirmDelete(record)))
      }
      return h(Space, { size: 0, wrap: false }, { default: () => btns })
    },
  },
])

onMounted(() => {
  void load()
  void loadDeploymentRoutes()
})
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

  <div class="compose-hd">
    <GroupHeader
      name="编排项目"
      :tag="`${shownProjects.length} 项`"
    >
      <Button @click="gotoStore">
        应用商店
      </Button>
      <Button :loading="syncing" @click="doSyncCaddy">
        <template #icon><SwapOutlined /></template>
        同步到网关
      </Button>
      <Button
        type="primary"
        class="btn-add"
        @click="openCreate"
      >
        ＋ 新建
      </Button>
    </GroupHeader>
  </div>

  <HostBar resource="compose" />

  <Table
    :columns="columns"
    :data-source="shownProjects"
    :loading="loading"
    :row-key="(record: ComposeView) => record.projectName"
    :scroll="{ x: 920 }"
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
      <template v-if="deleteTarget?.source === 'managed'">
        <Checkbox v-model:checked="deleteData">
          同时删除数据目录 <Typography.Text type="danger" style="font-size: 12px">appData/{{ deleteTarget?.projectName }}</Typography.Text>
        </Checkbox>
        <Checkbox v-model:checked="deleteVolumes">
          同时删除关联的命名卷（<Typography.Text type="danger" style="font-size: 12px">数据不可恢复</Typography.Text>）
        </Checkbox>
      </template>
      <Typography.Text v-else type="secondary" style="font-size: 12px">
        外部项目（已接管）：仅停止容器并从 GateBox 移出，<b>不会删除</b>其原 compose 文件。
      </Typography.Text>
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
