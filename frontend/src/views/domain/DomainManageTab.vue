<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import {
  NButton,
  NDataTable,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NPopconfirm,
  useMessage,
} from 'naive-ui'
import { listDomains, createDomain, updateDomain, deleteDomain, type Domain } from '../../api/domains'
import { listCredentials, type DNSCredential } from '../../api/credentials'
import CredentialFormModal from '../../components/CredentialFormModal.vue'

const message = useMessage()
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
  { title: '域名', key: 'name' },
  { title: '凭证', key: 'credentialId', render: (row: any) => credName(row.credentialId) },
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
            default: () => '确认删除该域名?',
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

// 在添加域名弹层里,点「添加凭证」打开嵌套的凭证弹层
function openCredential() {
  showCredModal.value = true
}

// 嵌套凭证保存成功后:刷新凭证列表,并自动选中刚建的凭证
function onCredentialSaved(c: DNSCredential) {
  loadCredentials()
  form.value.credentialId = c.id
}

async function save() {
  if (!form.value.name.trim()) {
    message.warning('请输入域名')
    return
  }
  try {
    if (editing.value) {
      await updateDomain(editing.value.id, form.value)
      message.success('已更新')
    } else {
      await createDomain(form.value)
      message.success('已添加')
    }
    showModal.value = false
    await load()
  } catch (e: any) {
    message.error(e.message)
  }
}

async function doDelete(id: string) {
  await deleteDomain(id)
  message.success('已删除')
  await load()
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
  <div style="margin-bottom: 16px; display: flex; justify-content: flex-end">
    <n-button type="primary" @click="openAdd">+ 添加域名</n-button>
  </div>
  <n-data-table :columns="columns" :data="domains" />

  <n-modal
    v-model:show="showModal"
    preset="dialog"
    :title="editing ? '编辑域名' : '添加域名'"
    :z-index="2000"
  >
    <n-form label-placement="top">
      <n-form-item label="域名">
        <n-input v-model:value="form.name" placeholder="如 neob.cn" />
      </n-form-item>
      <n-form-item label="凭证">
        <div style="display: flex; gap: 8px; width: 100%">
          <n-select
            v-model:value="form.credentialId"
            :options="credentials.map((c) => ({ label: c.name, value: c.id }))"
            placeholder="选择 DNS 凭证"
            clearable
            style="flex: 1"
          />
          <n-button @click="openCredential">添加凭证</n-button>
        </div>
      </n-form-item>
    </n-form>
    <template #action>
      <n-button @click="showModal = false">取消</n-button>
      <n-button type="primary" @click="save">保存</n-button>
    </template>
  </n-modal>

  <CredentialFormModal v-model:show="showCredModal" :editing="null" :z-index="2100" @saved="onCredentialSaved" />
</template>
