<script setup lang="ts">
import { ref, computed, onMounted, h } from 'vue'
import {
  Alert, Button, Table, Drawer, Form, Input, Popconfirm, Tag, Tooltip, message,
} from 'ant-design-vue'
import { EditOutlined, DeleteOutlined } from '@ant-design/icons-vue'
import {
  listVariables, listSystemVariables, getVariableMigration, clearVariableMigration,
  createVariable, updateVariable, deleteVariable,
  type Variable, type SystemVariable, type VariableMigration,
} from '../../api/settings'

const [messageApi, contextHolder] = message.useMessage()
const systemVars = ref<SystemVariable[]>([])
const userVars = ref<Variable[]>([])
const migration = ref<VariableMigration | null>(null)
const loading = ref(false)

const gatewaySystemVars = computed(() => systemVars.value.filter((v) => v.context === 'gateway'))
const containerSystemVars = computed(() => systemVars.value.filter((v) => v.context === 'container'))
const migrationConflicts = computed(() => migration.value?.conflicts ?? [])

const showAdd = ref(false)
const drafts = ref<{ key: string; value: string; description: string }[]>([])

const showEdit = ref(false)
const editing = ref<Variable | null>(null)
const editForm = ref({ value: '', description: '' })

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

/** 键名规则:compose ${KEY} 与 Caddy <%KEY%> 的交集(ADR-035 §3)。 */
function validateKey(key: string): string | null {
  if (!key) return '变量名不能为空'
  if (key.startsWith('GB_')) return `「${key}」不能以 GB_ 开头（系统保留前缀）`
  if (!/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) return `「${key}」不合法：仅字母/数字/下划线，且不能以数字开头`
  return null
}

async function saveAdd() {
  const rows = drafts.value.map((r) => ({ ...r, key: r.key.trim() })).filter((r) => r.key !== '' || r.value !== '' || r.description !== '')
  if (rows.length === 0) return messageApi.warning('请至少填写一个变量')
  for (const r of rows) {
    const err = validateKey(r.key)
    if (err) return messageApi.warning(err)
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

function openEdit(row: Variable) {
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

async function doDelete(row: Variable) {
  try {
    await deleteVariable(row.key)
    messageApi.success('已删除')
    await load()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

async function dismissMigration() {
  try {
    await clearVariableMigration()
    migration.value = null
    messageApi.success('已关闭迁移提示')
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

const userColumns = [
  {
    title: '变量名',
    key: 'key',
    customRender: ({ record }: { record: Variable }) => h(Tag, { size: 'small' }, { default: () => record.key }),
  },
  {
    title: '值',
    dataIndex: 'value',
    key: 'value',
    customRender: ({ record }: { record: Variable }) => {
      const v = record.value
      if (v === undefined || v === null || v === '') {
        return h('span', { style: { color: '#bbb' } }, '—')
      }
      return h('code', { class: 'dw-inline-code' }, v)
    },
  },
  { title: '说明', dataIndex: 'description', key: 'description' },
  {
    title: '引用写法',
    key: 'ref',
    width: 190,
    customRender: ({ record }: { record: Variable }) =>
      h('span', { style: 'font-family: monospace; font-size: 12px; color: #666' }, `<%${record.key}%>  ·  \${${record.key}}`),
  },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    customRender: ({ record }: { record: Variable }) =>
      h('div', { style: 'display:flex; justify-content:flex-end; gap:2px' }, [
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
      ]),
  },
]

const systemColumns = [
  {
    title: '变量名',
    key: 'key',
    customRender: ({ record }: { record: SystemVariable }) => h(Tag, { size: 'small' }, { default: () => record.key }),
  },
  {
    title: '引用写法',
    dataIndex: 'placeholder',
    key: 'placeholder',
    width: 240,
    customRender: ({ record }: { record: SystemVariable }) =>
      h('code', { class: 'dw-inline-code' }, record.placeholder),
  },
  {
    title: '值/解析',
    key: 'value',
    customRender: ({ record }: { record: SystemVariable }) => {
      const v = record.value || record.placeholder
      if (!v) return h('span', { style: { color: '#aaa' } }, '（按上下文解析）')
      return h('code', { class: 'dw-inline-code' }, v)
    },
  },
  { title: '说明', dataIndex: 'description', key: 'description' },
]

async function load() {
  loading.value = true
  try {
    const [sys, user, mig] = await Promise.all([
      listSystemVariables().catch(() => []),
      listVariables(),
      getVariableMigration().catch(() => null),
    ])
    systemVars.value = sys
    userVars.value = user
    migration.value = mig
  } catch (e: any) {
    messageApi.error(e.message)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <div style="padding: 16px 24px">
    <Alert
      type="info"
      show-icon
      style="margin-bottom: 12px"
      message="变量由网关与容器共用：同一变量名、同一份值，引用写法随上下文而变。"
    >
      <template #description>
        <div style="font-size: 12px; line-height: 1.7">
          网关 Caddy 片段用 <code class="dw-inline-code">&lt;%KEY%&gt;</code>；容器 Docker Compose 用
          <code class="dw-inline-code">${KEY}</code>。键名仅允许字母/数字/下划线、不得以数字开头、不得以
          <code class="dw-inline-code">GB_</code> 开头。<br />
          注意：容器变量在「保存编排」时即已固化，修改变量仅对之后新建/重新保存的编排生效（网关侧则实时生效）。
        </div>
      </template>
    </Alert>

    <Alert
      v-if="migrationConflicts.length"
      type="warning"
      show-icon
      closable
      style="margin-bottom: 12px"
      message="变量迁移：有冲突项未自动导入"
      @close="dismissMigration"
    >
      <template #description>
        <div style="font-size: 12px; line-height: 1.7; margin-bottom: 6px">
          原有容器变量与新统一存储的变量存在同名异值或键名不合法，未自动导入。请按需在下方手动创建/调整后关闭本提示。
        </div>
        <ul style="margin: 0; padding-left: 18px; font-size: 12px">
          <li v-for="c in migrationConflicts" :key="c.reason + c.key">
            <code class="dw-inline-code">{{ c.key }}</code>
            <template v-if="c.reason === 'conflict'">
              — 同名异值（网关：<code class="dw-inline-code">{{ c.gatewayValue || '（空）' }}</code>，容器：<code class="dw-inline-code">{{ c.containerValue || '（空）' }}</code>）
            </template>
            <template v-else> — 键名不合法（容器值：<code class="dw-inline-code">{{ c.containerValue || '（空）' }}</code>）</template>
          </li>
        </ul>
      </template>
    </Alert>

    <!-- 用户变量 -->
    <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 8px">
      <span class="dw-section" style="margin-bottom: 0">用户变量</span>
      <Button type="primary" @click="openAdd">+ 添加变量</Button>
    </div>
    <Table :columns="userColumns" :data-source="userVars" :loading="loading" :row-key="(r: Variable) => r.key" size="small" />

    <!-- 系统变量 -->
    <div class="dw-section" style="margin: 20px 0 8px">系统变量（只读）</div>
    <div style="font-size: 12px; color: #888; margin-bottom: 8px">网关内置</div>
    <Table :columns="systemColumns" :data-source="gatewaySystemVars" :loading="loading" :row-key="(r: SystemVariable) => r.key" size="small" :pagination="false" />
    <div style="font-size: 12px; color: #888; margin: 12px 0 8px">容器内置</div>
    <Table :columns="systemColumns" :data-source="containerSystemVars" :loading="loading" :row-key="(r: SystemVariable) => r.key" size="small" :pagination="false" />
  </div>

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
