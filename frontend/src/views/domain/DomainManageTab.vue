<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  Button,
  Drawer,
  Form,
  FormItem,
  Input,
  Popconfirm,
  Select,
  Table,
  message,
} from 'ant-design-vue'
import { listDomains, createDomain, updateDomain, deleteDomain, type Domain } from '../../api/domains'
import { listCredentials, type DNSCredential } from '../../api/credentials'
import CredentialFormModal from '../../components/CredentialFormModal.vue'

const [messageApi, contextHolder] = message.useMessage()
const domains = ref<Domain[]>([])
const credentials = ref<DNSCredential[]>([])
const showModal = ref(false)
const editing = ref<Domain | null>(null)
const form = ref({ name: '', credentialId: '' })

const showCredModal = ref(false)

function credName(id: string) {
  return credentials.value.find((c) => c.id === id)?.name || '-'
}

function formatTime(s: string) {
  return s.replace('T', ' ').slice(0, 19)
}

const columns = [
  { title: '域名', dataIndex: 'name', key: 'name' },
  {
    title: '凭证',
    dataIndex: 'credentialId',
    key: 'credentialId',
    customRender: ({ record }: { record: any }) => credName(record.credentialId),
  },
  {
    title: '创建时间',
    dataIndex: 'createdAt',
    key: 'createdAt',
    customRender: ({ record }: { record: any }) => formatTime(record.createdAt),
  },
  {
    title: '操作',
    key: 'actions',
    customRender: ({ record }: { record: any }) =>
      h('div', [
        h(Button, { size: 'small', onClick: () => openEdit(record) }, { default: () => '编辑' }),
        h(
          Popconfirm,
          { title: '确认删除该域名?', onConfirm: () => doDelete(record.id) },
          {
            default: () =>
              h(Button, { size: 'small', danger: true, style: 'margin-left: 8px' }, { default: () => '删除' }),
          },
        ),
      ]),
  },
]

function openAdd() {
  editing.value = null
  form.value = { name: '', credentialId: '' }
  showModal.value = true
}

function openEdit(d: Domain) {
  editing.value = d
  form.value = { name: d.name, credentialId: d.credentialId }
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

async function load() {
  domains.value = await listDomains()
  credentials.value = await listCredentials()
}

async function loadCredentials() {
  credentials.value = await listCredentials()
}
onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; justify-content: flex-end">
    <Button type="primary" @click="openAdd">+ 添加域名</Button>
  </div>
  <Table :columns="columns" :data-source="domains" />

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
