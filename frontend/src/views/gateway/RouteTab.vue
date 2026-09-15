<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, h } from 'vue'
import { Button, Tag, Popconfirm, Tooltip, Spin, Table, Descriptions, Modal, message } from 'ant-design-vue'
import {
  EyeOutlined, EditOutlined, StopOutlined, CaretRightOutlined,
  ReloadOutlined, FileTextOutlined, DeleteOutlined, ThunderboltOutlined, InfoCircleOutlined,
} from '@ant-design/icons-vue'
import {
  listGroups,
  listPorts,
  stopService,
  startService,
  restartService,
  deleteService,
  gatewayVersion,
  type GroupView,
  type ServiceItem,
  type PortBinding,
} from '../../api/gateway'
import { caddyIcon, dockerIcon } from '../../utils/brandIcons'
import AppFormModal from '../../components/AppFormModal.vue'
import RouteLogsModal from '../../components/RouteLogsModal.vue'

const [messageApi, contextHolder] = message.useMessage()
const groups = ref<GroupView[]>([])
const portBindings = ref<PortBinding[]>([])
const loading = ref(false)
const versionInfo = ref('')
const versionReachable = ref(false)

const showForm = ref(false)
const showEdit = ref<{ service: ServiceItem; group: GroupView } | null>(null)
const logsService = ref<ServiceItem | null>(null)
const viewTarget = ref<ServiceItem | null>(null)

/** 代理表单(新建/编辑共用)打开状态。 */
const appModalShow = computed(() => showForm.value || !!showEdit.value)
function closeAppModal() {
  showForm.value = false
  showEdit.value = null
}

// 拍平所有分组 → 单表;每行带 _group 引用供操作列判断可编辑/归属。
interface FlatRow extends ServiceItem {
  _group: GroupView
}
const rows = computed<FlatRow[]>(() =>
  groups.value.flatMap((g) => g.services.map((s) => ({ ...s, _group: g }) as FlatRow)),
)

// 统计行:Docker 代理 / 反代 / 静态代理 数量。manual+docker 都计入总数。
const stats = computed(() => {
  const docker = rows.value.filter((r) => r.source === 'docker').length
  const reverseProxy = rows.value.filter((r) => r.type === 'reverse_proxy' && r.source === 'manual').length
  const fileServer = rows.value.filter((r) => r.type === 'file_server' && r.source === 'manual').length
  return { total: rows.value.length, docker, reverseProxy, fileServer }
})

/** 协议 → 实际监听端口列表(来自「端口」页,仅启用项;首个为主端口)。 */
const portsByProtocol = computed(() => {
  const m: Record<string, number[]> = {}
  portBindings.value.forEach((b) => {
    if (b.enabled && b.ports?.length) m[b.protocol] = b.ports
  })
  return m
})

function typeTag(row: ServiceItem) {
  if (row.source === 'docker') {
    return h(Tag, { color: 'processing', size: 'small' }, { default: () => 'Docker 自动' })
  }
  if (row.type === 'reverse_proxy') {
    return h(Tag, { color: 'success', size: 'small' }, { default: () => '反向代理' })
  }
  return h(Tag, { color: 'warning', size: 'small' }, { default: () => '静态文件' })
}

function domainHost(d: { protocol: string; subdomain: string; rootDomain: string }): string {
  const host = d.subdomain ? `${d.subdomain}.${d.rootDomain}` : d.rootDomain
  return `${d.protocol}://${host}`
}

/** markdown 代码块(行内 code)样式:发布协议/发布域名/代理详情/端口统一呈现。 */
const CODE_STYLE =
  'display:inline-block;max-width:100%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;' +
  'font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;' +
  'background:#f5f5f5;border:1px solid #eee;border-radius:4px;padding:1px 6px;color:#333;line-height:1.6'
/** 端口链接:代码块外观 + 主色。 */
const CODE_LINK_STYLE = CODE_STYLE + ';color:#1677ff;text-decoration:none;cursor:pointer'

/** 聚合协议的逗号分隔文本(HTTPS/HTTP/其他),以代码块呈现。 */
function protocols(row: ServiceItem) {
  if (!row.domains?.length) return '-'
  const set = new Set<string>()
  row.domains.forEach((d) => set.add(d.protocol === 'https' ? 'HTTPS' : d.protocol === 'http' ? 'HTTP' : '其他'))
  return h('code', { style: CODE_STYLE }, [...set].join('/'))
}

/** 域名列:首个域名 + 域名行数量(多于 1 时);hover 显示全部(一行一个),以代码块呈现。 */
function domainCell(row: ServiceItem) {
  if (!row.domains?.length) return '-'
  const first = domainHost(row.domains[0])
  const label = row.domains.length > 1 ? `${first.split('://')[1]} (${row.domains.length})` : first.split('://')[1]
  if (row.domains.length <= 1) {
    return h('code', { style: CODE_STYLE }, label)
  }
  const all = h(
    'div',
    { style: 'white-space: pre-line; font-family: ui-monospace, Menlo, Consolas, monospace; line-height: 1.7' },
    row.domains.map(domainHost).join('\n'),
  )
  // 多域名:虚线下划线 + info 图标,提示可悬停查看全部域名。
  const trigger = h(
    'code',
    { style: CODE_STYLE + ';cursor:help;border-bottom:1px dashed #1677ff' },
    [
      label,
      h(InfoCircleOutlined, { style: { color: '#1677ff', fontSize: '12px', cursor: 'help', marginLeft: '4px' } }),
    ],
  )
  return h(Tooltip, null, { title: () => all, default: () => trigger })
}

/**
 * 端口列表:customPort 用域名自带端口;否则取「端口」页该协议的实际监听端口
 * (可多端口,如 https:443/9443);端口页无配置时回退协议默认(https=443,http=80)。
 */
function portList(row: ServiceItem): number[] {
  if (!row.domains?.length) return []
  const ports = new Set<number>()
  row.domains.forEach((d) => {
    if (d.customPort && d.port) {
      ports.add(d.port)
      return
    }
    const configured = portsByProtocol.value[d.protocol]
    if (configured?.length) configured.forEach((p) => ports.add(p))
    else ports.add(d.protocol === 'https' ? 443 : d.protocol === 'http' ? 80 : 0)
  })
  return [...ports].filter((p) => p > 0)
}

/**
 * 端口列:每个端口一个超链接(多域名取第一个域名的 host),点击在新标签打开
 * <协议>://<host>[:端口]。非 HTTP 协议或无 host 时退化为代码块文本。
 */
function portCell(row: ServiceItem) {
  const ports = portList(row)
  if (!ports.length) return '-'
  const d = row.domains![0]
  const host = d.subdomain ? `${d.subdomain}.${d.rootDomain}` : d.rootDomain
  const isHTTP = d.protocol === 'http' || d.protocol === 'https'
  const defPort = d.protocol === 'https' ? 443 : d.protocol === 'http' ? 80 : 0
  const linkable = isHTTP && !!host
  return h(
    'div',
    { style: 'display:flex;gap:6px;flex-wrap:wrap;align-items:center' },
    ports.map((p) => {
      if (!linkable) return h('code', { style: CODE_STYLE }, String(p))
      const suffix = p === defPort ? '' : `:${p}`
      return h(
        'a',
        { href: `${d.protocol}://${host}${suffix}`, target: '_blank', rel: 'noopener', style: CODE_LINK_STYLE },
        String(p),
      )
    }),
  )
}

/** 代理详情列,以代码块呈现。 */
function proxyDetail(row: ServiceItem) {
  if (row.type === 'reverse_proxy') {
    if (!row.upstream?.length) return '-'
    const proto = row.upstreamProto === 'https' ? 'https' : 'http'
    const first = h('code', { style: CODE_STYLE }, `${proto}://${row.upstream[0]}`)
    if (row.upstream.length <= 1) return first
    const extra = h('span', { style: 'color:#888; font-size:12px; margin-left:4px' }, `+${row.upstream.length - 1}`)
    return h('span', { style: 'display:inline-flex;align-items:center' }, [first, extra])
  }
  if (row.type === 'file_server') {
    return h('code', { style: CODE_STYLE }, row.root || '-')
  }
  return '-'
}

function upstreamAddr(row: ServiceItem) {
  return row.upstream?.[0]?.split(':')[0] || '-'
}
function upstreamPort(row: ServiceItem) {
  return row.upstream?.[0]?.split(':')[1] || '-'
}

function healthTag(row: ServiceItem) {
  if (row.type !== 'reverse_proxy') return '-'
  const map: Record<string, { type: any; text: string }> = {
    healthy: { type: 'success', text: '正常' },
    unhealthy: { type: 'error', text: '故障' },
    unknown: { type: 'default', text: '未知' },
  }
  const s = map[row.health] || map.unknown
  return h(Tag, { color: s.type, size: 'small' }, { default: () => s.text })
}

/** 健康列:docker 异常容器(容器未运行)优先标红;派生告警加黄色标记。 */
function healthCell(row: ServiceItem) {
  const tags: ReturnType<typeof h>[] = []
  if (row.source === 'docker' && row.containerState && row.containerState !== 'running') {
    tags.push(h(Tag, { color: 'error', size: 'small' }, { default: () => `容器${row.containerState}` }))
  } else {
    tags.push(healthTag(row))
  }
  if (row.derivedWarning) {
    tags.push(
      h(Tooltip, { title: row.derivedWarning }, { default: () => h(Tag, { color: 'warning', size: 'small' }, { default: () => '告警' }) }),
    )
  }
  return tags.length === 1 ? tags[0] : h('div', { style: 'display:flex;gap:6px' }, { default: () => tags })
}

function statusTag(row: ServiceItem) {
  return h(
    Tag,
    { color: row.enabled ? 'success' : 'default', size: 'small' },
    { default: () => (row.enabled ? '运行中' : '未运行') },
  )
}

async function toggleEnabled(row: ServiceItem) {
  try {
    if (row.enabled) {
      await stopService(row.id)
      messageApi.success('已停止')
    } else {
      await startService(row.id)
      messageApi.success('已启动')
    }
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

async function doRestart(row: ServiceItem) {
  try {
    await restartService(row.id)
    messageApi.success('已重启')
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

async function doDeleteService(row: ServiceItem) {
  try {
    await deleteService(row.id)
    messageApi.success('已删除')
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

/** 操作按钮:icon-only(不同颜色) + hover Tooltip 显示文字。 */
function actionBtn(icon: any, color: string, title: string, onClick?: () => void) {
  return h(
    Tooltip,
    { title, mouseEnterDelay: 0.3 },
    {
      default: () =>
        h(
          Button,
          { size: 'small', type: 'text', onClick },
          { default: () => h(icon, { style: { fontSize: '14px', color } }) },
        ),
    },
  )
}

function renderRow(row: ServiceItem, group: GroupView) {
  if (!group.editable) {
    // Docker 自动派生行:自动代理(去容器页) + 停止/启动/重启/日志(全部只作用于 caddy)。
    const dbtns: any[] = [
      actionBtn(ThunderboltOutlined, '#722ed1', '自动代理：前往容器页管理', () =>
        messageApi.info('Docker 自动代理请在「容器」中通过 caddy label 管理'),
      ),
      row.enabled
        ? actionBtn(StopOutlined, '#fa8c16', '停止', () => toggleEnabled(row))
        : actionBtn(CaretRightOutlined, '#52c41a', '启动', () => toggleEnabled(row)),
      actionBtn(ReloadOutlined, '#13c2c2', '重启', () => doRestart(row)),
      actionBtn(FileTextOutlined, '#1677ff', '日志', () => (logsService.value = row)),
    ]
    return h('div', {
      style: 'display:flex; align-items:center; justify-content:flex-end; gap:2px; flex-wrap:nowrap; white-space:nowrap',
    }, dbtns)
  }
  const btns: any[] = [
    actionBtn(EyeOutlined, '#1677ff', '查看', () => (viewTarget.value = row)),
    actionBtn(EditOutlined, '#722ed1', '编辑', () => (showEdit.value = { service: row, group })),
    row.enabled
      ? actionBtn(StopOutlined, '#fa8c16', '停止', () => toggleEnabled(row))
      : actionBtn(CaretRightOutlined, '#52c41a', '启动', () => toggleEnabled(row)),
    actionBtn(ReloadOutlined, '#13c2c2', '重启', () => doRestart(row)),
    actionBtn(FileTextOutlined, '#1677ff', '日志', () => (logsService.value = row)),
    h(
      Popconfirm,
      {
        title: row.enabled ? '该服务运行中，需先停止才能删除' : '确认删除该服务？',
        onConfirm: () => doDeleteService(row),
      },
      {
        default: () =>
          h(
            Button,
            { size: 'small', type: 'text', danger: true },
            { default: () => h(DeleteOutlined, { style: { fontSize: '14px' } }) },
          ) as any,
      },
    ),
  ]
  return h('div', {
    style: 'display:flex; align-items:center; justify-content:flex-end; gap:2px; flex-wrap:nowrap; white-space:nowrap',
  }, btns)
}

/** 归属行:<icon>/<归属>。caddy = 手动服务,Caddy 图标;docker = 编排派生,Docker 图标。 */
function ownerLine(row: ServiceItem) {
  if (!row.appName) return null
  return h(
    'span',
    {
      style: 'display:inline-flex;align-items:center;gap:4px;color:#888;font-size:12px;' +
        'min-width:0;max-width:100%;overflow:hidden',
    },
    [
      row.source === 'docker' ? dockerIcon(12) : caddyIcon(12),
      h('span', { style: 'overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, row.appName),
    ],
  )
}

const columns = [
  {
    title: '服务名称', dataIndex: 'name', key: 'name', width: 180, fixed: 'left' as const,
    // 名称 + 归属(icon/名称)双行,样式对齐「容器」页名称列。
    customRender: ({ record }: { record: FlatRow }) => {
      const nameNode = h('span', { style: 'font-weight:500;overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, record.name)
      const name = record.description
        ? h(Tooltip, { title: record.description }, { default: () => nameNode })
        : nameNode
      const owner = ownerLine(record)
      return h('div', { style: 'display:flex;flex-direction:column;gap:2px;min-width:0' }, owner ? [name, owner] : [name])
    },
  },
  { title: '服务状态', key: 'status', width: 80, customRender: ({ record }: { record: FlatRow }) => statusTag(record) },
  { title: '发布协议', key: 'protocols', width: 80, customRender: ({ record }: { record: FlatRow }) => protocols(record) },
  { title: '发布域名', key: 'domains', width: 170, customRender: ({ record }: { record: FlatRow }) => domainCell(record) },
  { title: '发布端口', key: 'ports', width: 110, customRender: ({ record }: { record: FlatRow }) => portCell(record) },
  { title: '代理类型', key: 'type', width: 100, customRender: ({ record }: { record: FlatRow }) => typeTag(record) },
  { title: '健康', key: 'health', width: 90, customRender: ({ record }: { record: FlatRow }) => healthCell(record) },
  { title: '代理详情', key: 'detail', width: 220, customRender: ({ record }: { record: FlatRow }) => proxyDetail(record) },
  { title: '操作', key: 'actions', width: 168, fixed: 'right' as const, customRender: ({ record }: { record: FlatRow }) => renderRow(record, record._group) },
]

async function load(silent = false) {
  if (!silent) loading.value = true
  try {
    const [g, p] = await Promise.all([listGroups(), listPorts().catch(() => portBindings.value)])
    groups.value = g
    portBindings.value = p
  } catch (e: any) {
    // 轮询失败静默(避免后台刷新刷屏);首次/手动刷新才弹错。
    if (!silent) messageApi.error(e.message)
  } finally {
    loading.value = false
  }
}

/** 拉取 caddy 版本(每 30s 刷新;失败保留旧值)。 */
async function loadVersion() {
  try {
    const v = await gatewayVersion()
    versionInfo.value = v.version
    versionReachable.value = v.reachable
  } catch { /* 保持旧值 */ }
}

// 10s 轮询:容器 label 变更 / 健康快照变化无需人工刷新即可反映(ADR-018 派生视图实时性)。
// docker 自动派生不落库,GET /gateway/apps 每次实时重算(后端 deriveDockerServices)。
let timer: ReturnType<typeof setInterval> | undefined
let versionTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  load()
  loadVersion()
  timer = setInterval(() => load(true), 10_000)
  versionTimer = setInterval(loadVersion, 30_000)
})
onUnmounted(() => {
  if (timer) clearInterval(timer)
  if (versionTimer) clearInterval(versionTimer)
})
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; align-items: center; justify-content: space-between">
    <div style="display: flex; gap: 18px; font-size: 13px; color: #888; align-items: baseline; flex-wrap: wrap">
      <span>Caddy <span style="font-family: monospace; font-size: 12px">{{ versionReachable ? versionInfo : '-' }}</span></span>
      <span>代理总数 <b>{{ stats.total }}</b></span>
      <span>Docker 代理 <b>{{ stats.docker }}</b></span>
      <span>反代数量 <b>{{ stats.reverseProxy }}</b></span>
      <span>静态代理 <b>{{ stats.fileServer }}</b></span>
    </div>
    <Button type="primary" @click="showForm = true">+ 创建代理</Button>
  </div>

  <Spin :spinning="loading">
    <Table
      :columns="columns"
      :data-source="rows"
      :row-key="(r: FlatRow) => r.id || (r._group.id + ':' + r.name)"
      :pagination="false"
      size="small"
      :table-layout="'fixed'"
      :scroll="{ x: 1210 }"
    />
    <div v-if="!rows.length" style="color:#999; font-size:13px; padding:12px 14px">暂无服务</div>
  </Spin>

  <AppFormModal
    :show="appModalShow"
    :edit-target="showEdit"
    @update:show="closeAppModal"
    @saved="load"
  />
  <RouteLogsModal v-if="logsService" :service="logsService" @close="logsService = null" />

  <Modal
    :open="!!viewTarget"
    title="服务详情"
    :footer="null"
    width="640"
    @cancel="viewTarget = null"
  >
    <Descriptions v-if="viewTarget" :column="1" size="small" bordered>
      <Descriptions.Item label="名称">{{ viewTarget.name }}</Descriptions.Item>
      <Descriptions.Item label="状态">{{ viewTarget.enabled ? '运行中' : '未运行' }}</Descriptions.Item>
      <Descriptions.Item label="类型">
        {{ viewTarget.type === 'reverse_proxy' ? '反向代理' : '静态文件' }}
        <template v-if="viewTarget.source === 'docker'">（Docker 自动）</template>
      </Descriptions.Item>
      <Descriptions.Item label="域名">
        <template v-if="viewTarget.domains?.length">
          <span v-for="(d, i) in viewTarget.domains" :key="i" style="display:block; font-family:monospace">
            {{ d.protocol }}://{{ d.subdomain ? d.subdomain + '.' : '' }}{{ d.rootDomain }}
            <template v-if="d.customPort && d.port">:{{ d.port }}</template>
          </span>
        </template>
        <template v-else>-</template>
      </Descriptions.Item>
      <Descriptions.Item v-if="viewTarget.type === 'reverse_proxy'" label="后端地址">
        <span v-for="(u, i) in viewTarget.upstream || []" :key="i" style="display:block; font-family:monospace">
          {{ viewTarget.upstreamProto }}://{{ u }}
        </span>
      </Descriptions.Item>
      <Descriptions.Item v-else label="静态目录">
        <span style="font-family:monospace">{{ viewTarget.root || '-' }}</span>
      </Descriptions.Item>
      <Descriptions.Item label="描述">{{ viewTarget.description || '-' }}</Descriptions.Item>
      <Descriptions.Item label="创建时间">{{ new Date(viewTarget.createdAt).toLocaleString() }}</Descriptions.Item>
    </Descriptions>
  </Modal>
</template>
