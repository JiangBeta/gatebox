<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { useRouter } from 'vue-router'
import {
  Button, Card, Col, Drawer, Form, Input, Popconfirm, Row, Select, Statistic, Table, Tag, Tooltip, message,
} from 'ant-design-vue'
import { EditOutlined, DeleteOutlined } from '@ant-design/icons-vue'
import {
  createDomain, updateDomain, deleteDomain, overview,
  type DomainOverview, type OverviewResponse,
} from '../../api/domains'
import { listCredentials, type DNSCredential } from '../../api/credentials'
import CredentialFormModal from '../../components/CredentialFormModal.vue'

const router = useRouter()
const [messageApi, contextHolder] = message.useMessage()

const data = ref<OverviewResponse>({
  domainCount: 0,
  credentialCount: 0,
  providerCount: 0,
  certTotal: 0,
  certExpiringSoon: 0,
  certExpired: 0,
  domains: [],
})
const credentials = ref<DNSCredential[]>([])

const showModal = ref(false)
const editing = ref<DomainOverview | null>(null)
const form = ref({ name: '', credentialId: '' })
const showCredModal = ref(false)

/** 打开「域名管理」(即证书页,tab=cert)。 */
function goCert() {
  router.push({ path: '/domain', query: { tab: 'cert' } })
}

const certStatusMap: Record<string, { type: 'success' | 'warning' | 'error' | 'default'; label: string }> = {
  success: { type: 'success', label: '成功' },
  expiring: { type: 'warning', label: '将过期' },
  expired: { type: 'error', label: '已过期' },
  unissued: { type: 'default', label: '未申请' },
}

function formatTime(s: string) {
  if (!s) return '-'
  return s.replace('T', ' ').slice(0, 19)
}

/** 按凭证 ID 取名称(无匹配显示 '-')。 */
function credName(id: string) {
  if (!id) return '-'
  return credentials.value.find((c) => c.id === id)?.name || '-'
}

const columns = [
  { title: '域名', dataIndex: 'name', key: 'name' },
  {
    title: 'DNS 凭证',
    dataIndex: 'credentialId',
    key: 'credentialId',
    customRender: ({ record }: { record: DomainOverview }) => credName(record.credentialId),
  },
  {
    title: '证书',
    dataIndex: 'certStatus',
    key: 'certStatus',
    customRender: ({ record }: { record: any }) => {
      const m = certStatusMap[record.certStatus] || certStatusMap.unissued
      return h(Tag, { color: m.type, size: 'small' }, { default: () => m.label })
    },
  },
  {
    title: '二级域名',
    dataIndex: 'subdomainCount',
    key: 'subdomainCount',
    customRender: ({ record }: { record: any }) =>
      h('a', { onClick: goCert }, record.subdomainCount ?? 0),
  },
  {
    title: '创建时间',
    dataIndex: 'createdAt',
    key: 'createdAt',
    customRender: ({ record }: { record: any }) => formatTime(record.createdAt),
  },
  {
    title: '最近签发时间',
    dataIndex: 'lastIssuedAt',
    key: 'lastIssuedAt',
    customRender: ({ record }: { record: any }) => formatTime(record.lastIssuedAt),
  },
  {
    title: '操作',
    key: 'actions',
    width: 110,
    customRender: ({ record }: { record: DomainOverview }) =>
      h('div', { style: 'display:flex;gap:6px' }, [
        h(Tooltip, { title: '编辑' }, { default: () => h(Button, { size: 'small', onClick: () => openEdit(record) }, { default: () => h(EditOutlined) }) }),
        h(Tooltip, { title: '删除' }, { default: () => h(Popconfirm, { title: `确认删除域名 ${record.name}?`, onConfirm: () => doDelete(record.id) }, { default: () => h(Button, { size: 'small', danger: true }, { default: () => h(DeleteOutlined) }) }) }),
      ]),
  },
]

function openAdd() {
  editing.value = null
  form.value = { name: '', credentialId: '' }
  showModal.value = true
}

function openEdit(d: DomainOverview) {
  editing.value = d
  form.value = { name: d.name, credentialId: d.credentialId || '' }
  showModal.value = true
}

function openCredential() {
  showCredModal.value = true
}

function onCredentialSaved(c: DNSCredential) {
  loadCredentials()
  form.value.credentialId = c.id
}

async function save() {
  if (!form.value.name.trim()) {
    messageApi.warning('请输入域名')
    return
  }
  try {
    if (editing.value) {
      await updateDomain(editing.value.id, form.value)
      messageApi.success('已更新')
    } else {
      await createDomain(form.value)
      messageApi.success('已添加')
    }
    showModal.value = false
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

async function doDelete(id: string) {
  try {
    await deleteDomain(id)
    messageApi.success('已删除')
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

async function loadCredentials() {
  credentials.value = await listCredentials()
}

async function load() {
  data.value = await overview()
}

onMounted(() => {
  load()
  loadCredentials()
})
</script>

<template>
  <contextHolder />
  <Row :gutter="[12, 12]">
    <Col :span="4">
      <Card><Statistic title="域名数量" :value="data.domainCount" /></Card>
    </Col>
    <Col :span="4">
      <Card hoverable style="cursor: pointer" @click="goCert">
        <Statistic title="证书总数" :value="data.certTotal" />
      </Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="即将过期" :value="data.certExpiringSoon" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="已过期" :value="data.certExpired" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="DNS 供应商" :value="data.providerCount" /></Card>
    </Col>
    <Col :span="4">
      <Card><Statistic title="DNS 凭证" :value="data.credentialCount" /></Card>
    </Col>
  </Row>

  <div style="margin: 16px 0; display: flex; justify-content: flex-end">
    <Button type="primary" @click="openAdd">+ 添加域名</Button>
  </div>
  <Table :columns="columns" :data-source="data.domains" />

  <Drawer
    :open="showModal"
    placement="right"
    width="480"
    :z-index="2000"
    @close="showModal = false"
  >
    <template #title>
      <span class="dw-drawer-title">{{ editing ? '编辑域名' : '添加域名' }}</span>
    </template>
    <Form layout="vertical">
      <Form.Item label="域名" required>
        <Input v-model:value="form.name" placeholder="如 neob.cn" />
      </Form.Item>
      <Form.Item label="凭证">
        <div style="display: flex; gap: 8px; width: 100%">
          <Select
            v-model:value="form.credentialId"
            :options="credentials.map((c) => ({ label: c.name, value: c.id }))"
            placeholder="选择 DNS 凭证"
            allow-clear
            style="flex: 1"
          />
          <Button @click="openCredential">添加凭证</Button>
        </div>
      </Form.Item>
    </Form>
    <template #footer>
      <div class="dw-footer">
        <Button @click="showModal = false">取消</Button>
        <Button type="primary" @click="save">保存</Button>
      </div>
    </template>
  </Drawer>

  <CredentialFormModal v-model:show="showCredModal" :editing="null" :z-index="2100" @saved="onCredentialSaved" />
</template>
