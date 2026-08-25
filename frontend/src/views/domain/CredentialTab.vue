<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { NButton, NDataTable, NPopconfirm, useMessage } from 'naive-ui'
import { listCredentials, deleteCredential, type DNSCredential } from '../../api/credentials'
import CredentialFormModal from '../../components/CredentialFormModal.vue'

const message = useMessage()
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
  { title: '凭证名称', key: 'name' },
  { title: '供应商', key: 'provider', render: (row: any) => providerLabel(row.provider) },
  { title: '创建时间', key: 'createdAt', render: (row: any) => formatTime(row.createdAt) },
  {
    title: '操作',
    key: 'actions',
    render: (row: any) =>
      h('div', [
        h(NButton, { size: 'small', onClick: () => openEdit(row) }, { default: () => '编辑' }),
        h(
          NPopconfirm,
          { onPositiveClick: () => doDelete(row.id) },
          {
            trigger: () =>
              h(NButton, { size: 'small', type: 'error', style: 'margin-left: 8px' }, { default: () => '删除' }),
            default: () => '确认删除该凭证?',
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
  message.success('已删除')
  await load()
}

async function load() {
  credentials.value = await listCredentials()
}
onMounted(load)
</script>

<template>
  <div style="margin-bottom: 16px; display: flex; justify-content: flex-end">
    <n-button type="primary" @click="openAdd">+ 添加凭证</n-button>
  </div>
  <n-data-table :columns="columns" :data="credentials" />

  <CredentialFormModal v-model:show="showModal" :editing="editing" @saved="load" />
</template>
