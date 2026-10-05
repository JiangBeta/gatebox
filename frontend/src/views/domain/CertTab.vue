<script setup lang="ts">
import { ref, onMounted, computed, h, watch } from 'vue'
import { useRoute } from 'vue-router'
import { Empty, Table, Button, Modal, Popconfirm, Input, Tooltip, Tag, Card, Space, Select, message } from 'ant-design-vue'
import {
  EyeOutlined, ReloadOutlined, DeleteOutlined, FileTextOutlined, FileSearchOutlined,
  CopyOutlined, PlusCircleOutlined,
} from '@ant-design/icons-vue'
import { caddyIcon, dockerIcon } from '../../utils/brandIcons'
import {
  getCertDetail, renewCert, deleteCert, listCertLogs, getCertLog, listSubdomains,
  getACMEEmail, setACMEEmail,
  type CertDetail, type CertLog, type SubdomainCert, type SubdomainSource,
} from '../../api/certificates'
import { listGroups, type GroupView, type ServiceItem } from '../../api/gateway'
import AppFormModal from '../../components/AppFormModal.vue'
import ComposeEditorModal from '../../components/ComposeEditorModal.vue'
import RunLogModal from '../../components/RunLogModal.vue'

const [messageApi, contextHolder] = message.useMessage()
const route = useRoute()

const subdomains = ref<SubdomainCert[]>([])
const detail = ref<CertDetail | null>(null)
const detailShow = ref(false)
const detailLoading = ref(false)
const renewing = ref<Record<string, boolean>>({})

// 证书状态筛选 + 根域名筛选（原型 certFilter / rootFilter）。
// 两个筛选叠在一起：状态筛的是"要不要管"，根域名筛的是"哪片"。
const certFilter = ref<'all' | 'valid' | 'expiring' | 'expired' | 'unissued'>('all')
const rootFilter = ref('all')

// ACME 注册邮箱(全局,内联管理)
const acmeEmail = ref('')
const emailSaving = ref(false)

// 来源点击 → 复用网关服务编辑抽屉 / 编排项目编辑抽屉
const svcEditorShow = ref(false)
const svcEditTarget = ref<{ service: ServiceItem; group: GroupView } | null>(null)
const composeEditorShow = ref(false)
const composeProject = ref<string | null>(null)
const composeService = ref<string | null>(null)
let groupsCache: GroupView[] | null = null

const logs = ref<CertLog[]>([])
const logsShow = ref(false)
const logsLoading = ref(false)
const logTarget = ref('')
const logDetail = ref('')
const logShow = ref(false)
const logLoading = ref(false)

/** 日志弹层（地图/文本双视图，RunLogModal）。 */
const mapLog = ref<{
  chain: string
  trigger: string
  action: string
  state: string
  startedAt: string
  endedAt: string
  evidence: string
} | null>(null)

function formatTime(s?: string) {
  if (!s) return '-'
  return s.replace('T', ' ').slice(0, 19)
}

/** 距离到期日剩余天数(向上取整,最小 0);无证书返回 null。 */
function daysLeft(s?: string): number | null {
  if (!s) return null
  const ms = new Date(s).getTime() - Date.now()
  if (isNaN(ms)) return null
  return Math.max(0, Math.ceil(ms / 86400000))
}

/** certStatus 四态：未签发 / 已过期 / 即将过期(≤30 天) / 有效。与原型 certStatusOf 一致。 */
type CertStatusKey = 'valid' | 'expiring' | 'expired' | 'unissued'
function certStatusOf(s: SubdomainCert): { key: CertStatusKey; label: string; color: string } {
  if (!s.hasCert || !s.notAfter) return { key: 'unissued', label: '未签发', color: '#8b95a7' }
  const t = new Date(s.notAfter).getTime()
  if (t < Date.now()) return { key: 'expired', label: '已过期', color: '#ef4444' }
  const d = daysLeft(s.notAfter) ?? 0
  if (d <= 30) return { key: 'expiring', label: '即将过期', color: '#d29922' }
  return { key: 'valid', label: '有效', color: '#22c55e' }
}

const STATUS_FILTERS: Array<{ key: 'all' | CertStatusKey; label: string }> = [
  { key: 'all', label: '全部' },
  { key: 'valid', label: '有效' },
  { key: 'expiring', label: '即将过期' },
  { key: 'expired', label: '已过期' },
  { key: 'unissued', label: '未签发' },
]

function statusCount(k: 'all' | CertStatusKey): number {
  if (k === 'all') return subdomains.value.length
  return subdomains.value.filter((s) => certStatusOf(s).key === k).length
}

const rootOptions = computed(() => {
  const roots = [...new Set(subdomains.value.map((s) => s.rootDomain).filter(Boolean))] as string[]
  return [{ label: '根域名：全部', value: 'all' }, ...roots.map((r) => ({ label: r, value: r }))]
})

const filteredSubdomains = computed(() =>
  subdomains.value.filter(
    (s) =>
      (certFilter.value === 'all' || certStatusOf(s).key === certFilter.value) &&
      (rootFilter.value === 'all' || s.rootDomain === rootFilter.value),
  ),
)

/** daysColorHex 剩余天数的十六进制色（状态标签内是纯 span，用不了 AntD 的 color 名）。 */
function daysColorHex(st: { key: CertStatusKey }): string {
  if (st.key === 'expired') return '#ef4444'
  if (st.key === 'expiring') return '#d29922'
  if (st.key === 'valid') return '#22c55e'
  return '#8b95a7'
}

/** 颁发机构人类可读名称(Let's Encrypt 中间证书 CN 如 YE1/YE2)。
 * 未知时原样显示,title 里放原始值。 */
function friendlyIssuer(issuer?: string) {
  const m: Record<string, string> = {
    YE1: "Let's Encrypt E1（ECC 中间证书）",
    YE2: "Let's Encrypt E2（ECC 中间证书）",
    R10: "Let's Encrypt R10（RSA 中间证书）",
    R11: "Let's Encrypt R11（RSA 中间证书）",
    R12: "Let's Encrypt R12（RSA 中间证书）",
    R13: "Let's Encrypt R13（RSA 中间证书）",
  }
  return (issuer && m[issuer]) || (issuer ? `颁发机构 ${issuer}` : '-')
}

function actionText(a: string) {
  return { ensure: '自动签发', renew: '重新申请', delete: '删除' }[a] || a
}

async function load() {
  try {
    subdomains.value = await listSubdomains()
  } catch (e: any) {
    messageApi.error('读取二级域名失败 — ' + e.message)
  }
}

async function loadEmail() {
  try {
    acmeEmail.value = (await getACMEEmail()).email || ''
  } catch (e: any) {
    messageApi.error('读取 ACME 邮箱失败 — ' + e.message)
  }
}

async function saveEmail() {
  emailSaving.value = true
  try {
    await setACMEEmail(acmeEmail.value.trim())
    messageApi.success('ACME 注册邮箱已保存')
  } catch (e: any) {
    messageApi.error('保存失败 — ' + e.message)
  } finally {
    emailSaving.value = false
  }
}

onMounted(() => {
  load()
  loadEmail()
})

/**
 * ?filter=expiring —— 域名概览统计卡的「证书数量」点进来时带筛选（原型 goto(..., filter)）。
 * 只认白名单里的值：query 是用户可写的，脏值落到表上会静默空列表。
 */
watch(
  () => route.query.filter,
  (v) => {
    const s = String(v || '')
    if (STATUS_FILTERS.some((f) => f.key === s)) certFilter.value = s as typeof certFilter.value
  },
  { immediate: true },
)

// --- 来源点击 → 打开编辑抽屉 ---

function sourceTip(s: SubdomainSource) {
  if (s.type === 'docker') {
    return `Docker 项目：${s.displayName || s.projectName || '-'} / ${s.serviceName || '-'}（点击编辑）`
  }
  return `Caddy 服务：${s.appName ? s.appName + ' / ' : ''}${s.serviceName || s.serviceId || '-'}（点击编辑）`
}

async function openSource(s: SubdomainSource) {
  if (s.type === 'docker') {
    if (!s.projectName) {
      messageApi.warning('缺少项目名，无法打开编辑')
      return
    }
    composeProject.value = s.projectName
    composeService.value = s.serviceName || null
    composeEditorShow.value = true
    return
  }
  if (!s.serviceId) {
    messageApi.warning('缺少服务 ID，无法打开编辑')
    return
  }
  try {
    if (!groupsCache) groupsCache = await listGroups()
    for (const g of groupsCache) {
      const svc = g.services.find((x) => x.id === s.serviceId)
      if (svc) {
        svcEditTarget.value = { service: svc, group: g }
        svcEditorShow.value = true
        return
      }
    }
    messageApi.warning('未找到对应服务，可能已被删除')
  } catch (e: any) {
    messageApi.error('打开编辑失败 — ' + e.message)
  }
}

function onSvcSaved() {
  groupsCache = null
  load()
}

function onSvcEditorShow(v: boolean) {
  svcEditorShow.value = v
  if (!v) svcEditTarget.value = null
}

function onComposeSaved() {
  load()
}

// --- 证书操作 ---

async function view(row: SubdomainCert) {
  detailLoading.value = true
  detailShow.value = true
  try {
    detail.value = await getCertDetail(row.fqdn)
  } catch (e: any) {
    messageApi.error('读取证书失败 — ' + e.message)
    detailShow.value = false
  } finally {
    detailLoading.value = false
  }
}

async function doRenew(row: SubdomainCert) {
  renewing.value = { ...renewing.value, [row.fqdn]: true }
  try {
    await renewCert(row.fqdn)
    messageApi.success(`${row.fqdn}:证书已重新申请`)
    await load()
  } catch (e: any) {
    messageApi.error(`${row.fqdn}:重新申请失败 — ${e.message}`)
  } finally {
    const next = { ...renewing.value }
    delete next[row.fqdn]
    renewing.value = next
  }
}

async function doDelete(row: SubdomainCert) {
  try {
    await deleteCert(row.fqdn)
    messageApi.success(`${row.fqdn}:证书已删除`)
    await load()
  } catch (e: any) {
    messageApi.error(`${row.fqdn}:删除失败 — ${e.message}`)
  }
}

async function openLogs(row: SubdomainCert) {
  logsShow.value = true
  logsLoading.value = true
  logTarget.value = row.fqdn
  try {
    logs.value = await listCertLogs(row.fqdn)
  } catch (e: any) {
    messageApi.error('读取日志失败 — ' + e.message)
    logsShow.value = false
  } finally {
    logsLoading.value = false
  }
}

async function viewLog(row: CertLog) {
  if (!row.logFile) {
    messageApi.info('该日志无 acme.sh 原始输出')
    return
  }
  logLoading.value = true
  logShow.value = true
  try {
    const res = await getCertLog(row.id)
    logDetail.value = res.log
  } catch (e: any) {
    messageApi.error('读取日志内容失败 — ' + e.message)
    logShow.value = false
  } finally {
    logLoading.value = false
  }
}

/**
 * openMapLog 打开地图视图。
 *
 * 用**最新一条**日志（原型「展示最后一份日志」），原始输出读 getCertLog——
 * 没存原始输出的条目也能开地图，只是没有对账步骤（弹层里会明说）。
 */
async function openMapLog(row: CertLog) {
  let evidence = ''
  if (row.logFile) {
    try {
      evidence = (await getCertLog(row.id)).log || ''
    } catch {
      evidence = ''
    }
  }
  mapLog.value = {
    chain: `acme_sequence · ${row.fqdn}`,
    // 证书日志只记了 action，没记是谁触发的（acme.sh 自身 cron？用户点申请？），
    // 这里不猜：留空让弹层显示「—」，动作按记录里的 action 显示。
    trigger: '',
    action: row.action,
    state: row.status === 'success' ? 'success' : 'failed',
    startedAt: row.createdAt,
    endedAt: row.createdAt,
    evidence,
  }
}

async function copyFqdn(fqdn: string) {
  try {
    await navigator.clipboard.writeText(fqdn)
    messageApi.success(`已复制 ${fqdn}`)
  } catch {
    // 非安全上下文（局域网 http）下 clipboard 不可用，退回提示让用户自己拷。
    messageApi.info(`请手动复制：${fqdn}`)
  }
}

/** 来源列:<品牌图标> <服务名>(对齐网关页归属图标),点击打开对应编辑抽屉。 */
function sourceCell(record: SubdomainCert) {
  if (!record.sources?.length) return '-'
  return h(
    'div',
    { style: 'display:flex;gap:12px;flex-wrap:wrap;align-items:center' },
    record.sources.map((s) => {
      const label = s.serviceName || s.displayName || s.projectName || ''
      return h(
        Tooltip,
        { title: sourceTip(s) },
        {
          default: () =>
            h(
              'span',
              {
                style: 'display:inline-flex;align-items:center;gap:4px;cursor:pointer;min-width:0',
                onClick: () => openSource(s),
              },
              [
                s.type === 'docker' ? dockerIcon(14) : caddyIcon(14),
                h('span', { style: 'overflow:hidden;text-overflow:ellipsis;white-space:nowrap' }, label),
              ],
            ),
        },
      )
    }),
  )
}

/**
 * actionCell 操作列。
 *
 * 抄原型顺序：查看 → 申请/重新申请 → 日志 → 复制域名 → 删除。
 * 「申请证书」用 PlusCircle、已签发用「重新申请」，两者语义不同不能同一个图标糊弄。
 */
function actionCell(record: SubdomainCert) {
  const btns = []
  if (record.hasCert) {
    btns.push(
      h(Tooltip, { title: '查看（公钥/私钥）' }, { default: () => h(Button, { size: 'small', style: { color: '#1677ff' }, onClick: () => view(record) }, { default: () => h(EyeOutlined) }) }),
    )
  }
  const renewTitle = record.hasCert ? '重新申请' : '申请证书'
  btns.push(
    h(Tooltip, { title: renewTitle }, {
      default: () => h(Popconfirm, { title: `${renewTitle} ${record.fqdn}？`, onConfirm: () => doRenew(record) }, {
        default: () => h(Button, { size: 'small', ghost: true, style: { color: record.hasCert ? '#13c2c2' : '#52c41a' }, loading: renewing.value[record.fqdn] },
          { default: () => (record.hasCert ? h(ReloadOutlined) : h(PlusCircleOutlined)) }),
      }),
    }),
  )
  // 日志：地图视图看步骤，列表看历史，两条路都要有。
  btns.push(
    h(Tooltip, { title: '日志（地图视图）' }, {
      default: () => h(Button, { size: 'small', style: { color: '#1677ff' }, onClick: () => openMapLogQuick(record) }, { default: () => h(FileTextOutlined) }),
    }),
    h(Tooltip, { title: `证书日志${record.hasCert ? '' : '（查看申请失败原因）'}` }, { default: () => h(Button, { size: 'small', onClick: () => openLogs(record) }, { default: () => h(FileSearchOutlined) }) }),
    h(Tooltip, { title: '复制域名' }, { default: () => h(Button, { size: 'small', style: { color: '#8b95a7' }, onClick: () => copyFqdn(record.fqdn) }, { default: () => h(CopyOutlined) }) }),
  )
  if (record.hasCert) {
    btns.push(
      h(Tooltip, { title: '删除' }, { default: () => h(Popconfirm, { title: `删除 ${record.fqdn} 的证书？`, okText: '删除', okButtonProps: { danger: true }, onConfirm: () => doDelete(record) }, { default: () => h(Button, { size: 'small', danger: true }, { default: () => h(DeleteOutlined) }) }) }),
    )
  }
  return h('div', { style: 'display:flex;gap:6px' }, btns)
}

/**
 * openMapLogQuick 从域名行直接开地图视图：取该域名最新一条证书日志。
 * 一条都没有时也开——弹层会显示「无日志」，比静默失败可排查。
 */
async function openMapLogQuick(record: SubdomainCert) {
  let last: CertLog | null = null
  try {
    last = (await listCertLogs(record.fqdn))[0] || null
  } catch (e: any) {
    messageApi.error('读取日志失败 — ' + e.message)
    return
  }
  if (!last) {
    mapLog.value = {
      chain: `acme_sequence · ${record.fqdn}`,
      trigger: 'manual',
      action: record.hasCert ? 'renew' : 'ensure',
      state: 'unknown',
      startedAt: '',
      endedAt: '',
      evidence: '',
    }
    return
  }
  await openMapLog(last)
}

const columns = [
  { title: '二级域名', dataIndex: 'fqdn', key: 'fqdn', customRender: ({ record }: { record: SubdomainCert }) => h('b', {}, record.fqdn) },
  {
    // 路由：这条二级域名在 Caddy 里对外的入口（协议 + 主机名）。
    title: '路由',
    key: 'route',
    width: 190,
    customRender: ({ record }: { record: SubdomainCert }) => {
      const proto = record.protocol || 'https'
      return h(Tooltip, { title: `${proto}://${record.fqdn}` }, {
        default: () => h('span', { style: 'color:#1677ff' }, `${proto}://${record.fqdn}`),
      })
    },
  },
  {
    title: '服务',
    key: 'sources',
    width: 200,
    customRender: ({ record }: { record: SubdomainCert }) => sourceCell(record),
  },
  {
    // 证书：状态 + 剩余天数合并成一列（原型的 certcell）；颁发机构 / 到期时间放 tooltip。
    title: '证书',
    key: 'cert',
    width: 150,
    customRender: ({ record }: { record: SubdomainCert }) => {
      const st = certStatusOf(record)
      const d = daysLeft(record.notAfter)
      return h(Tooltip, {
        title: `${st.label}${record.hasCert ? ` · 到期 ${formatTime(record.notAfter)} · ${friendlyIssuer(record.issuer)}` : ''}`,
      }, {
        default: () => h('span', { style: 'display:inline-flex;align-items:center;gap:6px' }, [
          h(Tag, { color: record.hasCert ? (st.key === 'valid' ? 'success' : st.key === 'expiring' ? 'warning' : 'error') : 'default', size: 'small', bordered: false },
            { default: () => st.label }),
          record.hasCert && d !== null
            ? h('span', { style: 'color:#888;font-size:12px' }, [
                '剩余 ', h('b', { style: { color: daysColorHex(st) } }, String(d)), ' 天',
              ])
            : null,
        ]),
      })
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 250,
    fixed: 'right' as const,
    customRender: ({ record }: { record: SubdomainCert }) => actionCell(record),
  },
]

const logColumns = [
  { title: '时间', dataIndex: 'createdAt', key: 'createdAt', customRender: ({ record }: { record: any }) => formatTime(record.createdAt), width: 150 },
  { title: '域名', dataIndex: 'fqdn', key: 'fqdn' },
  { title: '动作', dataIndex: 'action', key: 'action', width: 100, customRender: ({ record }: { record: any }) => actionText(record.action) },
  {
    title: '状态',
    dataIndex: 'status',
    key: 'status',
    width: 90,
    customRender: ({ record }: { record: any }) => h(Tag, { color: record.status === 'success' ? 'success' : 'error', size: 'small' }, { default: () => (record.status === 'success' ? '成功' : '失败') }),
  },
  { title: '消息', dataIndex: 'message', key: 'message' },
  {
    title: '操作',
    key: 'action',
    width: 150,
    customRender: ({ record }: { record: CertLog }) =>
      h('div', { style: 'display:flex;gap:6px' }, [
        h(Tooltip, { title: '地图视图（步骤对账）' }, {
          default: () => h(Button, { size: 'small', onClick: () => openMapLog(record) }, { default: () => h(FileTextOutlined) }),
        }),
        record.logFile
          ? h(Tooltip, { title: '查看 acme.sh 原始输出' }, {
              default: () => h(Button, { size: 'small', onClick: () => viewLog(record) }, { default: () => h(FileSearchOutlined) }),
            })
          : null,
      ]),
  },
]
</script>

<template>
  <contextHolder />

  <!-- ACME 注册邮箱(全局,内联管理) -->
  <Card size="small" style="margin-bottom: 12px">
    <Space wrap>
      <span>ACME 注册邮箱</span>
      <Input
        v-model:value="acmeEmail"
        placeholder="you@example.com"
        style="width: 280px"
        allow-clear
        @press-enter="saveEmail"
      />
      <Button type="primary" :loading="emailSaving" @click="saveEmail">保存</Button>
      <span style="color: #888; font-size: 12px">
        用于
        <a href="https://app.zerossl.com/signup" target="_blank" rel="noopener noreferrer">acme.sh</a>
        注册账号(全局唯一,所有证书共用);留空则不指定
      </span>
    </Space>
  </Card>

  <!-- 状态筛选条（全部/有效/即将过期/已过期/未签发）+ 根域名筛选。
       计数与列表同源，切筛选不会让统计和表格对不上。 -->
  <div class="cert-filter">
    <Button
      v-for="f in STATUS_FILTERS"
      :key="f.key"
      size="small"
      :type="certFilter === f.key ? 'primary' : 'default'"
      class="cert-chip"
      @click="certFilter = f.key"
    >
      {{ f.label }}
      <b class="cert-chip-n">{{ statusCount(f.key) }}</b>
    </Button>
    <Select
      v-model:value="rootFilter"
      :options="rootOptions"
      size="small"
      style="width: 180px; margin-left: 8px"
    />
  </div>

  <Table
    :columns="columns"
    :data-source="filteredSubdomains"
    :row-key="(r: SubdomainCert) => r.fqdn"
    :loading="false"
    :scroll="{ x: 1050 }"
    :pagination="{ pageSize: 20 }"
    size="small"
  />
  <Empty
    v-if="subdomains.length === 0"
    description="暂无二级域名"
    style="margin-top: 24px"
  />

  <!-- 查看证书:公钥/私钥分两个多行文本框 -->
  <Modal :open="detailShow" title="查看证书" :width="820" :footer="null" @cancel="detailShow = false">
    <template v-if="detail">
      <div style="display: flex; flex-direction: column; gap: 12px">
        <div>
          <div class="dw-label">公钥（fullchain.pem）</div>
          <Input.TextArea :value="detail.publicKey" :rows="9" readonly style="font-family: monospace; font-size: 11px" />
        </div>
        <div>
          <div class="dw-label">私钥（key.pem）</div>
          <Input.TextArea :value="detail.privateKey" :rows="9" readonly style="font-family: monospace; font-size: 11px" />
        </div>
        <div style="font-size: 12px; color: #888">
          {{ detail.fqdn }} · {{ friendlyIssuer(detail.issuer) }} · 有效期 {{ formatTime(detail.notBefore) }} ~ {{ formatTime(detail.notAfter) }}
        </div>
        <div style="font-size: 12px; color: #888; word-break: break-all">序列号：{{ detail.serial || '-' }}</div>
      </div>
    </template>
  </Modal>

  <!-- 证书操作日志(按域名过滤) -->
  <Modal :open="logsShow" :title="`证书操作日志${logTarget ? ' — ' + logTarget : ''}`" :width="980" :footer="null" @cancel="logsShow = false">
    <Table :columns="logColumns" :data-source="logs" :row-key="(r: any) => r.id" :loading="logsLoading" :pagination="{ pageSize: 20 }" size="small" />
  </Modal>

  <!-- acme.sh 原始输出 -->
  <Modal :open="logShow" title="acme.sh 输出（申请原始日志）" :width="860" :confirm-loading="logLoading" :footer="null" @cancel="logShow = false">
    <Input.TextArea :value="logDetail" :rows="22" readonly style="font-family: monospace; font-size: 11px" />
  </Modal>

  <!-- Caddy 来源 → 复用网关服务编辑抽屉 -->
  <AppFormModal
    :show="svcEditorShow"
    :edit-target="svcEditTarget"
    @update:show="onSvcEditorShow"
    @saved="onSvcSaved"
  />

  <!-- Docker 来源 → 复用编排项目编辑抽屉(定位到对应服务 tab) -->
  <ComposeEditorModal
    v-model:show="composeEditorShow"
    :project="composeProject"
    :service="composeService"
    @saved="onComposeSaved"
  />

  <!-- 日志（地图视图 / 文本视图） -->
  <RunLogModal
    v-if="mapLog"
    :open="!!mapLog"
    :chain="mapLog.chain"
    :trigger="mapLog.trigger"
    :action="mapLog.action"
    :state="mapLog.state"
    :started-at="mapLog.startedAt"
    :ended-at="mapLog.endedAt"
    :evidence="mapLog.evidence"
    @close="mapLog = null"
  />
</template>

<style scoped>
.cert-filter {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: 10px;
}
.cert-chip-n {
  margin-left: 4px;
  opacity: 0.75;
}
</style>
