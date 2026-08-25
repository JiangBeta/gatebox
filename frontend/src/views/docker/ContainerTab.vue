<script setup lang="ts">
import { ref, onMounted, onUnmounted, h, computed, type Component } from 'vue'
import {
  NDataTable, NTag, NButton, NSpace, NPopover, NAlert, NModal, NCheckbox,
  NText, NSpin, NIcon, NTooltip, NEllipsis, useMessage,
} from 'naive-ui'
import {
  DocumentTextOutline, TerminalOutline, PlayOutline, StopOutline,
  RefreshOutline, TrashOutline,
} from '@vicons/ionicons5'
import {
  listContainers, dockerInfo, startContainer, stopContainer, restartContainer,
  removeContainer, type ContainerView, type DockerInfo, type PortMapping,
} from '../../api/docker'
import ContainerLogsModal from '../../components/ContainerLogsModal.vue'
import ContainerExecModal from '../../components/ContainerExecModal.vue'

const message = useMessage()

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

let pollTimer: number | undefined
let tickTimer: number | undefined

const sourceMeta: Record<string, { type: 'success' | 'info' | 'default'; label: string; tip: string }> = {
  managed: { type: 'success', label: '托管', tip: 'GateBox 创建并管理的编排容器' },
  external: { type: 'info', label: '外部编排', tip: '由外部 compose 项目创建,GateBox 只读接入' },
  loose: { type: 'default', label: '游离', tip: 'docker run 起的容器,不属于任何编排' },
}

function fmtBytes(n: number): string {
  if (!n) return '-'
  const mib = n / 1048576
  if (mib >= 1024) return (mib / 1024).toFixed(2) + ' GiB'
  return mib.toFixed(1) + ' MiB'
}

function fmtTime(s?: string): string {
  if (!s) return '-'
  const d = new Date(s)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
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
    message.success(`${row.name}:${label}成功`)
    await load()
  } catch (e: any) {
    message.error(`${row.name}:${label}失败 — ${e.message}`)
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

function renderPorts(row: ContainerView) {
  if (!row.ports.length) return h(NText, { depth: 3 }, { default: () => '-' })
  return h(
    NPopover,
    { trigger: 'hover', placement: 'right' },
    {
      // 蓝色 + 虚线下划线:一眼看出可 hover(仅移动触发,无需点击)
      trigger: () => h(NText, {
        style: 'cursor: default; color: #4098fc; border-bottom: 1px dashed #4098fc',
      }, { default: () => `端口：${row.ports.length}` }),
      default: () =>
        h('div', { style: 'display: flex; flex-direction: column; gap: 6px' },
          row.ports.map((p: PortMapping) =>
            // 每一条映射一块底,一眼区分「一条映射」。
            // 用 popover 的 divider 色做底,深浅主题下都与 popover 背景有明显差异
            h('div', {
              style: 'display: flex; align-items: center; gap: 6px; white-space: nowrap;' +
                ' padding: 3px 8px; border-radius: 4px;' +
                ' background: var(--n-divider-color); border: 1px solid var(--n-divider-color)',
            }, [
              p.host
                ? h(NTag, { size: 'small', type: p.ip === '0.0.0.0' || p.ip === '::' ? 'warning' : 'success', bordered: false },
                    { default: () => `${p.ip || '0.0.0.0'}:${p.host}` })
                : h(NTag, { size: 'small', type: 'default', bordered: false }, { default: () => '未映射' }),
              h('span', '→'),
              h(NTag, { size: 'small', bordered: false }, { default: () => `${p.container}/${p.protocol}` }),
            ]),
          ),
        ),
    },
  )
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
  type?: 'default' | 'primary' | 'error'
  /** 自定义图标/文字颜色(如 stop/restart 用红、start 用绿)。禁用态自动淡化 */
  color?: string
}) {
  const color = opts.disabled ? undefined : opts.color
  return h(NTooltip, { trigger: 'hover', delay: 300 }, {
    trigger: () =>
      h('span', { style: 'display: inline-flex' }, [
        h(NButton, {
          size: 'small',
          quaternary: true,
          circle: true,
          type: opts.type,
          disabled: opts.disabled,
          loading: opts.loading,
          onClick: opts.onClick,
        }, { icon: () => h(NIcon, { color }, { default: () => h(opts.icon) }) }),
      ]),
    default: () => opts.tip,
  })
}

const columns = computed(() => [
  {
    title: '名称',
    key: 'name',
    minWidth: 220,
    render: (row: ContainerView) => {
      const meta = sourceMeta[row.source] || sourceMeta.loose
      return h('div', { style: 'display: flex; flex-direction: column; gap: 2px; min-width: 0' }, [
        h('div', { style: 'display: flex; align-items: center; gap: 6px; min-width: 0' }, [
          // min-width:0 + flex:1 是让 ellipsis 在 flex 容器里真正生效的前提
          h(NEllipsis, { style: 'font-weight: 600; flex: 1; min-width: 0' }, { default: () => row.name }),
          h(NPopover, { trigger: 'hover' }, {
            trigger: () => h(NTag, { size: 'tiny', type: meta.type, bordered: false, style: 'flex-shrink: 0' }, { default: () => meta.label }),
            default: () => meta.tip,
          }),
          row.health
            ? h(NTag, { size: 'tiny', type: row.health === 'healthy' ? 'success' : 'warning', bordered: false, style: 'flex-shrink: 0' },
                { default: () => row.health })
            : null,
          // 隐形占位:吸走名称列的剩余宽度,让标签贴着名称而不是被推到列右缘
          h('span', { style: 'flex: 1' }),
        ]),
        h(NEllipsis, { depth: 3, style: 'font-size: 12px' }, { default: () => row.image }),
      ])
    },
  },
  {
    title: '状态',
    key: 'state',
    width: 90,
    render: (row: ContainerView) =>
      h(NTag, { size: 'small', type: row.state === 'running' ? 'success' : 'default', bordered: false },
        { default: () => (row.state === 'running' ? '运行中' : row.state === 'exited' ? '已停止' : row.state) }),
  },
  { title: '端口', key: 'ports', width: 88, render: renderPorts },
  {
    title: 'CPU',
    key: 'cpu',
    width: 88,
    render: (row: ContainerView) => {
      if (row.state !== 'running') return h(NText, { depth: 3 }, { default: () => '-' })
      if (!row.hasStats) return h(NSpin, { size: 12 })
      return h('span', `${row.cpuPercent.toFixed(2)} %`)
    },
  },
  {
    title: '内存',
    key: 'memory',
    width: 138,
    render: (row: ContainerView) => {
      if (row.state !== 'running') return h(NText, { depth: 3 }, { default: () => '-' })
      if (!row.hasStats) return h(NSpin, { size: 12 })
      return h('div', { style: 'display: flex; flex-direction: column; line-height: 1.3' }, [
        h('span', fmtBytes(row.memoryUsage)),
        h(NText, { depth: 3, style: 'font-size: 12px' },
          { default: () => `${row.memoryPercent.toFixed(2)}% / ${fmtBytes(row.memoryLimit)}` }),
      ])
    },
  },
  { title: '创建时间', key: 'createdAt', width: 132, render: (row: ContainerView) => fmtTime(row.createdAt) },
  { title: '运行时间', key: 'uptime', width: 116, render: (row: ContainerView) => fmtUptime(row.startedAt) },
  {
    title: '操作',
    key: 'actions',
    width: 152,
    // 固定在右侧:横向滚动时操作按钮始终可达
    fixed: 'right' as const,
    render: (row: ContainerView) => {
      const isBusy = !!busy.value[row.id]
      const running = row.state === 'running'
      // 槽位数量恒定(启停共用一个槽),避免行与行之间图标错位
      return h(NSpace, { size: 0, wrap: false, align: 'center' }, {
        default: () => [
          iconBtn({
            icon: DocumentTextOutline,
            tip: '日志',
            onClick: () => (logsTarget.value = row),
          }),
          iconBtn({
            icon: TerminalOutline,
            tip: running ? '控制台' : '容器未运行,无法进入控制台',
            disabled: !running,
            onClick: () => (execTarget.value = row),
          }),
          running
            ? iconBtn({
                icon: StopOutline,
                tip: '停止',
                color: '#e88080',
                loading: isBusy,
                onClick: () => act(row, stopContainer, '停止'),
              })
            : iconBtn({
                icon: PlayOutline,
                tip: '启动',
                color: '#18a058',
                loading: isBusy,
                onClick: () => act(row, startContainer, '启动'),
              }),
          iconBtn({
            icon: RefreshOutline,
            tip: running ? '重启' : '容器未运行,请直接启动',
            disabled: !running,
            color: '#e88080',
            loading: isBusy && running,
            onClick: () => act(row, restartContainer, '重启'),
          }),
          iconBtn({
            icon: TrashOutline,
            // 只有已停止的容器可删除(docs §4.5):强删会跳过优雅停止,可能损坏数据
            tip: running ? '运行中的容器不可删除,请先停止' : '删除',
            type: 'error',
            disabled: running,
            loading: isBusy && !running,
            onClick: () => confirmRemove(row),
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
  <n-alert v-if="loadError" type="error" :show-icon="true" style="margin-bottom: 12px">
    {{ loadError }}
    <template v-if="loadError.includes('Network') || loadError.includes('请求失败')">
      —— 请确认 Docker 守护进程可用，且 GateBox 有权访问 /var/run/docker.sock
    </template>
  </n-alert>

  <div v-if="info" class="summary">
    <span>Docker {{ info.serverVersion }}</span>
    <span>容器 {{ info.containersRunning }}/{{ info.containers }} 运行中</span>
    <span>镜像 {{ info.images }}</span>
    <span>日志驱动 {{ info.loggingDriver }}</span>
    <span>{{ info.imagePlatform }} · {{ info.ncpu }} 核</span>
    <n-tag v-if="!info.logsReadable" size="tiny" type="warning" :bordered="false">
      当前日志驱动不支持在线查看日志
    </n-tag>
  </div>

  <!-- scroll-x = 各列宽度之和。窄屏时横向滚动,而不是把「名称」压到换行 -->
  <n-data-table
    :columns="columns"
    :data="containers"
    :loading="loading"
    :row-key="(row: ContainerView) => row.id"
    :scroll-x="1024"
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

  <n-modal
    :show="!!removeTarget"
    preset="dialog"
    type="error"
    title="删除容器"
    positive-text="确认删除"
    negative-text="取消"
    @positive-click="doRemove"
    @negative-click="removeTarget = null"
    @close="removeTarget = null"
  >
    <div style="display: flex; flex-direction: column; gap: 10px">
      <span>
        确定删除容器 <b>{{ removeTarget?.name }}</b> 吗？此操作不可撤销。
      </span>
      <n-checkbox v-model:checked="removeVolumes">
        同时删除该容器的匿名卷
      </n-checkbox>
      <n-text depth="3" style="font-size: 12px">
        匿名卷可能保存着应用数据。不确定时请保持不勾选——孤儿卷可稍后在「存储卷」中清理。
      </n-text>
    </div>
  </n-modal>
</template>

<style scoped>
.summary {
  display: flex;
  gap: 18px;
  align-items: center;
  flex-wrap: wrap;
  margin-bottom: 12px;
  font-size: 13px;
  color: #666;
}
</style>
