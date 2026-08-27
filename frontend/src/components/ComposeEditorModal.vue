<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount, computed } from 'vue'
import {
  NDrawer, NInput, NButton, NSpace, NAlert, NText, NTag, NRadioGroup, NRadioButton,
  NCollapse, NCollapseItem, NInputNumber, NSelect, useMessage,
} from 'naive-ui'
import {
  getCompose, createCompose, saveCompose, validateCompose, wsURL,
  type DeployProgress,
} from '../api/docker'
import { parseCompose, serializeCompose, type ComposeState, type ServiceConfig } from '../utils/compose'

const props = defineProps<{ show: boolean; project: string | null; readOnly?: boolean }>()
const emit = defineEmits<{ 'update:show': (v: boolean) => void; saved: () => void }>()

const message = useMessage()

const projectName = ref('')
const displayName = ref('')
const loading = ref(false)
const saving = ref(false)
const validating = ref(false)
const validateError = ref('')
const validateWarnings = ref<string[]>([])

// 双向同步的状态(docs §7.2)
const formState = ref<ComposeState>({ services: {} })
const activeService = ref('')
const activeTab = ref<'form' | 'yaml'>('form')
/** 代数版本锁:任一同步任务开始时 ++,执行后判断是否仍是最新。 */
let stateVersion = 0

// 部署状态
const deploying = ref(false)
const deployLines = ref<DeployProgress[]>([])
const deployError = ref('')
const deployDone = ref(false)
let deploySocket: WebSocket | null = null

const isNew = computed(() => !props.project)
const editable = computed(() => !props.readOnly)

const editorEl = ref<HTMLElement | null>(null)
let editorView: any = null
let cmModule: any = null
let editorDebounce: number | undefined

const restartOptions = [
  { label: '不重启', value: 'no' },
  { label: '总是', value: 'always' },
  { label: '失败时', value: 'on-failure' },
  { label: '除非手动停止', value: 'unless-stopped' },
]

function defaultTemplate(): string {
  return `services:
  web:
    image: nginx:alpine
    container_name: my-web
    restart: unless-stopped
    ports:
      - "8080:80"
`
}

async function loadCodeMirror() {
  if (cmModule) return cmModule
  const [cm, langYaml, state] = await Promise.all([
    import('codemirror'),
    import('@codemirror/lang-yaml'),
    import('@codemirror/state'),
  ])
  cmModule = { cm, langYaml, state }
  return cmModule
}

async function ensureEditor() {
  if (editorView || !editorEl.value) return
  const { cm, langYaml, state } = await loadCodeMirror()
  editorView = new cm.EditorView({
    doc: serializeCompose(formState.value),
    extensions: [
      cm.basicSetup,
      langYaml.yaml(),
      state.EditorState.tabSize.of(2),
      cm.EditorView.editable.of(editable.value),
      cm.EditorView.lineWrapping,
      cm.EditorView.updateListener.of((update: any) => {
        if (update.docChanged) scheduleYamlToForm()
      }),
    ],
    parent: editorEl.value,
  })
}

function destroyEditor() {
  editorView?.destroy()
  editorView = null
}

function editorContent(): string {
  return editorView ? editorView.state.doc.toString() : serializeCompose(formState.value)
}

function setEditorContent(y: string) {
  if (!editorView) return
  const doc = editorView.state.doc
  if (doc.toString() !== y) {
    editorView.dispatch({ changes: { from: 0, to: doc.length, insert: y } })
  }
}

// --- 双向同步(Generation Lock) ---

/** 表单 → YAML。禁止 @input 实时触发,只在 @blur / 显式同步时调用。 */
function formToYaml() {
  const v = ++stateVersion
  const y = serializeCompose(formState.value)
  if (v !== stateVersion) return // 期间有更新的同步,丢弃本任务
  setEditorContent(y)
}

/** YAML → 表单。语法错误时不覆盖 formState(docs §7.2)。 */
function yamlToForm() {
  const v = ++stateVersion
  let s: ComposeState
  try {
    s = parseCompose(editorContent())
  } catch {
    return // 语法错误:表单维持上一次成功解析的状态
  }
  if (v !== stateVersion) return
  formState.value = s
  if (!s.services[activeService.value]) activeService.value = Object.keys(s.services)[0] || ''
}

/** 编辑器 onChange → 400ms 防抖后同步到表单。 */
function scheduleYamlToForm() {
  if (editorDebounce) clearTimeout(editorDebounce)
  editorDebounce = window.setTimeout(yamlToForm, 400)
}

function currentYAML(): string {
  // 保存时以表单为真理:先强制把编辑器最新内容同步过来,再序列化
  if (activeTab.value === 'yaml') yamlToForm()
  return serializeCompose(formState.value)
}

watch(() => props.show, async (v) => {
  if (v) {
    loading.value = true
    validateError.value = ''
    deployLines.value = []
    deployError.value = ''
    deployDone.value = false
    activeTab.value = 'form'
    projectName.value = props.project || ''
    displayName.value = ''
    try {
      let yaml = ''
      if (props.project) {
        const d = await getCompose(props.project)
        displayName.value = d.displayName
        yaml = d.yaml
      } else {
        yaml = defaultTemplate()
      }
      formState.value = parseCompose(yaml)
      activeService.value = Object.keys(formState.value.services)[0] || ''
    } catch (e: any) {
      message.error('读取配置失败 — ' + e.message)
      formState.value = { services: {} }
    } finally {
      loading.value = false
    }
    await nextTick()
    await ensureEditor()
  } else {
    closeDeploy()
    destroyEditor()
  }
})

// --- 服务增删 ---

function addService() {
  let name = 'web'
  let i = 2
  while (formState.value.services[name]) name = `web${i++}`
  formState.value.services[name] = { image: '' }
  activeService.value = name
  formToYaml()
}

function removeService(name: string) {
  delete formState.value.services[name]
  if (activeService.value === name) activeService.value = Object.keys(formState.value.services)[0] || ''
  formToYaml()
}

// --- 校验 / 保存 / 部署 ---

async function doValidate() {
  const project = projectName.value.trim() || props.project || ''
  if (!project) {
    message.warning('请填写 projectName')
    return
  }
  validating.value = true
  validateError.value = ''
  validateWarnings.value = []
  try {
    const res = await validateCompose({ project, yaml: currentYAML() })
    validateWarnings.value = res.warnings || []
    if (res.warnings?.length) {
      message.warning(`校验通过，但有 ${res.warnings.length} 条警告`)
    } else {
      message.success('校验通过')
    }
  } catch (e: any) {
    validateError.value = e.message
    message.error('校验失败')
  } finally {
    validating.value = false
  }
}

async function doSave(): Promise<string | null> {
  const yaml = currentYAML()
  if (!yaml.trim()) {
    message.warning('内容不能为空')
    return null
  }
  saving.value = true
  try {
    if (isNew.value) {
      const p = projectName.value.trim()
      if (!p) {
        message.warning('请填写 projectName')
        return null
      }
      await createCompose(p, { displayName: displayName.value, yaml })
      projectName.value = p
    } else {
      await saveCompose(props.project!, { displayName: displayName.value, yaml })
    }
    return projectName.value
  } catch (e: any) {
    validateError.value = e.message
    message.error('保存失败 — ' + e.message)
    return null
  } finally {
    saving.value = false
  }
}

async function doSaveAndDeploy() {
  const project = await doSave()
  if (project) startDeploy(project)
}

function startDeploy(project: string) {
  deploying.value = true
  deployDone.value = false
  deployError.value = ''
  deployLines.value = []

  deploySocket = new WebSocket(wsURL(`/docker/compose/${project}/deploy`))
  deploySocket.onmessage = (ev) => {
    const p: DeployProgress = JSON.parse(ev.data)
    if (p.error) {
      deployError.value = p.error
      deploying.value = false
      return
    }
    deployLines.value.push(p)
    if (p.done) {
      deployDone.value = true
      deploying.value = false
      message.success('部署完成')
      emit('saved')
    }
  }
  deploySocket.onerror = () => {
    deployError.value = '部署连接失败'
    deploying.value = false
  }
}

function closeDeploy() {
  if (deploySocket) {
    deploySocket.onmessage = null
    deploySocket.close()
    deploySocket = null
  }
  deploying.value = false
}

onBeforeUnmount(() => {
  closeDeploy()
  destroyEditor()
})

// --- 列表/键值字段的 textarea 互转 ---

function listToText(list?: string[]): string {
  return (list || []).join('\n')
}

function textToList(text: string): string[] {
  return text.split('\n').map((s) => s.trim()).filter(Boolean)
}

function envToText(env?: Record<string, string>): string {
  return Object.entries(env || {}).map(([k, v]) => `${k}=${v}`).join('\n')
}

function textToEnv(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const s = line.trim()
    const eq = s.indexOf('=')
    if (eq > 0) out[s.slice(0, eq).trim()] = s.slice(eq + 1)
  }
  return out
}
</script>

<template>
  <n-drawer
    :show="show"
    placement="right"
    width="min(960px, 100vw)"
    :mask-closable="!deploying"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <template #header>
      <span style="font-size: 15px; font-weight: 600">{{ isNew ? '创建应用' : `编辑 ${displayName || projectName}` }}</span>
    </template>
    <div style="display: flex; flex-direction: column; gap: 12px">
      <div style="display: flex; gap: 12px">
        <div style="flex: 1">
          <n-text depth="3" style="font-size: 12px">projectName（{{ isNew ? '创建后不可改' : '只读' }}）</n-text>
          <n-input v-model:value="projectName" :disabled="!isNew" placeholder="my-app" />
        </div>
        <div style="flex: 1">
          <n-text depth="3" style="font-size: 12px">展示名</n-text>
          <n-input v-model:value="displayName" :disabled="readOnly" placeholder="家庭影院" />
        </div>
      </div>

      <n-alert v-if="validateError" type="error" :show-icon="true" style="margin-bottom: 0">
        {{ validateError }}
      </n-alert>
      <n-alert v-if="validateWarnings.length" type="warning" :show-icon="true" style="margin-bottom: 0">
        <div v-for="(w, i) in validateWarnings" :key="i">{{ w }}</div>
      </n-alert>
      <n-alert v-if="readOnly" type="info" :show-icon="true">
        外部编排只读。编辑需在源文件中进行或先「接管」。
      </n-alert>

      <n-radio-group v-model:value="activeTab" size="small">
        <n-radio-button value="form">表单</n-radio-button>
        <n-radio-button value="yaml">YAML</n-radio-button>
      </n-radio-group>

      <!-- 表单视图 -->
      <div v-if="activeTab === 'form'" style="display: flex; gap: 12px; min-height: 400px">
        <!-- 服务列表 -->
        <div style="width: 200px; flex-shrink: 0; border-right: 1px solid #eee; padding-right: 8px">
          <div v-for="(svc, name) in formState.services" :key="name" style="margin-bottom: 4px">
            <n-button
              size="tiny"
              :type="activeService === name ? 'primary' : 'default'"
              :ghost="activeService !== name"
              block
              @click="activeService = name"
            >
              {{ name }}
            </n-button>
            <n-button v-if="editable" size="tiny" quaternary type="error" style="margin-left: 2px" @click="removeService(name)">
              ✕
            </n-button>
          </div>
          <n-button v-if="editable" size="tiny" dashed block style="margin-top: 8px" @click="addService">
            + 服务
          </n-button>
        </div>

        <!-- 服务表单 -->
        <div v-if="activeService && formState.services[activeService]" style="flex: 1; display: flex; flex-direction: column; gap: 10px; overflow: auto; max-height: 480px">
          <n-text strong>{{ activeService }}</n-text>

          <div style="display: flex; gap: 10px">
            <div style="flex: 2">
              <n-text depth="3" style="font-size: 12px">镜像</n-text>
              <n-input v-model:value="formState.services[activeService].image" :disabled="!editable" @blur="formToYaml" />
            </div>
            <div style="flex: 1">
              <n-text depth="3" style="font-size: 12px">容器名</n-text>
              <n-input v-model:value="formState.services[activeService].container_name" :disabled="!editable" @blur="formToYaml" />
            </div>
            <div style="flex: 1">
              <n-text depth="3" style="font-size: 12px">重启策略</n-text>
              <n-select v-model:value="formState.services[activeService].restart" :options="restartOptions" :disabled="!editable" @blur="formToYaml" />
            </div>
          </div>

          <div>
            <n-text depth="3" style="font-size: 12px">端口（一行一个，如 8080:80）</n-text>
            <n-input
              :value="listToText(formState.services[activeService].ports)"
              type="textarea"
              :autosize="{ minRows: 2 }"
              :disabled="!editable"
              @update:value="(v) => { formState.services[activeService].ports = textToList(v) }"
              @blur="formToYaml"
            />
          </div>

          <div>
            <n-text depth="3" style="font-size: 12px">环境变量（一行一个 KEY=value）</n-text>
            <n-input
              :value="envToText(formState.services[activeService].environment)"
              type="textarea"
              :autosize="{ minRows: 2 }"
              :disabled="!editable"
              @update:value="(v) => { formState.services[activeService].environment = textToEnv(v) }"
              @blur="formToYaml"
            />
          </div>

          <div>
            <n-text depth="3" style="font-size: 12px">卷挂载（一行一个，如 ./config:/config）</n-text>
            <n-input
              :value="listToText(formState.services[activeService].volumes)"
              type="textarea"
              :autosize="{ minRows: 2 }"
              :disabled="!editable"
              @update:value="(v) => { formState.services[activeService].volumes = textToList(v) }"
              @blur="formToYaml"
            />
          </div>

          <div>
            <n-text depth="3" style="font-size: 12px">网络（一行一个）</n-text>
            <n-input
              :value="listToText(formState.services[activeService].networks)"
              type="textarea"
              :autosize="{ minRows: 1 }"
              :disabled="!editable"
              @update:value="(v) => { formState.services[activeService].networks = textToList(v) }"
              @blur="formToYaml"
            />
          </div>

          <!-- 高级字段 -->
          <n-collapse>
            <n-collapse-item title="高级设置" name="advanced">
              <div style="display: flex; flex-direction: column; gap: 10px">
                <div style="display: flex; gap: 10px">
                  <div style="flex: 1">
                    <n-text depth="3" style="font-size: 12px">network_mode</n-text>
                    <n-input v-model:value="formState.services[activeService].network_mode" :disabled="!editable" @blur="formToYaml" />
                  </div>
                  <div style="flex: 1">
                    <n-text depth="3" style="font-size: 12px">user（PUID:PGID）</n-text>
                    <n-input v-model:value="formState.services[activeService].user" :disabled="!editable" @blur="formToYaml" />
                  </div>
                </div>
                <div>
                  <n-text depth="3" style="font-size: 12px">command</n-text>
                  <n-input v-model:value="formState.services[activeService].command" :disabled="!editable" @blur="formToYaml" />
                </div>
                <div>
                  <n-text depth="3" style="font-size: 12px">设备（一行一个，如 /dev/dri:/dev/dri）</n-text>
                  <n-input
                    :value="listToText(formState.services[activeService].devices)"
                    type="textarea"
                    :autosize="{ minRows: 1 }"
                    :disabled="!editable"
                    @update:value="(v) => { formState.services[activeService].devices = textToList(v) }"
                    @blur="formToYaml"
                  />
                </div>
                <div>
                  <n-text depth="3" style="font-size: 12px">cap_add（一行一个）</n-text>
                  <n-input
                    :value="listToText(formState.services[activeService].cap_add)"
                    type="textarea"
                    :autosize="{ minRows: 1 }"
                    :disabled="!editable"
                    @update:value="(v) => { formState.services[activeService].cap_add = textToList(v) }"
                    @blur="formToYaml"
                  />
                </div>
                <div>
                  <n-text depth="3" style="font-size: 12px">健康检查命令（如 curl -f http://localhost:8096）</n-text>
                  <n-input
                    :value="formState.services[activeService].healthcheck?.test"
                    :disabled="!editable"
                    @update:value="(v) => { const hc = formState.services[activeService].healthcheck || {}; hc.test = v; formState.services[activeService].healthcheck = hc }"
                    @blur="formToYaml"
                  />
                </div>
                <div style="display: flex; gap: 10px">
                  <div style="flex: 1">
                    <n-text depth="3" style="font-size: 12px">复制数 replicas</n-text>
                    <n-input-number
                      :value="formState.services[activeService].deploy?.replicas ?? null"
                      :disabled="!editable"
                      :min="0"
                      @update:value="(v) => { const d = formState.services[activeService].deploy || {}; d.replicas = v ?? undefined; formState.services[activeService].deploy = d }"
                      @blur="formToYaml"
                    />
                  </div>
                  <div style="flex: 1">
                    <n-text depth="3" style="font-size: 12px">构建上下文</n-text>
                    <n-input
                      :value="typeof formState.services[activeService].build === 'string' ? formState.services[activeService].build : (formState.services[activeService].build as any)?.context"
                      :disabled="!editable"
                      @update:value="(v) => { const b = formState.services[activeService].build; formState.services[activeService].build = typeof b === 'object' && b ? { ...b, context: v } : v }"
                      @blur="formToYaml"
                    />
                  </div>
                </div>

                <!-- caddy 路由 -->
                <div>
                  <n-text depth="3" style="font-size: 12px">Caddy 路由（domain + path）</n-text>
                  <div v-for="(r, i) in formState.services[activeService].caddyRoutes || []" :key="i" style="display: flex; gap: 6px; margin-bottom: 4px">
                    <n-input v-model:value="r.domain" placeholder="example.com" :disabled="!editable" @blur="formToYaml" />
                    <n-input v-model:value="r.path" placeholder="/path" :disabled="!editable" @blur="formToYaml" />
                    <n-button v-if="editable" size="tiny" quaternary type="error" @click="formState.services[activeService].caddyRoutes!.splice(i, 1); formToYaml()">✕</n-button>
                  </div>
                  <n-button v-if="editable" size="tiny" dashed @click="() => { const s = formState.services[activeService]; s.caddyRoutes = s.caddyRoutes || []; s.caddyRoutes.push({}); formToYaml() }">+ 路由</n-button>
                </div>
              </div>
            </n-collapse-item>
          </n-collapse>

          <!-- 未识别字段只读预览 -->
          <n-collapse v-if="formState.services[activeService]._rawConfigs">
            <n-collapse-item title="未识别字段（只读，在 YAML 中编辑）" name="raw">
              <pre style="font-size: 12px; background: #f7f7f7; padding: 8px; border-radius: 4px; overflow: auto">{{ JSON.stringify(formState.services[activeService]._rawConfigs, null, 2) }}</pre>
            </n-collapse-item>
          </n-collapse>
        </div>
      </div>

      <!-- YAML 视图 -->
      <div v-else>
        <div
          ref="editorEl"
          style="height: 480px; border: 1px solid #e0e0e0; border-radius: 4px; overflow: auto"
        />
      </div>

      <div v-if="deploying || deployDone || deployError" style="max-height: 160px; overflow: auto; font-size: 12px; font-family: monospace; background: #f7f7f7; border-radius: 4px; padding: 8px">
        <div v-for="(l, i) in deployLines" :key="i" style="line-height: 1.6">
          <n-tag size="tiny" :type="l.status === 'Done' ? 'success' : l.status === 'Error' ? 'error' : 'default'" :bordered="false" style="margin-right: 6px">
            {{ l.status }}
          </n-tag>
          <span>{{ l.id }}</span>
          <span style="color: #888; margin-left: 6px">{{ l.text }}</span>
        </div>
        <n-text v-if="deployError" type="error" style="font-size: 12px">{{ deployError }}</n-text>
        <n-text v-if="deployDone" type="success" style="font-size: 12px">✓ 部署完成</n-text>
      </div>
    </div>

    <template #footer>
      <n-space justify="end">
        <template v-if="editable">
          <n-button size="small" :loading="validating" @click="doValidate">校验</n-button>
          <n-button size="small" :loading="saving" @click="doSave">保存</n-button>
          <n-button size="small" type="primary" :loading="saving || deploying" :disabled="deploying" @click="doSaveAndDeploy">
            保存并部署
          </n-button>
        </template>
        <n-button size="small" @click="emit('update:show', false)">关闭</n-button>
      </n-space>
    </template>
  </n-drawer>
</template>
