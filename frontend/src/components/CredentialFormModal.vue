<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { Drawer, Form, FormItem, Input, Select, Button, Space, message } from 'ant-design-vue'
import {
  createCredential,
  updateCredential,
  verifyCredential,
  listProviders,
  type DNSCredential,
  type DNSProviderSpec,
} from '../api/credentials'

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

// 供应商与字段 schema 全部来自后端注册表（不再前端硬编码，ADR-039 §5）。
const providers = ref<DNSProviderSpec[]>([])
const providerOptions = computed(() => providers.value.map((p) => ({ label: p.label || p.id, value: p.id })))
const fields = computed(() => providers.value.find((p) => p.id === form.value.provider)?.fields || [])

const form = ref({ provider: '', name: '', fields: {} as Record<string, string> })

watch(
  () => props.show,
  async (s) => {
    if (!s) return
    if (!providers.value.length) {
      try {
        providers.value = await listProviders()
      } catch (e: any) {
        messageApi.error(`加载供应商失败：${e.message}`)
      }
    }
    const first = providers.value[0]?.id || ''
    form.value = props.editing
      ? { provider: props.editing.provider, name: props.editing.name, fields: { ...props.editing.fields } }
      : { provider: first, name: '', fields: {} }
    verifyResult.value = ''
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
      <FormItem v-for="f in fields" :key="f.name" :label="f.label || f.name" :required="f.required">
        <Input
          v-model:value="form.fields[f.name]"
          :type="f.secret || f.type === 'password' ? 'password' : 'text'"
          placeholder="请输入"
        />
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
