<script setup lang="ts">
import { ref, onMounted, onUnmounted, h, computed } from 'vue'
import {
  NDataTable, NTag, NButton, NSpace, NPopover, NAlert, NModal, NCheckbox,
  NText, NSpin, useMessage,
} from 'naive-ui'
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
      trigger: () => h(NText, { style: 'cursor: default; border-bottom: 1px dashed #aaa' }, { default: () => `端口：${row.ports.length}` }),
      default: () =>
        h('div', { style: 'display: flex; flex-direction: column; gap: 6px' },
          row.ports.map((p: PortMapping) =>
            h('div', { style: 'display: flex; align-items: center; gap: 6px; white-space: nowrap' }, [
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

const columns = computed(() => [
  {
    title: '名称',
    key: 'name',
    render: (row: ContainerView) => {
      const meta = sourceMeta[row.source] || sourceMeta.loose
      return h('div', { style: 'display: flex; flex-direction: column; gap: 2px' }, [
        h('div', { style: 'display: flex; align-items: center; gap: 6px' }, [
          h('span', { style: 'font-weight: 600' }, row.name),
          h(NPopover, { trigger: 'hover' }, {
            trigger: () => h(NTag, { size: 'tiny', type: meta.type, bordered: false }, { default: () => meta.label }),
            default: () => meta.tip,
          }),
          row.health
            ? h(NTag, { size: 'tiny', type: row.health === 'healthy' ? 'success' : 'warning', bordered: false },
                { default: () => row.health })
            : null,
        ]),
        h(NText, { depth: 3, style: 'font-size: 12px' }, { default: () => row.image }),
      ])
    },
  },
  {
    title: '状态',
    key: 'state',
    width: 110,
    render: (row: ContainerView) =>
      h(NTag, { size: 'small', type: row.state === 'running' ? 'success' : 'default', bordered: false },
        { default: () => (row.state === 'running' ? '运行中' : row.state === 'exited' ? '已停止' : row.state) }),
  },
  { title: '端口', key: 'ports', width: 110, render: renderPorts },
  {
    title: 'CPU',
    key: 'cpu',
    width: 100,
    render: (row: ContainerView) => {
      if (row.state !== 'running') return h(NText, { depth: 3 }, { default: () => '-' })
      if (!row.hasStats) return h(NSpin, { size: 12 })
      return h('span', `${row.cpuPercent.toFixed(2)} %`)
    },
  },
  {
    title: '内存',
    key: 'memory',
    width: 160,
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
  { title: '创建时间', key: 'createdAt', width: 140, render: (row: ContainerView) => fmtTime(row.createdAt) },
  { title: '运行时间', key: 'uptime', width: 130, render: (row: ContainerView) => fmtUptime(row.startedAt) },
  {
    title: '操作',
    key: 'actions',
    width: 330,
    render: (row: ContainerView) => {
      const isBusy = !!busy.value[row.id]
      const running = row.state === 'running'
      return h(NSpace, { size: 4, wrap: false }, {
        default: () => [
          h(NButton, { size: 'tiny', onClick: () => (logsTarget.value = row) }, { default: () => '日志' }),
          h(NButton, {
            size: 'tiny',
            disabled: !running,
            onClick: () => (execTarget.value = row),
          }, { default: () => '控制台' }),
          running
            ? h(NButton, { size: 'tiny', loading: isBusy, onClick: () => act(row, restartContainer, '重启') },
                { default: () => '重启' })
            : h(NButton, { size: 'tiny', type: 'primary', ghost: true, loading: isBusy, onClick: () => act(row, startContainer, '启动') },
                { default: () => '启动' }),
          running
            ? h(NButton, { size: 'tiny', loading: isBusy, onClick: () => act(row, stopContainer, '停止') },
                { default: () => '停止' })
            : null,
          h(NButton, {
            size: 'tiny',
            type: 'error',
            ghost: true,
            // 只有已停止的容器可删除(docs §4.5):强删会跳过优雅停止,可能损坏数据
            disabled: running,
            loading: isBusy,
            onClick: () => confirmRemove(row),
          }, { default: () => '删除' }),
        ].filter(Boolean),
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

  <n-data-table
    :columns="columns"
    :data="containers"
    :loading="loading"
    :row-key="(row: ContainerView) => row.id"
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
