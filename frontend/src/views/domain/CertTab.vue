<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { Empty, Table, Button, Modal, Popconfirm, Input, Tooltip, Tag, message } from 'ant-design-vue'
import { EyeOutlined, ReloadOutlined, DeleteOutlined, FileTextOutlined } from '@ant-design/icons-vue'
import { listCertificates, getCertDetail, renewCert, deleteCert, listCertLogs, getCertLog, type Cert, type CertDetail, type CertLog } from '../../api/certificates'

const [messageApi, contextHolder] = message.useMessage()

const certs = ref<Cert[]>([])
const detail = ref<CertDetail | null>(null)
const detailShow = ref(false)
const detailLoading = ref(false)
const renewing = ref<Record<string, boolean>>({})

const logs = ref<CertLog[]>([])
const logsShow = ref(false)
const logsLoading = ref(false)
const logTarget = ref('')
const logDetail = ref('')
const logShow = ref(false)
const logLoading = ref(false)

function formatTime(s: string) {
  if (!s) return '-'
  return s.replace('T', ' ').slice(0, 19)
}

/** 颁发机构人类可读名称(Let's Encrypt 中间证书 CN 如 YE1/YE2)。
 * 未知时原样显示,title 里放原始值。 */
function friendlyIssuer(issuer: string) {
  const m: Record<string, string> = {
    YE1: "Let's Encrypt E1（ECC 中间证书）",
    YE2: "Let's Encrypt E2（ECC 中间证书）",
    R10: "Let's Encrypt R10（RSA 中间证书）",
    R11: "Let's Encrypt R11（RSA 中间证书）",
    R12: "Let's Encrypt R12（RSA 中间证书）",
    R13: "Let's Encrypt R13（RSA 中间证书）",
  }
  return m[issuer] || (issuer ? `颁发机构 ${issuer}` : '-')
}

function actionText(a: string) {
  return { ensure: '自动签发', renew: '重新申请', delete: '删除' }[a] || a
}

async function load() {
  try {
    certs.value = await listCertificates()
  } catch (e: any) {
    messageApi.error('读取证书列表失败 — ' + e.message)
  }
}
onMounted(load)

async function view(row: Cert) {
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

async function doRenew(row: Cert) {
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

async function doDelete(row: Cert) {
  try {
    await deleteCert(row.fqdn)
    messageApi.success(`${row.fqdn}:证书已删除`)
    await load()
  } catch (e: any) {
    messageApi.error(`${row.fqdn}:删除失败 — ${e.message}`)
  }
}

async function openLogs(row: Cert) {
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

const columns = [
  { title: '二级域名', dataIndex: 'fqdn', key: 'fqdn' },
  {
    title: '颁发机构',
    dataIndex: 'issuer',
    key: 'issuer',
    customRender: ({ record }: { record: any }) =>
      h(Tooltip, { title: `原始值：${record.issuer}` }, { default: () => record.issuer ? friendlyIssuer(record.issuer) : '-' }),
  },
  {
    title: '到期时间',
    dataIndex: 'notAfter',
    key: 'notAfter',
    customRender: ({ record }: { record: any }) => formatTime(record.notAfter),
  },
  { title: '序列号', dataIndex: 'serial', key: 'serial' },
  {
    title: '操作',
    key: 'actions',
    width: 220,
    customRender: ({ record }: { record: any }) =>
      h('div', { style: 'display:flex;gap:6px' }, [
        h(Tooltip, { title: '查看（公钥/私钥）' }, { default: () => h(Button, { size: 'small', onClick: () => view(record) }, { default: () => h(EyeOutlined) }) }),
        h(Tooltip, { title: '重新申请' }, { default: () => h(Popconfirm, { title: `重新申请 ${record.fqdn} 的证书？`, onConfirm: () => doRenew(record) }, { default: () => h(Button, { size: 'small', type: 'primary', ghost: true, loading: renewing.value[record.fqdn] }, { default: () => h(ReloadOutlined) }) }) }),
        h(Tooltip, { title: '证书日志' }, { default: () => h(Button, { size: 'small', onClick: () => openLogs(record) }, { default: () => h(FileTextOutlined) }) }),
        h(Tooltip, { title: '删除' }, { default: () => h(Popconfirm, { title: `删除 ${record.fqdn} 的证书？`, okText: '删除', okButtonProps: { danger: true }, onConfirm: () => doDelete(record) }, { default: () => h(Button, { size: 'small', danger: true }, { default: () => h(DeleteOutlined) }) }) }),
      ]),
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
  <Table :columns="columns" :data-source="certs" :row-key="(r: any) => r.fqdn" />
  <Empty
    v-if="certs.length === 0"
    description="暂无证书"
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
</template>