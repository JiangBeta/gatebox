<script setup lang="ts">
import { ref, onMounted, onUnmounted, h, computed, type Component, type VNode } from 'vue'
import {
  Table, Tag, Button, Space, Popover, Alert, Modal, Checkbox,
  Spin, Tooltip, Typography, Input, message, Drawer,
} from 'ant-design-vue'
import {
  FileTextOutlined, CodeOutlined, PlayCircleOutlined, PauseCircleOutlined,
  ReloadOutlined, DeleteOutlined, SwapOutlined, SyncOutlined,
} from '@ant-design/icons-vue'
import {
  listContainers, dockerInfo, startContainer, stopContainer, restartContainer,
  removeContainer, convertPreview, convertContainer, upgradeContainers,
  type ContainerView, type DockerInfo, type PortMapping, type UpgradeResult,
} from '../../api/docker'
import ContainerLogsModal from '../../components/ContainerLogsModal.vue'
import ContainerExecModal from '../../components/ContainerExecModal.vue'

const [messageApi, contextHolder] = message.useMessage()

const containers = ref<ContainerView[]>([])
const info = ref<DockerInfo | null>(null)
const loading = ref(true)
const loadError = ref('')
const busy = ref<Record<string, boolean>>({})
/** 每秒递增,用于让「运行时间」列平滑走动而不必等下一次轮询 */
const tick = ref(0)

const logsTarget = ref<ContainerView | null>(null)
const execTarget = ref<ContainerView | null>(null)

const removeTarget = ref<ContainerView | null>(null)
const removeVolumes = ref(false)

// 游离容器转编排(docs §4.3)
const convertTarget = ref<ContainerView | null>(null)
const convertYaml = ref('')
const convertProject = ref('')
const convertLoading = ref(false)
const convertBusy = ref(false)

// 容器升级:allTarget 为 true 表示「全部升级」,否则是单行升级
const upgradeAll = ref(false)
const upgradeSingle = ref<ContainerView | null>(null)
const upgrading = ref(false)
const upgradeResults = ref<UpgradeResult[]>([])

let pollTimer: number | undefined
let tickTimer: number | undefined

function fmtBytes(n: number): string {
  if (!n) return '-'
  const mib = n / 1048576
  if (mib >= 1024) return (mib / 1024).toFixed(2) + ' GiB'
  return mib.toFixed(1) + ' MiB'
}

function fmtUptime(startedAt?: string): string {
  if (!startedAt) return '-'
  void tick.value // 建立依赖,使其每秒重算
  const ms = Date.now() - new Date(startedAt).getTime()
  if (ms < 0) return '-'
  const sec = Math.floor(ms / 1000)
  const d = Math.floor(sec / 86400)
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  if (d > 0) return `${d} 天 ${h} 小时`
  if (h > 0) return `${h} 小时 ${m} 分`
  if (m > 0) return `${m} 分 ${sec % 60} 秒`
  return `${sec} 秒`
}

// 运行时间(Hover 精确到秒,随 tick 每秒跳动)
function fmtUptimeSec(startedAt?: string): string {
  if (!startedAt) return '-'
  void tick.value
  const ms = Date.now() - new Date(startedAt).getTime()
  if (ms < 0) return '-'
  const sec = Math.floor(ms / 1000)
  const d = Math.floor(sec / 86400)
  const s = sec % 60
  const h = Math.floor((sec % 86400) / 3600)
  const m = Math.floor((sec % 3600) / 60)
  return `${d} 天 ${h} 小时 ${m} 分 ${s} 秒`
}

function pad2(n: number): string { return String(n).padStart(2, '0') }

// 创建时间:主按钮显示日期,Hover 显示时刻
function fmtDate(s?: string): string {
  if (!s) return '-'
  const d = new Date(s)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`
}
function fmtClock(s?: string): string {
  if (!s) return '-'
  const d = new Date(s)
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

function fmtGiB(n?: number): string { return n == null ? '-' : n.toFixed(2) + ' GiB' }

// --- 状态按钮(Outlined):透明底 + 彩色描边,颜色随状态变化 ---
function outlinedBtn(label: string, fg: string): VNode {
  return h(Button, {
    size: 'small',
    style: { color: fg, border: `1px solid ${fg}`, background: 'transparent', boxShadow: 'none', fontWeight: 600, fontSize: '13px' },
  }, { default: () => label })
}

// --- 数据按钮(Filled):无边框、浅蓝底、蓝字,带鼠标触动(hover)面板 ---
// muted=true 时(非运行/采集中)降为浅灰底灰字。
function filledBtn(label: string, muted = false): VNode {
  const s = muted
    ? { color: '#999', background: '#f5f5f5' }
    : { color: '#1677ff', background: '#e6f4ff' }
  return h(Button, {
    size: 'small',
    style: { ...s, border: 'none', boxShadow: 'none', fontWeight: 600, padding: '0 10px', fontSize: '13px' },
  }, { default: () => label })
}

// hover 面板:蓝 filled 按钮 + 悬停内容(鼠标触动呈现)。复杂内容用 Popover(content 槽)
function hoverPanel(trigger: VNode, content: VNode, placement = 'right'): VNode {
  return h(Popover, { placement, trigger: 'hover', mouseEnterDelay: 0.1, mouseLeaveDelay: 0.05 }, {
    default: () => trigger,
    content: () => content,
  })
}

// 横向柱状图:外层灰=未使用,内层绿=使用比例(占 widthPct 的容器宽度)
function pctBar(pct: number, widthPct: number): VNode {
  return h('div', {
    style: `position:relative;height:8px;width:${widthPct}%;max-width:${widthPct}%;border-radius:4px;background:#d9d9d9;overflow:hidden;flex:0 0 auto`,
  }, [
    h('div', {
      style: `position:absolute;left:0;top:0;bottom:0;width:${Math.min(100, Math.max(0, pct))}%;background:#52c41a;border-radius:4px`,
    }),
  ])
}

async function load(showSpinner = false) {
  if (showSpinner) loading.value = true
  try {
    containers.value = await listContainers()
    loadError.value = ''
  } catch (e: any) {
    loadError.value = e.message || '加载失败'
  } finally {
    loading.value = false
  }
}

async function act(row: ContainerView, fn: (id: string) => Promise<void>, label: string) {
  busy.value = { ...busy.value, [row.id]: true }
  try {
    await fn(row.id)
    messageApi.success(`${row.name}:${label}成功`)
    await load()
  } catch (e: any) {
    messageApi.error(`${row.name}:${label}失败 — ${e.message}`)
  } finally {
    const next = { ...busy.value }
    delete next[row.id]
    busy.value = next
  }
}

function confirmRemove(row: ContainerView) {
  removeTarget.value = row
  removeVolumes.value = false
}

async function doRemove() {
  const row = removeTarget.value
  if (!row) return
  removeTarget.value = null
  await act(row, (id) => removeContainer(id, removeVolumes.value), '删除')
}

async function openConvert(row: ContainerView) {
  convertTarget.value = row
  convertLoading.value = true
  convertYaml.value = ''
  convertProject.value = ''
  try {
    const res = await convertPreview(row.id)
    convertYaml.value = res.yaml
    convertProject.value = res.projectName
  } catch (e: any) {
    messageApi.error('反推 compose 失败 — ' + e.message)
    convertTarget.value = null
  } finally {
    convertLoading.value = false
  }
}

async function doConvert() {
  const row = convertTarget.value
  if (!row) return
  if (!convertProject.value.trim()) {
    messageApi.warning('请填写 projectName')
    return
  }
  convertBusy.value = true
  try {
    await convertContainer(row.id, convertProject.value.trim())
    messageApi.success('已转为编排,请到「编排」Tab 部署')
    convertTarget.value = null
    await load()
  } catch (e: any) {
    messageApi.error('转换失败 — ' + e.message)
  } finally {
    convertBusy.value = false
  }
}

function confirmUpgradeAll() {
  upgradeAll.value = true
  upgradeResults.value = []
}

function confirmUpgradeOne(row: ContainerView) {
  upgradeSingle.value = row
  upgradeResults.value = []
}

async function doUpgrade() {
  const ids = upgradeSingle.value ? [upgradeSingle.value.id] : []
  upgrading.value = true
  upgradeResults.value = []
  try {
    upgradeResults.value = await upgradeContainers(ids)
    messageApi.success('升级操作完成')
    await load()
  } catch (e: any) {
    messageApi.error('升级失败 — ' + e.message)
    // 出错保留弹窗展示提示
  } finally {
    upgrading.value = false
  }
}

function closeUpgrade() {
  if (upgrading.value) return
  upgradeAll.value = false
  upgradeSingle.value = null
  upgradeResults.value = []
}

function upgradeStatusText(status: string): string {
  switch (status) {
    case 'upgraded': return '已升级'
    case 'up-to-date': return '已是最新'
    case 'skipped': return '已跳过'
    case 'error': return '失败'
    default: return status
  }
}

function renderPorts(row: ContainerView) {
  if (!row.ports.length) return h('div', {}, '-')
  const trigger = filledBtn(`端口：${row.ports.length}`)
  // hover 面板 4 列:IP:端口(右) / 箭头(中) / 容器端口(右) / 协议(左)
  const content = h('div', { style: 'padding: 2px 4px; min-width: 320px' }, [
    h('div', {
      style: 'display:flex; gap:10px; padding:3px 8px; margin-bottom:2px; color:#666; font-size:13px; border-bottom:1px solid #f0f0f0',
    }, [
      h('span', { style: 'width:130px; text-align:right' }, 'IP:端口'),
      h('span', { style: 'width:18px; text-align:center' }, ''),
      h('span', { style: 'width:70px; text-align:right' }, '容器端口'),
      h('span', { style: 'width:64px' }, '协议'),
    ]),
    ...row.ports.map((p) => h('div', {
      style: 'display:flex; gap:10px; padding:3px 8px; align-items:center',
    }, [
      h('span', { style: 'width:130px; text-align:right; font-family:monospace; font-size:13px' },
        p.host ? `${p.ip || '0.0.0.0'}:${p.host}` : '未映射'),
      h('span', { style: 'width:18px; text-align:center; color:#999' }, '→'),
      h('span', { style: 'width:70px; text-align:right; font-family:monospace; font-size:13px' }, `${p.container}`),
      h('span', { style: 'width:64px; color:#888; font-size:13px' }, p.protocol),
    ])),
  ])
  return hoverPanel(trigger, content)
}

// CPU 列:蓝色 filled 按钮 + hover 面板(整体 5 列 + 每核两组 3 列)
function renderCpu(record: ContainerView) {
  if (record.state !== 'running') return filledBtn('-', true)
  if (!record.hasStats) return filledBtn('采集中', true)
  const u = record.cpuPercent
  const trigger = filledBtn(`${u.toFixed(2)} %`)
  const hst = record.host
  const cores = hst?.corePercents || []
  const freq = hst?.cpuFreqGHz
  // 第一行 5 列:CPU / 整体使用率柱状图(60%宽) / % 居右绿 / CPU 频率 / 频率值右蓝。
  // 整行不换行:white-space:nowrap + 面板最小宽度足够,保证 5 项单行排列
  const row1 = h('div', {
    style: 'display:flex; align-items:center; gap:6px; width:100%; flex-wrap:nowrap; white-space:nowrap',
  }, [
    h('span', { style: 'width:28px; color:#555; font-size:12px' }, 'CPU'),
    pctBar(u, 60),
    h('span', { style: 'width:58px; text-align:right; color:#52c41a; font-weight:600; font-size:12px' }, `${u.toFixed(2)} %`),
    h('span', { style: 'width:60px; text-align:right; color:#555; font-size:12px' }, 'CPU 频率'),
    h('span', { style: 'width:70px; text-align:right; color:#1677ff; font-weight:600; font-size:12px' },
      freq ? `${freq.toFixed(2)} GHz` : '-'),
  ])
  // 第二行:核心均分两列;每列内每核 3 列(核心名 / 柱状图60% / 值)
  const coreGroup = (arr: number[], base: number): VNode =>
    h('div', { style: 'flex:1; display:flex; flex-direction:column; gap:5px; min-width:0' }, [
      ...arr.map((p, i) => h('div', { style: 'display:flex; align-items:center; gap:6px' }, [
        h('span', { style: 'width:26px; color:#888; font-family:monospace; font-size:12px' }, `C${base + i}`),
        pctBar(p, 60),
        h('span', { style: 'width:46px; text-align:right; color:#52c41a; font-size:12px' }, `${p.toFixed(1)} %`),
      ])),
    ])
  let row2: VNode | null = null
  if (cores.length) {
    const half = Math.ceil(cores.length / 2)
    row2 = h('div', { style: 'display:flex; gap:16px; border-top:1px solid #f0f0f0; margin-top:8px; padding-top:7px' }, [
      coreGroup(cores.slice(0, half), 0),
      coreGroup(cores.slice(half), half),
    ])
  }
  // min-width 保证第一行 5 项单行放下:0.6W(柱状图) + 固定栏(≈240px) = W → W≈600
  const content = h('div', { style: 'min-width: 600px' }, [row1, row2].filter(Boolean))
  return hoverPanel(trigger, content)
}

// 内存列:蓝色 filled 按钮 + hover 面板(项目/数值两列,宿主机 meminfo 数据)
function renderMemory(record: ContainerView) {
  if (record.state !== 'running') return filledBtn('-', true)
  if (!record.hasStats) return filledBtn('采集中', true)
  const trigger = filledBtn(fmtBytes(record.memoryUsage))
  const hst = record.host
  const items: [string, string][] = [
    ['内存总量(Total)', fmtGiB(hst?.memTotalGiB)],
    ['使用量(Used)', fmtGiB(hst?.memUsedGiB)],
    ['可用量(Avail)', fmtGiB(hst?.memAvailGiB)],
    ['缓存(Cache)', fmtGiB(hst?.memCacheGiB)],
  ]
  const content = h('div', { style: 'min-width: 210px' }, items.map(([k, v]) => h('div', {
    style: 'display:flex; justify-content:space-between; gap:20px; padding:3px 6px; white-space:nowrap',
  }, [
    h('span', { style: 'color:#666; font-size:13px' }, k),
    h('span', { style: 'color:#1677ff; font-family:monospace; font-weight:600; font-size:13px' }, v),
  ])))
  return hoverPanel(trigger, content)
}

// 创建时间:按钮示 yyyy-MM-dd,hover 示 HH:mm:ss
function renderCreatedAt(record: ContainerView) {
  const trigger = filledBtn(fmtDate(record.createdAt))
  const content = h('div', { style: 'padding:4px 8px; font-family:monospace; font-size:13px' }, fmtClock(record.createdAt))
  return hoverPanel(trigger, content)
}

// 运行时间:按钮示粗粒度, hover 精确到秒并随 tick 每秒跳动
function renderUptime(record: ContainerView) {
  const trigger = filledBtn(fmtUptime(record.startedAt))
  const content = h('div', { style: 'padding:4px 8px; font-family:monospace; font-size:13px; min-width:150px' },
    fmtUptimeSec(record.startedAt))
  return hoverPanel(trigger, content)
}

/**
 * 操作列的图标按钮:hover 出文字提示,替代原先的文字按钮以节省横向空间。
 * trigger 外面套 span 是必要的——disabled 的按钮自身不派发鼠标事件,
 * 而「为什么这个按钮是灰的」恰恰是最需要提示的场景。
 */
function iconBtn(opts: {
  icon: Component
  tip: string
  onClick?: () => void
  disabled?: boolean
  loading?: boolean
  type?: 'default' | 'primary' | 'danger'
  /** 自定义图标/文字颜色(如 stop/restart 用红、start 用绿)。禁用态自动淡化 */
  color?: string
}) {
  const color = opts.disabled ? undefined : opts.color
  return h(Tooltip, { title: opts.tip, trigger: 'hover' }, {
    default: () =>
      h('span', { style: 'display: inline-flex' }, [
        h(Button, {
          size: 'small',
          type: 'text',
          shape: 'circle',
          danger: opts.type === 'danger',
          disabled: opts.disabled,
          loading: opts.loading,
          onClick: opts.onClick,
        }, { icon: () => h(opts.icon, { style: color ? { color } : {} }) }),
      ]),
  })
}

const columns = computed(() => [
  {
    title: '名称',
    dataIndex: 'name',
    key: 'name',
    // 固定宽度:不随列拉伸变化,下方镜像名保持可读
    width: 300,
    fixed: 'left' as const,
    customRender: ({ record }: { record: ContainerView }) =>
      // 名称 + 仅镜像名(居左与名称对齐);健康标签已移入独立「健康」列。
      // 单元格根/子级一律 div/span 纯元素(避开 rc-table 数字键样式 bug)。
      h('div', { style: 'display:flex;flex-direction:column;gap:2px;min-width:0' }, [
        h('span', { style: 'font-weight:600;overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, record.name),
        h('span', { style: 'color:#888;font-size:12px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, record.image),
      ]),
  },
  {
    title: '健康',
    key: 'health',
    width: 92,
    customRender: ({ record }: { record: ContainerView }) => {
      if (!record.health) return outlinedBtn('未检测', '#999')
      const color = record.health === 'healthy' ? '#52c41a'
        : record.health === 'unhealthy' ? '#ff4d4f'
          : record.health === 'starting' ? '#faad14' : '#1677ff'
      return outlinedBtn(record.health, color)
    },
  },
  {
    title: '状态',
    dataIndex: 'state',
    key: 'state',
    width: 92,
    customRender: ({ record }: { record: ContainerView }) => {
      const map: Record<string, [string, string]> = {
        running: ['运行中', '#52c41a'],
        exited: ['已停止', '#999'],
        paused: ['已暂停', '#faad14'],
        restarting: ['重启中', '#1677ff'],
        created: ['已创建', '#1677ff'],
        dead: ['异常', '#ff4d4f'],
      }
      const [label, color] = map[record.state] || [record.state, '#1677ff']
      return outlinedBtn(label, color)
    },
  },
  { title: '端口', key: 'ports', width: 110, customRender: ({ record }: { record: ContainerView }) => renderPorts(record) },
  {
    title: 'CPU',
    dataIndex: 'cpu',
    key: 'cpu',
    width: 100,
    customRender: ({ record }: { record: ContainerView }) => renderCpu(record),
  },
  {
    title: '内存',
    dataIndex: 'memory',
    key: 'memory',
    width: 130,
    customRender: ({ record }: { record: ContainerView }) => renderMemory(record),
  },
  {
    title: '创建时间',
    dataIndex: 'createdAt',
    key: 'createdAt',
    width: 118,
    customRender: ({ record }: { record: ContainerView }) => renderCreatedAt(record),
  },
  { title: '运行时间', dataIndex: 'uptime', key: 'uptime', width: 122, customRender: ({ record }: { record: ContainerView }) => renderUptime(record) },
  {
    title: '操作',
    key: 'actions',
    width: 208,
    fixed: 'right' as const,
    customRender: ({ record }: { record: ContainerView }) => {
      const isBusy = !!busy.value[record.id]
      const running = record.state === 'running'
      // 槽位数量恒定(启停共用一个槽),避免行与行之间图标错位
      return h(Space, { size: 0, wrap: false, align: 'center' }, {
        default: () => [
          iconBtn({
            icon: FileTextOutlined,
            tip: '日志',
            onClick: () => (logsTarget.value = record),
          }),
          iconBtn({
            icon: CodeOutlined,
            tip: running ? '控制台' : '容器未运行,无法进入控制台',
            disabled: !running,
            onClick: () => (execTarget.value = record),
          }),
          running
            ? iconBtn({
                icon: PauseCircleOutlined,
                tip: '停止',
                color: '#ff4d4f',
                loading: isBusy,
                onClick: () => act(record, stopContainer, '停止'),
              })
            : iconBtn({
                icon: PlayCircleOutlined,
                tip: '启动',
                color: '#52c41a',
                loading: isBusy,
                onClick: () => act(record, startContainer, '启动'),
              }),
          iconBtn({
            icon: ReloadOutlined,
            tip: running ? '重启' : '容器未运行,请直接启动',
            disabled: !running,
            color: '#ff4d4f',
            loading: isBusy && running,
            onClick: () => act(record, restartContainer, '重启'),
          }),
          iconBtn({
            icon: DeleteOutlined,
            // 只有已停止的容器可删除(docs §4.5):强删会跳过优雅停止,可能损坏数据
            tip: running ? '运行中的容器不可删除,请先停止' : '删除',
            type: 'danger',
            disabled: running,
            loading: isBusy && !running,
            onClick: () => confirmRemove(record),
          }),
          // 游离容器可转为编排(docs §4.3)
          ...(record.source === 'loose'
            ? [iconBtn({
                icon: SwapOutlined,
                tip: '转为编排',
                onClick: () => openConvert(record),
              })]
            : []),
          iconBtn({
            icon: SyncOutlined,
            // 升级 = 拉新镜像 + 重建容器(会停止并重建,存在风险)
            tip: '升级(比对镜像 tag,有新版则重建)',
            color: '#1677ff',
            loading: isBusy,
            onClick: () => confirmUpgradeOne(record),
          }),
        ],
      })
    },
  },
])

onMounted(async () => {
  await load(true)
  try {
    info.value = await dockerInfo()
  } catch {
    /* info 失败不影响列表 */
  }
  // 轮询快照:采集器在后端收敛连接,前端只需定期取(docs §5.2)
  pollTimer = window.setInterval(() => load(), 3000)
  tickTimer = window.setInterval(() => tick.value++, 1000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
  if (tickTimer) clearInterval(tickTimer)
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
    <template v-if="loadError.includes('Network') || loadError.includes('请求失败')">
      —— 请确认 Docker 守护进程可用，且 GateBox 有权访问 /var/run/docker.sock
    </template>
  </Alert>

  <div class="header-row">
    <div v-if="info" class="summary">
      <span>Docker {{ info.serverVersion }}</span>
      <span>容器 {{ info.containersRunning }}/{{ info.containers }} 运行中</span>
      <span>镜像 {{ info.images }}</span>
      <span>日志驱动 {{ info.loggingDriver }}</span>
      <span>{{ info.imagePlatform }} · {{ info.ncpu }} 核</span>
      <Tag v-if="!info.logsReadable" color="warning">
        当前日志驱动不支持在线查看日志
      </Tag>
    </div>
    <div class="header-actions">
      <Button
        type="primary"
        ghost
        :disabled="!containers.length"
        @click="confirmUpgradeAll"
      >
        <template #icon><SyncOutlined /></template>
        全部升级
      </Button>
    </div>
  </div>

  <!-- scroll-x = 各列宽度之和。窄屏时横向滚动,而不是把「名称」压到换行 -->
  <Table
    :columns="columns"
    :data-source="containers"
    :loading="loading"
    :row-key="(record: ContainerView) => record.id"
    :scroll="{ x: 1310 }"
    size="small"
  />

  <ContainerLogsModal
    v-if="logsTarget"
    :container="logsTarget"
    @close="logsTarget = null"
  />
  <ContainerExecModal
    v-if="execTarget"
    :container="execTarget"
    @close="execTarget = null"
  />

  <Modal
    :open="!!removeTarget"
    title="删除容器"
    :ok-text="'确认删除'"
    :cancel-text="'取消'"
    @ok="doRemove"
    @cancel="removeTarget = null"
    @close="removeTarget = null"
  >
    <div style="display: flex; flex-direction: column; gap: 10px">
      <span>
        确定删除容器 <b>{{ removeTarget?.name }}</b> 吗？此操作不可撤销。
      </span>
      <Checkbox v-model:checked="removeVolumes">
        同时删除该容器的匿名卷
      </Checkbox>
      <Typography.Text type="secondary" style="font-size: 12px">
        匿名卷可能保存着应用数据。不确定时请保持不勾选——孤儿卷可稍后在「存储卷」中清理。
      </Typography.Text>
    </div>
  </Modal>

  <!-- 游离容器转编排 -->
  <Drawer
    :open="!!convertTarget"
    :width="720"
    placement="right"
    :mask-closable="!convertBusy"
    @close="convertTarget = null"
  >
    <template #title>
      <span class="dw-drawer-title">转为编排</span>
    </template>
    <Spin :spinning="convertLoading">
      <div style="display: flex; flex-direction: column; gap: 12px">
        <Alert type="warning" :show-icon="true">
          转换将删除原容器并重建为编排项目，存在数据风险。请核对下方反推的配置。
        </Alert>
        <div>
          <div class="dw-label">projectName <span class="dw-required">*</span></div>
          <Input v-model:value="convertProject" placeholder="my-app" />
        </div>
        <div>
          <div class="dw-label">反推的 docker-compose.yaml</div>
          <pre style="font-size: 12px; background: #f7f7f7; padding: 10px; border-radius: 4px; overflow: auto; max-height: 300px">{{ convertYaml }}</pre>
        </div>
      </div>
    </Spin>
    <template #footer>
      <div class="dw-footer">
        <Button @click="convertTarget = null" :disabled="convertBusy">取消</Button>
        <Button type="primary" :loading="convertBusy" :disabled="convertLoading" @click="doConvert">确认转换</Button>
      </div>
    </template>
  </Drawer>

  <!-- 容器升级确认/结果 -->
  <Modal
    :open="!!upgradeAll || !!upgradeSingle"
    :title="upgradeAll ? '全部升级' : '升级容器'"
    :ok-text="upgradeResults.length ? '关闭' : '确认升级'"
    :cancel-text="upgradeResults.length ? undefined : '取消'"
    :ok-button-props="{ disabled: upgrading }"
    :confirm-loading="upgrading"
    @ok="upgradeResults.length ? closeUpgrade() : doUpgrade()"
    @cancel="closeUpgrade"
    @close="closeUpgrade"
    :style="{ maxWidth: '640px' }"
  >
    <div style="display: flex; flex-direction: column; gap: 10px">
      <Alert v-if="!upgradeResults.length" type="warning" :show-icon="true" :style="{ marginBottom: '4px' }">
        {{ upgradeAll ? '将比对全部容器的镜像 tag,只升级有新版本的容器。' : `将比对容器 ${upgradeSingle?.name || ''} 的镜像 tag,有新版本则重建。` }}
        升级需停止并重建容器,存在风险,请确认。
      </Alert>

      <Spin :spinning="upgrading">
        <div v-if="upgradeResults.length" style="display: flex; flex-direction: column; gap: 6px">
          <div v-for="r in upgradeResults" :key="r.id"
               style="display: flex; align-items: center; justify-content: space-between; gap: 8px; padding: 4px 0; border-bottom: 1px solid #f0f0f0">
            <span style="font-weight: 600; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ r.name }}</span>
            <Tag
              :color="r.status === 'upgraded' ? 'success' : r.status === 'error' ? 'error' : r.status === 'skipped' ? 'warning' : 'default'"
            >
              {{ upgradeStatusText(r.status) }}
            </Tag>
          </div>
          <Alert v-if="upgradeResults.some(r => r.status === 'error')" type="error" :show-icon="true" :style="{ fontSize: '12px' }">
            {{ upgradeResults.filter(r => r.message).map(r => `${r.name}: ${r.message}`).join('；') }}
          </Alert>
        </div>
      </Spin>
    </div>
  </Modal>
</template>

<style scoped>
/* 汇总行 + 「全部升级」同一行:信息居左,按钮居右 */
.header-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
  margin-bottom: 12px;
}
.summary {
  display: flex;
  gap: 18px;
  align-items: center;
  flex-wrap: wrap;
  font-size: 13px;
  color: #666;
}
.header-actions {
  flex-shrink: 0;
}
</style>
