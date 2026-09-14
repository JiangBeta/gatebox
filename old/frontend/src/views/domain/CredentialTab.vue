<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { Button, Popconfirm, Table, message } from 'ant-design-vue'
import { listCredentials, deleteCredential, type DNSCredential } from '../../api/credentials'
import CredentialFormModal from '../../components/CredentialFormModal.vue'

const [messageApi, contextHolder] = message.useMessage()
const credentials = ref<DNSCredential[]>([])
const showModal = ref(false)
const editing = ref<DNSCredential | null>(null)

const providerOptions = [
  { label: 'Cloudflare', value: 'cloudflare' },
  { label: 'DNSPod（dnspod.cn）', value: 'dnspod' },
  { label: 'Aliyun', value: 'aliyun' },
]

function providerLabel(p: string) {
  return providerOptions.find((o) => o.value === p)?.label || p
}

function formatTime(s: string) {
  return s.replace('T', ' ').slice(0, 19)
}

const columns = [
  { title: '凭证名称', dataIndex: 'name', key: 'name' },
  {
    title: '供应商',
    dataIndex: 'provider',
    key: 'provider',
    customRender: ({ record }: { record: any }) => providerLabel(record.provider),
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
          { title: '确认删除该凭证?', onConfirm: () => doDelete(record.id) },
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
  showModal.value = true
}

function openEdit(c: DNSCredential) {
  editing.value = c
  showModal.value = true
}

async function doDelete(id: string) {
  await deleteCredential(id)
  messageApi.success('已删除')
  await load()
}

async function load() {
  credentials.value = await listCredentials()
}
onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; justify-content: flex-end">
    <Button type="primary" @click="openAdd">+ 添加凭证</Button>
  </div>
  <Table :columns="columns" :data-source="credentials" />

  <CredentialFormModal v-model:show="showModal" :editing="editing" @saved="load" />
</template>
