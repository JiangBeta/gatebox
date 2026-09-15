<script setup lang="ts">
import { ref, onMounted, h } from 'vue'
import { Table, Button, Tag, Popconfirm, Tooltip, message } from 'ant-design-vue'
import { PlusOutlined, EditOutlined, DeleteOutlined, StopOutlined, CaretRightOutlined } from '@ant-design/icons-vue'
import { listPorts, updatePort, deletePort, restartPorts, type PortBinding } from '../../api/gateway'
import PortFormModal from '../../components/PortFormModal.vue'

const [messageApi, contextHolder] = message.useMessage()

const rows = ref<PortBinding[]>([])
const loading = ref(false)
const restarting = ref(false)
const toggling = ref<Record<string, boolean>>({})

// 新建/编辑弹层(共享 PortFormModal)
const modalShow = ref(false)
const editTarget = ref<PortBinding | null>(null)

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
  editTarget.value = null
  modalShow.value = true
}

function openEdit(row: PortBinding) {
  editTarget.value = row
  modalShow.value = true
}

async function toggle(row: PortBinding) {
  toggling.value = { ...toggling.value, [row.protocol]: true }
  try {
    await updatePort(row.protocol, { description: row.description, ports: row.ports, enabled: !row.enabled })
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

/** 操作按钮:icon-only + hover Tooltip,与变量页/片段页风格统一。 */
function actionBtn(icon: any, color: string, title: string, onClick?: () => void, disabled = false) {
  return h(
    Tooltip,
    { title, mouseEnterDelay: 0.3 },
    {
      default: () =>
        h(
          Button,
          { size: 'small', type: 'text', disabled, onClick },
          { default: () => h(icon, { style: { fontSize: '14px', color } }) },
        ),
    },
  )
}

const columns = [
  {
    title: '协议',
    key: 'protocol',
    width: 110,
    customRender: ({ record }: { record: PortBinding }) => h(Tag, { size: 'small' }, { default: () => record.protocol }),
  },
  { title: '说明', dataIndex: 'description', key: 'description', ellipsis: true },
  {
    title: '网络',
    key: 'network',
    width: 100,
    customRender: ({ record }: { record: PortBinding }) =>
      h(Tag, { size: 'small' }, { default: () => ({ udp: 'UDP', both: 'TCP & UDP' } as Record<string, string>)[record.network || 'tcp'] || 'TCP' }),
  },
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
    title: '状态',
    key: 'status',
    width: 80,
    customRender: ({ record }: { record: PortBinding }) => record.enabled
      ? h(Tag, { color: 'success', size: 'small' }, { default: () => '启用' })
      : h(Tag, { color: 'default', size: 'small' }, { default: () => '停用' }),
  },
  {
    title: '来源',
    key: 'builtin',
    width: 80,
    customRender: ({ record }: { record: PortBinding }) => record.builtin
      ? h(Tag, { color: 'processing', size: 'small' }, { default: () => '内置' })
      : h(Tag, { color: 'success', size: 'small' }, { default: () => '自定义' }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 140,
    customRender: ({ record }: { record: PortBinding }) =>
      h('div', { style: 'display:flex; align-items:center; justify-content:flex-start; gap:2px; white-space:nowrap' }, [
        record.enabled
          ? actionBtn(StopOutlined, '#fa8c16', '停用', () => toggle(record), !!toggling.value[record.protocol])
          : actionBtn(CaretRightOutlined, '#52c41a', '启用', () => toggle(record), !!toggling.value[record.protocol]),
        actionBtn(EditOutlined, '#722ed1', '修改', () => openEdit(record)),
        record.builtin
          ? h('span', { style: 'color:#999;font-size:12px;white-space:nowrap;padding-left:4px' }, '只读')
          : h(
              Popconfirm,
              { title: `删除 ${record.protocol}？`, okText: '删除', okButtonProps: { danger: true }, onConfirm: () => doDelete(record) },
              {
                default: () =>
                  h(
                    Button,
                    { size: 'small', type: 'text', danger: true },
                    { default: () => h(DeleteOutlined, { style: { fontSize: '14px' } }) },
                  ),
              },
            ),
      ]),
  },
]
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; justify-content: space-between; align-items: center">
    <span style="color: #888; font-size: 13px">
      一个对外协议可对应多个实际端口（首个为主端口）；HTTP/HTTPS 为系统内置项不可删除。
      修改后点「重启」按新端口重新加载 Caddy，无需重启进程。
    </span>
    <div style="display: flex; gap: 8px">
      <Button :loading="restarting" @click="doRestart">重启</Button>
      <Button type="primary" :icon="h(PlusOutlined)" @click="openNew">+ 添加协议</Button>
    </div>
  </div>
  <Table
    :columns="columns"
    :data-source="rows"
    :loading="loading"
    :row-key="(r: PortBinding) => r.protocol"
    :pagination="false"
  />

  <PortFormModal v-model:show="modalShow" :edit-target="editTarget" @saved="load" />
</template>
