<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { Empty, Table, Button, Modal, Popconfirm, Input, Tooltip, Tag, Card, Space, message } from 'ant-design-vue'
import { EyeOutlined, ReloadOutlined, DeleteOutlined, FileTextOutlined } from '@ant-design/icons-vue'
import { caddyIcon, dockerIcon } from '../../utils/brandIcons'
import {
  getCertDetail, renewCert, deleteCert, listCertLogs, getCertLog, listSubdomains,
  getACMEEmail, setACMEEmail,
  type CertDetail, type CertLog, type SubdomainCert, type SubdomainSource,
} from '../../api/certificates'
import { listGroups, type GroupView, type ServiceItem } from '../../api/gateway'
import AppFormModal from '../../components/AppFormModal.vue'
import ComposeEditorModal from '../../components/ComposeEditorModal.vue'

const [messageApi, contextHolder] = message.useMessage()

const subdomains = ref<SubdomainCert[]>([])
const detail = ref<CertDetail | null>(null)
const detailShow = ref(false)
const detailLoading = ref(false)
const renewing = ref<Record<string, boolean>>({})

// ACME 注册邮箱(全局,内联管理)
const acmeEmail = ref('')
const emailSaving = ref(false)

// 来源点击 → 复用网关服务编辑抽屉 / 编排项目编辑抽屉
const svcEditorShow = ref(false)
const svcEditTarget = ref<{ service: ServiceItem; group: GroupView } | null>(null)
const composeEditorShow = ref(false)
const composeProject = ref<string | null>(null)
let groupsCache: GroupView[] | null = null

const logs = ref<CertLog[]>([])
const logsShow = ref(false)
const logsLoading = ref(false)
const logTarget = ref('')
const logDetail = ref('')
const logShow = ref(false)
const logLoading = ref(false)

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

function daysColor(d: number | null) {
  if (d === null) return undefined
  if (d <= 10) return 'red'
  if (d <= 30) return 'orange'
  return 'green'
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

// --- 来源点击 → 打开编辑抽屉 ---

function sourceTip(s: SubdomainSource) {
  if (s.type === 'docker') return `Docker 项目：${s.displayName || s.projectName || '-'}（点击编辑）`
  return `Caddy 服务：${s.appName ? s.appName + ' / ' : ''}${s.serviceName || s.serviceId || '-'}（点击编辑）`
}

async function openSource(s: SubdomainSource) {
  if (s.type === 'docker') {
    if (!s.projectName) {
      messageApi.warning('缺少项目名，无法打开编辑')
      return
    }
    composeProject.value = s.projectName
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

/** 来源列:Caddy/Docker 品牌图标(对齐网关页归属图标),点击打开对应编辑抽屉;文字 hover 呈现。 */
function sourceCell(record: SubdomainCert) {
  if (!record.sources?.length) return '-'
  return h(
    'div',
    { style: 'display:flex;gap:10px;flex-wrap:wrap;align-items:center' },
    record.sources.map((s) =>
      h(
        Tooltip,
        { title: sourceTip(s) },
        {
          default: () =>
            h(
              'span',
              {
                style: 'display:inline-flex;align-items:center;cursor:pointer;line-height:1',
                onClick: () => openSource(s),
              },
              [s.type === 'docker' ? dockerIcon(18) : caddyIcon(18)],
            ),
        },
      ),
    ),
  )
}

/** 操作列:仅有证书时可用。 */
function actionCell(record: SubdomainCert) {
  if (!record.hasCert) return '-'
  return h('div', { style: 'display:flex;gap:6px' }, [
    h(Tooltip, { title: '查看（公钥/私钥）' }, { default: () => h(Button, { size: 'small', onClick: () => view(record) }, { default: () => h(EyeOutlined) }) }),
    h(Tooltip, { title: '重新申请' }, { default: () => h(Popconfirm, { title: `重新申请 ${record.fqdn} 的证书？`, onConfirm: () => doRenew(record) }, { default: () => h(Button, { size: 'small', type: 'primary', ghost: true, loading: renewing.value[record.fqdn] }, { default: () => h(ReloadOutlined) }) }) }),
    h(Tooltip, { title: '证书日志' }, { default: () => h(Button, { size: 'small', onClick: () => openLogs(record) }, { default: () => h(FileTextOutlined) }) }),
    h(Tooltip, { title: '删除' }, { default: () => h(Popconfirm, { title: `删除 ${record.fqdn} 的证书？`, okText: '删除', okButtonProps: { danger: true }, onConfirm: () => doDelete(record) }, { default: () => h(Button, { size: 'small', danger: true }, { default: () => h(DeleteOutlined) }) }) }),
  ])
}

const columns = [
  { title: '二级域名', dataIndex: 'fqdn', key: 'fqdn' },
  {
    title: '来源',
    key: 'sources',
    width: 170,
    customRender: ({ record }: { record: SubdomainCert }) => sourceCell(record),
  },
  {
    title: '颁发机构',
    dataIndex: 'issuer',
    key: 'issuer',
    customRender: ({ record }: { record: SubdomainCert }) =>
      h(Tooltip, { title: `原始值：${record.issuer || '-'}` }, { default: () => (record.hasCert ? friendlyIssuer(record.issuer) : '-') }),
  },
  {
    title: '到期时间',
    dataIndex: 'notAfter',
    key: 'notAfter',
    customRender: ({ record }: { record: SubdomainCert }) => formatTime(record.notAfter),
  },
  {
    title: '有效时间',
    key: 'daysLeft',
    width: 110,
    customRender: ({ record }: { record: SubdomainCert }) => {
      const d = daysLeft(record.notAfter)
      if (d === null) return '-'
      return h(Tag, { color: daysColor(d), size: 'small' }, { default: () => `剩余 ${d} 天` })
    },
  },
  {
    title: '操作',
    key: 'actions',
    width: 220,
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
    width: 90,
    customRender: ({ record }: { record: CertLog }) =>
      record.logFile
        ? h('div', { style: 'display:flex;gap:6px' }, [
            h(Tooltip, { title: '查看 acme.sh 原始输出' }, { default: () => h(Button, { size: 'small', onClick: () => viewLog(record) }, { default: () => h(FileTextOutlined) }) }),
          ])
        : '-',
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

  <Table :columns="columns" :data-source="subdomains" :row-key="(r: any) => r.fqdn" :loading="false" />
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

  <!-- Docker 来源 → 复用编排项目编辑抽屉 -->
  <ComposeEditorModal
    v-model:show="composeEditorShow"
    :project="composeProject"
    @saved="onComposeSaved"
  />
</template>
