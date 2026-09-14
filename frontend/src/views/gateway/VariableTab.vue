<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  Button, Table, Drawer, Form, Input, Popconfirm, Tag, Tooltip, message,
} from 'ant-design-vue'
import { EditOutlined, DeleteOutlined } from '@ant-design/icons-vue'
import { listVariables, createVariable, updateVariable, deleteVariable, type Variable } from '../../api/gateway'

const [messageApi, contextHolder] = message.useMessage()
const userVars = ref<Variable[]>([])
const loading = ref(false)

const showAdd = ref(false)
const drafts = ref<{ key: string; value: string; description: string }[]>([])

const showEdit = ref(false)
const editing = ref<Variable | null>(null)
const editForm = ref({ value: '', description: '' })

const builtinVars = [
  { key: 'GB_APP', value: '(运行时填充)', description: '当前应用名称' },
  { key: 'GB_SERVICE', value: '(运行时填充)', description: '当前服务名称' },
  { key: 'GB_STATIC_ROOT', value: '(运行时填充)', description: '静态文件根目录' },
  { key: 'GB_HOST_PORT', value: '(运行时填充)', description: '主机端口' },
]

const allVars = computed(() => {
  const builtin = builtinVars.map((v) => ({ ...v, createdAt: '', _builtin: true }))
  const user = userVars.value.map((v) => ({ ...v, _builtin: false }))
  return [...builtin, ...user]
})

function openAdd() {
  drafts.value = []
  addDraft()
  showAdd.value = true
}

function addDraft() {
  drafts.value.push({ key: '', value: '', description: '' })
}

function removeDraft(i: number) {
  drafts.value.splice(i, 1)
}

async function saveAdd() {
  const rows = drafts.value.map((r) => ({ ...r, key: r.key.trim() })).filter((r) => r.key !== '' || r.value !== '' || r.description !== '')
  if (rows.length === 0) return messageApi.warning('请至少填写一个变量')
  for (const r of rows) {
    if (!r.key) return messageApi.warning('变量名不能为空')
    if (r.key.startsWith('GB_')) return messageApi.warning(`「${r.key}」不能以 GB_ 开头(保留前缀)`)
  }
  try {
    for (const r of rows) {
      await createVariable(r.key, r.value, r.description)
    }
    messageApi.success(`已创建 ${rows.length} 个变量`)
    showAdd.value = false
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

function openEdit(row: any) {
  if (row._builtin) return
  editing.value = row
  editForm.value = { value: row.value, description: row.description || '' }
  showEdit.value = true
}

async function saveEdit() {
  if (!editing.value) return
  try {
    await updateVariable(editing.value.key, editForm.value.value, editForm.value.description)
    messageApi.success('已更新')
    showEdit.value = false
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

async function doDelete(row: any) {
  if (row._builtin) return
  try {
    await deleteVariable(row.key)
    messageApi.success('已删除')
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

/** 操作按钮:icon-only(不同颜色) + hover Tooltip 显示文字,与代理页/片段页风格统一。 */
function actionBtn(icon: any, color: string, title: string, onClick?: () => void) {
  return h(
    Tooltip,
    { title, mouseEnterDelay: 0.3 },
    {
      default: () =>
        h(
          Button,
          { size: 'small', type: 'text', onClick },
          { default: () => h(icon, { style: { fontSize: '14px', color } }) },
        ),
    },
  )
}

const columns = [
  {
    title: '变量名',
    key: 'key',
    customRender: ({ record }: { record: any }) => h(Tag, { size: 'small' }, { default: () => record.key }),
  },
  { title: '值', dataIndex: 'value', key: 'value' },
  { title: '说明', dataIndex: 'description', key: 'description' },
  {
    title: '来源',
    key: 'builtin',
    width: 90,
    customRender: ({ record }: { record: any }) => record._builtin
      ? h(Tag, { color: 'processing', size: 'small' }, { default: () => '系统' })
      : h(Tag, { color: 'success', size: 'small' }, { default: () => '自定义' }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    customRender: ({ record }: { record: any }) => {
      if (record._builtin) return h('span', { style: 'color: #999; font-size: 12px' }, '只读')
      return h('div', { style: 'display:flex; justify-content:flex-end; gap:2px' }, [
        actionBtn(EditOutlined, '#722ed1', '修改', () => openEdit(record)),
        h(
          Popconfirm,
          { title: '确认删除该变量？', onConfirm: () => doDelete(record) },
          {
            default: () =>
              h(
                Button,
                { size: 'small', type: 'text', danger: true },
                { default: () => h(DeleteOutlined, { style: { fontSize: '14px' } }) },
              ),
          },
        ),
      ])
    },
  },
]

async function load() {
  loading.value = true
  try {
    userVars.value = await listVariables()
  } catch (e: any) {
    messageApi.error(e.message)
  } finally {
    loading.value = false
  }
}

onMounted(load)

defineExpose({ openAdd })
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; justify-content: space-between; align-items: center">
    <span style="color: #888; font-size: 13px">用户自定义变量，用于 Caddy 片段中，引用写法 &lt;%KEY%&gt;</span>
    <Button type="primary" @click="openAdd">+ 添加变量</Button>
  </div>
  <Table :columns="columns" :data-source="allVars" :loading="loading" :row-key="(r: any) => r.key" />

  <!-- 批量添加变量(草稿表) -->
  <Drawer :open="showAdd" placement="right" :width="720" @close="showAdd = false">
    <template #title>
      <span class="dw-drawer-title">添加变量</span>
    </template>
      <div style="display: flex; justify-content: flex-end; margin-bottom: 8px">
        <Button @click="addDraft">+ 添加变量</Button>
      </div>
      <div style="display: flex; gap: 8px; align-items: center; padding: 0 4px 8px;">
        <span class="dw-label" style="flex: 1; margin-bottom: 0">变量名<span class="dw-required">*</span></span>
        <span class="dw-label" style="flex: 1; margin-bottom: 0">值<span class="dw-required">*</span></span>
        <span class="dw-label" style="flex: 1.4; margin-bottom: 0">说明</span>
        <span style="width: 32px"></span>
      </div>
      <div v-for="(r, i) in drafts" :key="i" style="display: flex; gap: 8px; align-items: center; margin-bottom: 8px">
        <Input v-model:value="r.key" placeholder="如 BACKEND_IP" style="flex: 1" @update:value="(val: string) => { r.key = val.toUpperCase() }" />
        <Input v-model:value="r.value" placeholder="如 192.168.1.10" style="flex: 1" />
        <Input v-model:value="r.description" placeholder="该变量的作用说明" style="flex: 1.4" />
        <Button size="small" type="text" danger @click="removeDraft(i)">
          <template #icon><DeleteOutlined /></template>
        </Button>
      </div>
    <template #footer>
      <div class="dw-footer">
        <Button @click="showAdd = false">取消</Button>
        <Button type="primary" @click="saveAdd">确定</Button>
      </div>
    </template>
  </Drawer>

  <!-- 修改单个变量 -->
  <Drawer :open="showEdit" placement="right" :width="480" @close="showEdit = false">
    <template #title>
      <span class="dw-drawer-title">修改变量</span>
    </template>
      <Form layout="vertical">
        <Form.Item label="变量名">
          <Input :value="editing?.key" disabled placeholder="如 BACKEND_IP" />
        </Form.Item>
        <Form.Item label="值">
          <Input v-model:value="editForm.value" placeholder="如 192.168.1.10" />
        </Form.Item>
        <Form.Item label="说明">
          <Input v-model:value="editForm.description" placeholder="该变量的作用说明" />
        </Form.Item>
      </Form>
    <template #footer>
      <div class="dw-footer">
        <Button @click="showEdit = false">取消</Button>
        <Button type="primary" @click="saveEdit">确定</Button>
      </div>
    </template>
  </Drawer>
</template>
