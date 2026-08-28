<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { NDrawer, NForm, NFormItem, NInput, NSelect, NButton, NSpace, useMessage } from 'naive-ui'
import { createCredential, updateCredential, verifyCredential, type DNSCredential } from '../api/credentials'

const props = defineProps<{
  show: boolean
  editing: DNSCredential | null
  zIndex?: number
}>()
const emit = defineEmits<{
  'update:show': [boolean]
  saved: [DNSCredential]
}>()

const message = useMessage()
const verifyResult = ref('')

const providerOptions = [
  { label: 'Cloudflare', value: 'cloudflare' },
  { label: 'DNSPod（dnspod.cn）', value: 'dnspod' },
  { label: 'Aliyun', value: 'aliyun' },
]

const providerFields: Record<string, { key: string; label: string }[]> = {
  cloudflare: [{ key: 'token', label: 'API Token' }],
  dnspod: [
    { key: 'id', label: 'SecretId' },
    { key: 'token', label: 'SecretKey' },
  ],
  aliyun: [
    { key: 'accessKeyId', label: 'AccessKey ID' },
    { key: 'accessKeySecret', label: 'AccessKey Secret' },
  ],
}

const form = ref({ provider: 'cloudflare', name: '', fields: {} as Record<string, string> })
const fields = computed(() => providerFields[form.value.provider] || [])

watch(
  () => props.show,
  (s) => {
    if (s) {
      form.value = props.editing
        ? { provider: props.editing.provider, name: props.editing.name, fields: { ...props.editing.fields } }
        : { provider: 'cloudflare', name: '', fields: {} }
      verifyResult.value = ''
    }
  },
)

async function doVerify() {
  try {
    const r = await verifyCredential({ provider: form.value.provider, fields: form.value.fields })
    verifyResult.value = r.ok ? `通过：${r.message}` : `不通过：${r.message}`
  } catch (e: any) {
    verifyResult.value = `验证失败：${e.message}`
  }
}

async function save() {
  if (!form.value.name.trim()) {
    message.warning('请输入凭证名称')
    return
  }
  try {
    const payload = { provider: form.value.provider, name: form.value.name, fields: form.value.fields }
    const saved = props.editing
      ? await updateCredential(props.editing.id, payload)
      : await createCredential(payload)
    message.success(props.editing ? '已更新' : '已添加')
    emit('saved', saved)
    emit('update:show', false)
  } catch (e: any) {
    message.error(e.message)
  }
}
</script>

<template>
  <n-drawer
    :show="show"
    placement="right"
    width="min(480px, 100vw)"
    :z-index="zIndex"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div style="display: flex; flex-direction: column; height: 100%">
      <div style="padding: 14px 24px; border-bottom: 1px solid #eee; font-size: 16px; font-weight: 600; flex-shrink: 0">{{ editing ? '编辑凭证' : '添加凭证' }}</div>
      <div style="flex: 1; overflow: auto; padding: 16px 24px">
        <n-form label-placement="top">
      <n-form-item label="供应商">
        <n-select v-model:value="form.provider" :options="providerOptions" :disabled="!!editing" />
      </n-form-item>
      <n-form-item label="凭证名称">
        <n-input v-model:value="form.name" placeholder="一般为域名" />
      </n-form-item>
      <n-form-item v-for="f in fields" :key="f.key" :label="f.label">
        <n-input v-model:value="form.fields[f.key]" type="password" show-password-on="click" placeholder="请输入" />
      </n-form-item>
      <div v-if="verifyResult" :style="{ color: verifyResult.startsWith('通过') ? '#18a058' : '#d03050' }">
        {{ verifyResult }}
      </div>
    </n-form>
      </div>
      <div style="padding: 14px 24px; border-top: 1px solid #eee; display: flex; justify-content: flex-end; gap: 8px; flex-shrink: 0">
        <n-button @click="doVerify">验证</n-button>
        <n-button @click="emit('update:show', false)">取消</n-button>
        <n-button type="primary" @click="save">保存</n-button>
      </div>
    </div>
  </n-drawer>
</template>
