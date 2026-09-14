<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { Table, Button, Modal, Form, Input, InputNumber, Select, Tag, Popconfirm, Alert, message } from 'ant-design-vue'
import { PlusOutlined, EditOutlined, PlayCircleOutlined, StopOutlined, CaretRightOutlined, DeleteOutlined } from '@ant-design/icons-vue'
import { listPorts, createPort, updatePort, deletePort, restartPorts, type PortBinding } from '../../api/gateway'

const [messageApi, contextHolder] = message.useMessage()

const rows = ref<PortBinding[]>([])
const loading = ref(false)
const restarting = ref(false)
const toggling = ref<Record<string, boolean>>({})

// 编辑/新建弹层
const modalShow = ref(false)
const modalIsNew = ref(false)
const saving = ref(false)
const form = ref({ protocol: '', description: '', defaultPort: 0, ports: [] as number[], enabled: true })

async function load() {
  loading.value = true
  try {
    rows.value = await listPorts()
  } catch (e: any) {
    messageApi.error('读取端口失败 — ' + e.message)
  } finally {
    loading.value = false
  }
}
onMounted(load)

function openNew() {
  modalIsNew.value = true
  form.value = { protocol: '', description: '', defaultPort: 0, ports: [], enabled: true }
  modalShow.value = true
}

function openEdit(row: PortBinding) {
  modalIsNew.value = false
  form.value = { protocol: row.protocol, description: row.description, defaultPort: row.defaultPort, ports: [...row.ports], enabled: row.enabled }
  modalShow.value = true
}

function validatePorts(list: number[]): boolean {
  return list.length > 0 && list.every((n) => Number.isInteger(n) && n > 0 && n <= 65535)
}

async function save() {
  const f = form.value
  if (!/^[a-z][a-z0-9]{0,31}$/.test(f.protocol)) {
    messageApi.warning('协议名需为小写字母/数字')
    return
  }
  if (f.defaultPort <= 0 || f.defaultPort > 65535) {
    messageApi.warning('默认端口非法')
    return
  }
  if (!validatePorts(f.ports)) {
    messageApi.warning('请填写至少一个 1~65535 的实际端口')
    return
  }
  saving.value = true
  try {
    if (modalIsNew.value) {
      await createPort(f)
      messageApi.success('已新增')
    } else {
      await updatePort(f.protocol, f)
      messageApi.success('已保存')
    }
    modalShow.value = false
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  } finally {
    saving.value = false
  }
}

async function toggle(row: PortBinding) {
  toggling.value = { ...toggling.value, [row.protocol]: true }
  try {
    await updatePort(row.protocol, { description: row.description, defaultPort: row.defaultPort, ports: row.ports, enabled: !row.enabled })
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  } finally {
    const next = { ...toggling.value }
    delete next[row.protocol]
    toggling.value = next
  }
}

async function doRestart() {
  restarting.value = true
  try {
    await restartPorts()
    messageApi.success('已按当前端口配置重新加载 Caddy')
  } catch (e: any) {
    messageApi.error('重启失败 — ' + e.message)
  } finally {
    restarting.value = false
  }
}

async function doDelete(row: PortBinding) {
  try {
    await deletePort(row.protocol)
    messageApi.success(`已删除 ${row.protocol}`)
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

const columns = [
  { title: '协议', dataIndex: 'protocol', key: 'protocol' },
  { title: '说明', dataIndex: 'description', key: 'description' },
  { title: '默认端口', dataIndex: 'defaultPort', key: 'defaultPort', width: 110 },
  {
    title: '实际端口',
    dataIndex: 'ports',
    key: 'ports',
    customRender: ({ record }: { record: PortBinding }) =>
      h('div', { style: 'display:flex;gap:6px;flex-wrap:wrap' }, (record.ports || []).map((p) =>
        h(Tag, { color: record.enabled ? 'blue' : 'default', size: 'small' }, { default: () => String(p) }),
      )),
  },
  {
    title: '操作',
    key: 'actions',
    width: 220,
    customRender: ({ record }: { record: PortBinding }) =>
      h('div', { style: 'display:flex;gap:6px' }, [
        record.enabled
          ? h(Popconfirm, { title: `停用 ${record.protocol}？`, onConfirm: () => toggle(record) }, { default: () => h(Button, { size: 'small', loading: toggling.value[record.protocol] }, { default: () => h(StopOutlined) }) })
          : h(Popconfirm, { title: `启用 ${record.protocol}？`, onConfirm: () => toggle(record) }, { default: () => h(Button, { size: 'small', type: 'primary', ghost: true, loading: toggling.value[record.protocol] }, { default: () => h(CaretRightOutlined) }) }),
        h(Button, { size: 'small', onClick: () => openEdit(record) }, { default: () => h(EditOutlined) }),
        h(Button, { size: 'small', type: 'primary', ghost: true, onClick: doRestart }, { default: () => h(PlayCircleOutlined) }),
        record.builtin
          ? h(Tag, { size: 'small', color: 'default' }, { default: () => '内置' })
          : h(Popconfirm, { title: `删除 ${record.protocol}？`, okText: '删除', okButtonProps: { danger: true }, onConfirm: () => doDelete(record) }, { default: () => h(Button, { size: 'small', danger: true }, { default: () => h(DeleteOutlined) }) }),
      ]),
  },
]
</script>

<template>
  <div style="max-width: 960px; padding: 4px 8px">
    <contextHolder />
    <Alert
      type="info"
      show-icon
      :style="{ marginBottom: '12px' }"
      message="一个对外协议可对应多个实际端口；HTTP/HTTPS 为系统默认项不可删除，其余可增删改"
      description="完成修改后点「重启」即刻按新端口重新加载 Caddy，无需重启进程。"
    />
    <div style="display: flex; justify-content: flex-end; gap: 8px; margin-bottom: 10px">
      <Button :loading="restarting" @click="doRestart">重启</Button>
      <Button type="primary" :icon="h(PlusOutlined)" @click="openNew">+ 添加协议</Button>
    </div>
    <Table
      :columns="columns"
      :data-source="rows"
      :loading="loading"
      :row-key="(r: PortBinding) => r.protocol"
      :pagination="false"
      size="small"
    />

    <Modal :open="modalShow" :title="modalIsNew ? '添加协议端口' : `编辑 ${form.protocol}`" :confirm-loading="saving" @ok="save" @cancel="modalShow = false">
      <Form layout="vertical">
        <Form.Item label="协议">
          <Input v-model:value="form.protocol" :disabled="!modalIsNew" placeholder="http / https / mysql / ..." />
        </Form.Item>
        <Form.Item label="说明">
          <Input v-model:value="form.description" placeholder="HTTP / MySQL ..." />
        </Form.Item>
        <Form.Item label="默认端口">
          <InputNumber v-model:value="form.defaultPort" :min="1" :max="65535" style="width: 160px" />
        </Form.Item>
        <Form.Item label="实际端口（回车/逗号分隔新增，可多个）">
          <Select v-model:value="form.ports" mode="tags" :open="false" placeholder="如 443 / 9443" :token-separators="[',', ' ']" style="max-width: 420px" />
        </Form.Item>
      </Form>
    </Modal>
  </div>
</template>