<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Drawer, Form, FormItem, Input, Select, Button, Space, message } from 'ant-design-vue'
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

const [messageApi, contextHolder] = message.useMessage()
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
    messageApi.warning('请输入凭证名称')
    return
  }
  try {
    const payload = { provider: form.value.provider, name: form.value.name, fields: form.value.fields }
    const saved = props.editing
      ? await updateCredential(props.editing.id, payload)
      : await createCredential(payload)
    messageApi.success(props.editing ? '已更新' : '已添加')
    emit('saved', saved)
    emit('update:show', false)
  } catch (e: any) {
    messageApi.error(e.message)
  }
}
</script>

<template>
  <contextHolder />
  <Drawer
    :open="show"
    placement="right"
    :width="480"
    :z-index="zIndex"
    @close="(v: boolean | MouseEvent) => emit('update:show', false)"
  >
    <template #title>
      <span class="dw-drawer-title">{{ editing ? '编辑凭证' : '添加凭证' }}</span>
    </template>
    <Form layout="vertical">
      <FormItem label="供应商">
        <Select v-model:value="form.provider" :options="providerOptions" :disabled="!!editing" />
      </FormItem>
      <FormItem label="凭证名称">
        <Input v-model:value="form.name" placeholder="一般为域名" />
      </FormItem>
      <FormItem v-for="f in fields" :key="f.key" :label="f.label">
        <Input v-model:value="form.fields[f.key]" type="password" placeholder="请输入" />
      </FormItem>
      <div v-if="verifyResult" :style="{ color: verifyResult.startsWith('通过') ? '#52c41a' : '#ff4d4f' }">
        {{ verifyResult }}
      </div>
    </Form>
    <template #footer>
      <div class="dw-footer">
        <Button @click="emit('update:show', false)">取消</Button>
        <Button @click="doVerify">验证</Button>
        <Button type="primary" @click="save">保存</Button>
      </div>
    </template>
  </Drawer>
</template>
