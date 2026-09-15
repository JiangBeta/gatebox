<script setup lang="ts">
import { ref, watch } from 'vue'
import { Modal, Form, Input, Select, message } from 'ant-design-vue'
import { createPort, updatePort, type PortBinding } from '../api/gateway'

const props = defineProps<{ show: boolean; editTarget?: PortBinding | null }>()
const emit = defineEmits<{ 'update:show': [boolean]; saved: [PortBinding] }>()

const [messageApi, contextHolder] = message.useMessage()
const saving = ref(false)
const isNew = ref(true)
const form = ref({ protocol: '', description: '', ports: [] as (number | string)[], network: 'tcp', enabled: true })

// http/https 恒 tcp;非 http 协议可选 tcp/udp/both(由支持该协议类别的扩展代理)。
const isHttpProto = (p: string) => p === 'http' || p === 'https'
const networkOptions = [
  { value: 'tcp', label: 'TCP' },
  { value: 'udp', label: 'UDP' },
  { value: 'both', label: 'TCP & UDP' },
]

// parsePorts 归一化 tags 输入(字符串/数字)为升序端口;校验 1~65535 且无重复。
function parsePorts(list: (number | string)[]): { ok: boolean; ports: number[]; msg: string } {
  const ports: number[] = []
  for (const v of list || []) {
    if (v === '' || v === null || v === undefined) continue
    const n = Number(String(v).trim())
    if (!Number.isInteger(n) || n < 1 || n > 65535) return { ok: false, ports: [], msg: `实际端口 ${v} 非法(需 1~65535)` }
    if (ports.includes(n)) return { ok: false, ports: [], msg: `实际端口 ${n} 重复` }
    ports.push(n)
  }
  if (ports.length === 0) return { ok: false, ports: [], msg: '请填写至少一个实际端口' }
  return { ok: true, ports: ports.sort((a, b) => a - b), msg: '' }
}

function reset() {
  const t = props.editTarget
  if (t) {
    isNew.value = false
    form.value = { protocol: t.protocol, description: t.description, ports: [...t.ports], network: t.network || 'tcp', enabled: t.enabled }
  } else {
    isNew.value = true
    form.value = { protocol: '', description: '', ports: [], network: 'tcp', enabled: true }
  }
}
watch(() => props.show, (s) => { if (s) reset() })

async function save() {
  const f = form.value
  if (isNew.value && !/^[a-z][a-z0-9]{0,31}$/.test(f.protocol)) {
    messageApi.warning('协议名需为小写字母/数字')
    return
  }
  const parsed = parsePorts(f.ports)
  if (!parsed.ok) {
    messageApi.warning(parsed.msg)
    return
  }
  saving.value = true
  try {
    let saved: PortBinding
    if (isNew.value) {
      saved = await createPort({ protocol: f.protocol, description: f.description, ports: parsed.ports, network: f.network })
    } else {
      saved = await updatePort(f.protocol, { description: f.description, ports: parsed.ports, network: f.network, enabled: f.enabled })
    }
    messageApi.success(isNew.value ? '已新增' : '已保存')
    emit('saved', saved)
    emit('update:show', false)
  } catch (e: any) {
    messageApi.error(e.message)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <contextHolder />
  <Modal
    :open="show"
    :title="isNew ? '添加协议端口' : `编辑 ${form.protocol}`"
    :confirm-loading="saving"
    @ok="save"
    @cancel="emit('update:show', false)"
  >
    <Form layout="vertical" :colon="false">
      <Form.Item label="协议">
        <Input v-model:value="form.protocol" :disabled="!isNew" placeholder="http / https / mysql / ..." />
      </Form.Item>
      <Form.Item label="说明">
        <Input v-model:value="form.description" placeholder="HTTP / MySQL ..." />
      </Form.Item>
      <Form.Item label="网络（L4 传输层；HTTP/HTTPS 恒为 TCP）">
        <Select
          v-model:value="form.network"
          :options="networkOptions"
          :disabled="isHttpProto(form.protocol)"
          style="width: 180px"
        />
      </Form.Item>
      <Form.Item label="实际端口（回车/逗号分隔，可多个）">
        <Select
          v-model:value="form.ports"
          mode="tags"
          :open="false"
          placeholder="如 443 / 9443"
          :token-separators="[',', ' ']"
          style="width: 100%"
        />
      </Form.Item>
    </Form>
  </Modal>
</template>
