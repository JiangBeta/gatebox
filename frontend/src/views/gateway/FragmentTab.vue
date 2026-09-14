<script setup lang="ts">
import { ref, watch, onMounted, nextTick, onBeforeUnmount, h } from 'vue'
import {
  Button, Drawer, Input, Switch, Tooltip,
  Tag, Popconfirm, Typography, Table, message,
} from 'ant-design-vue'
import { DownOutlined, UpOutlined, PlusOutlined, CloseOutlined, EyeOutlined, EditOutlined, DeleteOutlined } from '@ant-design/icons-vue'
import {
  listFragments, createFragment, updateFragment, deleteFragment, listVariables, createVariable,
  type FragmentView, type Variable,
} from '../../api/gateway'
import CodeEditor from '../../components/CodeEditor.vue'

const [messageApi, contextHolder] = message.useMessage()
const list = ref<FragmentView[]>([])
const loading = ref(false)

const showForm = ref(false)
const editing = ref<FragmentView | null>(null)
const builtin = ref(false)

const form = ref({
  name: '',
  description: '',
  defaultEnabled: false,
  defaultHidden: false,
  code: '',
})

function onNameInput(e: Event) {
  const el = e.target as HTMLInputElement
  form.value.name = el.value.toUpperCase().replace(/[^A-Z0-9\-_.]/g, '')
}

const variables = ref<Variable[]>([])
const varsExpanded = ref(false)

// 行内新增变量
const addVarVisible = ref(false)
const addDrafts = ref<{ key: string; value: string; description: string }[]>([])

const builtinVars = [
  { key: 'GB_APP', desc: '当前应用名称' },
  { key: 'GB_SERVICE', desc: '当前服务名称' },
  { key: 'GB_STATIC_ROOT', desc: '静态文件根目录' },
  { key: 'GB_HOST_PORT', desc: '主机端口' },
]

const editorRef = ref<InstanceType<typeof CodeEditor> | null>(null)

async function loadMeta() {
  try { variables.value = await listVariables() } catch { /* 忽略变量加载失败 */ }
}

function openAdd() {
  editing.value = null
  builtin.value = false
  form.value = { name: '', description: '', defaultEnabled: false, defaultHidden: false, code: '' }
  varsExpanded.value = false
  showForm.value = true
}

function openEdit(row: FragmentView) {
  editing.value = row
  builtin.value = row.builtin
  form.value = {
    name: row.name, description: row.description || '',
    defaultEnabled: row.defaultEnabled, defaultHidden: row.defaultHidden, code: row.code,
  }
  varsExpanded.value = false
  showForm.value = true
}

watch(showForm, (v) => {
  if (!v) addVarVisible.value = false
})

function insertVar(key: string) {
  const ed = editorRef.value
  if (!ed) return
  ed.focus()
  nextTick(() => {
    ;(ed as any).insertAtCursor?.(`<%${key}%>`)
  })
}

// 行内新增变量
function addVarDraft() { addDrafts.value.push({ key: '', value: '', description: '' }) }
function removeVarDraft(i: number) { addDrafts.value.splice(i, 1) }
function openAddVar() { addDrafts.value = [{ key: '', value: '', description: '' }]; addVarVisible.value = true }
async function saveAddVar() {
  const rows = addDrafts.value.map((r) => ({ ...r, key: r.key.trim().toUpperCase() })).filter((r) => r.key !== '' || r.value !== '')
  if (rows.length === 0) return messageApi.warning('请至少填写一个变量')
  for (const r of rows) {
    if (!r.key) return messageApi.warning('变量名不能为空')
    if (r.key.startsWith('GB_')) return messageApi.warning(`「${r.key}」不能以 GB_ 开头`)
  }
  try {
    for (const r of rows) await createVariable(r.key, r.value, r.description)
    messageApi.success(`已创建 ${rows.length} 个变量`)
    addVarVisible.value = false
    await loadMeta()
  } catch (e: any) { messageApi.error(e.message) }
}

async function save() {
  if (!form.value.name.trim()) return messageApi.warning('请输入片段名')
  const payload = {
    name: form.value.name.trim(), description: form.value.description,
    code: form.value.code,
    defaultEnabled: form.value.defaultEnabled, defaultHidden: form.value.defaultHidden,
  }
  try {
    if (editing.value) { await updateFragment(editing.value.id, payload); messageApi.success('已更新') }
    else { await createFragment(payload); messageApi.success('已创建') }
    showForm.value = false
    await load()
  } catch (e: any) { messageApi.error(e.message) }
}

async function doDelete(row: FragmentView) {
  try { await deleteFragment(row.id); messageApi.success('已删除'); await load() }
  catch (e: any) { messageApi.error(e.message) }
}

/** 操作按钮:icon-only(不同颜色) + hover Tooltip 显示文字,与代理页操作区风格统一。 */
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
  { title: '名称', dataIndex: 'name', key: 'name' },
  {
    title: '来源', key: 'builtin', width: 90,
    customRender: ({ record }: { record: FragmentView }) => record.builtin
      ? h(Tag, { color: 'processing', size: 'small' }, { default: () => '内置' })
      : h(Tag, { color: 'success', size: 'small' }, { default: () => '自定义' }),
  },
  { title: '默认启用', key: 'defaultEnabled', width: 100, customRender: ({ record }: { record: FragmentView }) => record.defaultEnabled ? '是' : '否' },
  {
    title: '说明', key: 'desc',
    customRender: ({ record }: { record: FragmentView }) => record.description
      ? h(Tooltip, { title: record.description }, { default: () => h('span', record.description.slice(0, 20)) })
      : '-',
  },
  {
    title: '操作', key: 'actions', width: 110,
    customRender: ({ record }: { record: FragmentView }) => {
      // 统一左对齐:与代理页一致,自定义行含修改+删除,内置行仅查看。
      return h('div', { style: 'display:flex; justify-content:flex-start; gap:2px' }, [
        record.builtin
          ? actionBtn(EyeOutlined, '#1677ff', '查看', () => openEdit(record))
          : h('span', { style: 'display:inline-flex; gap:2px' }, [
              actionBtn(EditOutlined, '#722ed1', '修改', () => openEdit(record)),
              h(
                Popconfirm,
                { title: '确认删除该片段？', onConfirm: () => doDelete(record) },
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
      ])
    },
  },
]

async function load() {
  loading.value = true
  try { list.value = await listFragments() } catch (e: any) { messageApi.error(e.message) } finally { loading.value = false }
}

onMounted(async () => { loadMeta(); await load() })
</script>

<template>
  <contextHolder />
  <div style="margin-bottom: 16px; display: flex; justify-content: flex-end">
    <Button type="primary" @click="openAdd">+ 创建片段</Button>
  </div>
  <Table :columns="columns" :data-source="list" :loading="loading" :row-key="(r: FragmentView) => r.id" />

  <Drawer :open="showForm" placement="right" :width="720" @close="showForm = false"
    :body-style="{ display: 'flex', flexDirection: 'column', minHeight: '0', overflow: 'hidden' }">
    <template #title>
      <span class="dw-drawer-title">{{ editing ? (builtin ? '编辑内置片段' : '修改片段') : '创建片段' }}</span>
    </template>

      <div style="display: flex; flex-direction: column; min-height: 0; flex: 1">

        <!-- 基本信息 -->
        <div style="flex-shrink: 0">
          <div class="dw-section">基本信息</div>
          <div style="display: flex; gap: 10px">
            <div style="flex: 1">
              <div class="dw-label">片段名称 <span class="dw-required">*</span></div>
              <Input :value="form.name" @input="onNameInput" :disabled="builtin"
                placeholder="如 MY_CUSTOM_HEADER" :style="{ fontFamily: 'monospace' }" />
              <div class="dw-desc" style="margin-top: 2px">仅允许大写字母、数字、- _ .</div>
            </div>
            <div style="flex: 1">
              <div class="dw-label">片段说明</div>
              <Input v-model:value="form.description" :disabled="builtin" placeholder="可选" />
            </div>
          </div>
          <div style="display: flex; gap: 16px; margin-top: 8px; align-items: flex-start">
            <div style="flex: 1">
              <div class="dw-desc" style="margin-top: 2px">片段名仅允许大写字母、数字、- _ .</div>
            </div>
            <div style="display: flex; flex-direction: column; gap: 8px; padding-top: 22px">
                <div style="display: flex; align-items: center; gap: 12px">
                  <span style="font-size: 14px; color: #000; width: 72px">默认启用</span>
                  <Switch v-model:checked="form.defaultEnabled" />
                </div>
                <div style="display: flex; align-items: center; gap: 12px">
                  <span style="font-size: 14px; color: #000; width: 72px">默认隐藏</span>
                  <Switch v-model:checked="form.defaultHidden" />
                </div>
                <div v-if="builtin" style="color: #888; font-size: 12px; line-height: 1.5">内置片段仅可调整默认开关,内容与名称不可修改。</div>
              </div>
          </div>
        </div>

        <!-- 片段内容 -->
        <div class="dw-divider" style="margin: 12px 0 0; padding-top: 12px; display: flex; flex-direction: column; min-height: 0; flex: 1">
          <div class="dw-section" style="margin-bottom: 8px; flex-shrink: 0">片段内容</div>

          <!-- 变量选择框 -->
          <div style="border: 1px solid #e0e0e6; border-radius: 6px; padding: 8px 10px; margin-bottom: 8px; flex-shrink: 0">
            <!-- 内建变量 -->
            <div style="display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 4px">
              <Tooltip v-for="bv in builtinVars" :key="bv.key" :title="bv.desc">
                <Tag size="small" style="cursor: pointer; font-family: monospace; font-size: 12px; background: #f0f0f0"
                  @click="insertVar(bv.key)">{{ bv.key }}</Tag>
              </Tooltip>
            </div>
            <!-- 用户变量 -->
            <div v-if="variables.length || addVarVisible" style="border-top: 1px dashed #e0e0e6; padding-top: 6px">
              <!-- 第一行: 变量内容 + 新增/收起按钮 -->
              <div style="display: flex; flex-wrap: wrap; gap: 4px; align-items: center">
                <Tooltip v-for="v in (varsExpanded ? variables : variables.slice(0, 8))" :key="v.key"
                  :title="'值：' + (v.value || '（空）') + (v.description ? ' · ' + v.description : '')">
                  <Tag size="small" style="cursor: pointer; font-family: monospace; font-size: 12px; background: #f0f0f0"
                    @click="insertVar(v.key)">{{ v.key }}</Tag>
                </Tooltip>
                <div style="flex: 1"></div>
                <Button v-if="!addVarVisible" type="link" size="small" @click="openAddVar" style="font-size: 11px">
                  <template #icon><PlusOutlined style="font-size: 12px" /></template>新增
                </Button>
                <Button v-if="!addVarVisible" type="text" size="small" @click="varsExpanded = !varsExpanded" style="font-size: 11px">
                  {{ varsExpanded ? '收起' : `全部（${variables.length}）` }}
                  <template #icon><component :is="varsExpanded ? UpOutlined : DownOutlined" /></template>
                </Button>
              </div>
              <!-- 行内新增变量表单 -->
              <div v-if="addVarVisible" style="margin-top: 8px; border: 1px solid #e0e0e6; border-radius: 4px; padding: 8px; background: #fafafa">
                <div style="display: flex; gap: 6px; align-items: center; padding: 0 2px 4px; color: #888; font-size: 11px">
                  <span style="flex: 1">变量名</span>
                  <span style="flex: 1">值</span>
                  <span style="flex: 1.2">说明</span>
                  <span style="width: 28px"></span>
                </div>
                <div v-for="(r, i) in addDrafts" :key="i" style="display: flex; gap: 6px; align-items: center; margin-bottom: 4px">
                  <Input v-model:value="r.key" size="small" placeholder="NAME" style="flex: 1" @update:value="(val: string) => { r.key = val.toUpperCase() }" />
                  <Input v-model:value="r.value" size="small" placeholder="值" style="flex: 1" />
                  <Input v-model:value="r.description" size="small" placeholder="说明" style="flex: 1.2" />
                  <Button size="small" type="text" danger @click="removeVarDraft(i)">
                    <template #icon><CloseOutlined /></template>
                  </Button>
                </div>
                <div style="display: flex; gap: 6px; margin-top: 6px">
                  <Button size="small" @click="addVarDraft">+ 添加行</Button>
                  <div style="flex: 1"></div>
                  <Button size="small" @click="addVarVisible = false">取消</Button>
                  <Button size="small" type="primary" @click="saveAddVar">确定</Button>
                </div>
              </div>
            </div>
          </div>

          <!-- 代码编辑器(复用 CodeEditor) -->
          <div style="flex: 1; min-height: 0; display: flex; flex-direction: column; margin-bottom: 6px">
            <CodeEditor ref="editorRef" v-model="form.code" language="json" height="100%" />
          </div>

          <div style="color: #888; font-size: 12px; line-height: 1.6; margin-bottom: 4px; flex-shrink: 0">
            点击上方变量名可插入到编辑器光标处。格式：&lt;%变量名%&gt;。
            ${...} 原样透传给 caddy 环境变量。
          </div>
        </div>

      </div>

      <template #footer>
      <div class="dw-footer">
        <Button @click="showForm = false">取消</Button>
        <Button v-if="editing" type="primary" @click="save">{{ builtin ? '保存开关' : '保存' }}</Button>
        <Button v-else type="primary" @click="save">确定</Button>
      </div>
      </template>
  </Drawer>
</template>
