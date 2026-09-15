<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount, computed } from 'vue'
import {
  Drawer, Modal, Input, Button, Space, Alert, Tag, Popconfirm, Switch, Progress,
  Collapse, InputNumber, Select, Typography, Tooltip, message,
} from 'ant-design-vue'
import {
  getCompose, createCompose, saveCompose, validateCompose, wsURL,
  dockerInfo, listImages, listNetworks,
  type DeployProgress, type DockerInfo, type ImageView, type NetworkView,
} from '../api/docker'
import {
  listVariables, listSystemVariables, createVariable,
  type Variable, type SystemVariable,
} from '../api/settings'
import { listDomains, type Domain } from '../api/domains'
import { parseCompose, serializeCompose, type ComposeState, type ServiceConfig, type CaddyRoute } from '../utils/compose'
import { listFragments, type FragmentView } from '../api/gateway'
import { listPorts, createPort, type PortBinding } from '../api/gateway'
import { listCapabilities, hasNonHTTPProtocol, type Capability } from '../api/capabilities'
import CodeEditor from './CodeEditor.vue'
// maple-mono 字体:按需引入 latin 子集的 400/700 字重
import '@fontsource/maple-mono/latin-400.css'
import '@fontsource/maple-mono/latin-700.css'

const props = defineProps<{ show: boolean; project: string | null; readOnly?: boolean }>()
const emit = defineEmits<{ 'update:show': (v: boolean) => void; saved: () => void }>()

const [messageApi, contextHolder] = message.useMessage()

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
/** 代数版本锁:任一同步任务开始时 ++,执行后判断是否仍是最新。 */
let stateVersion = 0
/** YAML 编辑器是否有未同步到表单的改动(两栏并排,保存据此决定以谁为准)。 */
let yamlDirty = false

// 部署状态
const deploying = ref(false)
const deployLines = ref<DeployProgress[]>([])
const deployError = ref('')
const deployDone = ref(false)
let deploySocket: WebSocket | null = null

// 项目目录(相对运行目录,无结尾 /):用于把 `./<项目目录>/…` 相对挂载归并为 ${GB_PROJ_FILE} 变量
const projectDir = ref('')

const isNew = computed(() => !props.project)
const editable = computed(() => !props.readOnly)

// 系统信息 / 已有镜像 / 域名列表(镜像选择、域名访问、资源上限用)
const info = ref<DockerInfo | null>(null)
const images = ref<ImageView[]>([])
const domains = ref<Domain[]>([])

// 两栏布局:form/yaml 显隐与分隔比例(可拖动调整)
const showForm = ref(true)
const showYaml = ref(true)
const splitRatio = ref(0.5)
const configTab = ref<'basic' | 'runtime' | 'relation'>('basic')
const configTabs = [
  { key: 'basic', label: '基本配置' },
  { key: 'runtime', label: '运行配置' },
  { key: 'relation', label: '关联配置' },
]
const logDriverOptions = [
  { label: 'json-file', value: 'json-file' },
  { label: 'journal', value: 'journald' },
]
const mainAreaEl = ref<HTMLElement | null>(null)
let dragging = false
const formAreaStyle = computed(() =>
  showYaml.value
    ? { width: `calc(${splitRatio.value * 100}% - 6px)`, flexShrink: '0' }
    : { flex: '1 1 auto' },
)

function startDrag(e: MouseEvent) {
  dragging = true
  e.preventDefault()
  document.addEventListener('mousemove', onDrag)
  document.addEventListener('mouseup', stopDrag)
}
function onDrag(e: MouseEvent) {
  if (!dragging || !mainAreaEl.value) return
  const rect = mainAreaEl.value.getBoundingClientRect()
  splitRatio.value = Math.min(0.8, Math.max(0.2, (e.clientX - rect.left) / rect.width))
}
function stopDrag() {
  dragging = false
  document.removeEventListener('mousemove', onDrag)
  document.removeEventListener('mouseup', stopDrag)
}
// YAML 区重新显示后,CodeMirror 需重新测量(隐藏期间容器尺寸为 0)
watch(showYaml, (v) => {
  if (v) nextTick(() => editorRef.value?.requestMeasure())
})

const restartOptions = [
  { label: '不重启', value: 'no' },
  { label: '总是', value: 'always' },
  { label: '失败时', value: 'on-failure' },
  { label: '除非手动停止', value: 'unless-stopped' },
]

function defaultTemplate(): string {
  return `services:
  Ser-1:
    image: ''
    restart: unless-stopped
`
}

/** 服务在表单中的序号(1 起),用于变量排号 `${...GB_SER_N_...}`。 */
function serviceNo(name: string): number {
  const keys = Object.keys(formState.value.services)
  const idx = keys.indexOf(name)
  return idx >= 0 ? idx + 1 : 1
}
/** 构造 GB 变量名 `${GB_...}` 文本。 */
function gbVar(suffix: string): string {
  return `\${GB_${suffix}}`
}
/** 当前活动服务的应用名(容器名 || Ser-N)。 */
function appName(): string {
  const s = formState.value.services[activeService.value]
  return s?.container_name || `Ser-${serviceNo(activeService.value)}`
}

// --- CodeEditor 委托 ---

const editorRef = ref<InstanceType<typeof CodeEditor> | null>(null)
const yamlText = ref('')
let settingYaml = false

// 统一变量(ADR-035,网关+容器共用):编辑 YAML 时点击标签在光标处插入 ${KEY}
const containerVars = ref<Variable[]>([])
// 容器系统变量(只读,后端下发)
const containerBuiltinVars = ref<SystemVariable[]>([])
function insertContainerVar(key: string) {
  insertText(`\${${key}}`)
}
/** 向 EDIT 编辑器光标处插入原文(系统变量插 ${KEY},项目变量插推导值)。 */
function insertText(text: string) {
  const ed = editorRef.value as any
  if (!ed?.insertAtCursor) return
  ed.focus()
  nextTick(() => ed.insertAtCursor(text))
}
function loadContainerVars() {
  listVariables().then((vs) => { containerVars.value = vs }).catch(() => {})
  listSystemVariables().then((sys) => { containerBuiltinVars.value = sys.filter((v) => v.context === 'container') }).catch(() => {})
}

/** 项目变量(EDIT 变量框下半):分 3 行(项目变量 / 应用名称 / 外部)。
 *  label=chip 显示文案;tip=悬停提示;insert=点击插入到 EDIT 的文本(值。项目目录例外,插 ${GB_PROJ_FILE})。 */
const projectVarChips = computed(() => {
  const groups: { title: string; chips: { label: string; tip: string; insert: string }[] }[] = []
  const pName = projectName.value.trim()
  groups.push({
    title: '项目变量',
    chips: [
      { label: '项目名称', tip: pName ? `项目值：${pName}` : '未填写时显示「项目名称」', insert: pName },
    ],
  })
  // 应用(每服务一个,都放一行):显示输入的应用名称或 Ser-数字,点击插值
  const svcNames = Object.keys(formState.value.services)
  groups.push({
    title: '应用名称',
    chips: svcNames.map((name) => {
      const no = serviceNo(name)
      const ser = formState.value.services[name].container_name || `Ser-${no}`
      return { label: ser, tip: `服务名称（应用名）：${ser}`, insert: ser }
    }),
  })
  // 端口(每映射一个,都放一行):显示外部端口值,悬停看映射,点击插值
  const portChips: { label: string; tip: string; insert: string }[] = []
  for (const name of svcNames) {
    const no = serviceNo(name)
    ;(formState.value.services[name].ports || []).map(parsePortRow).forEach((p) => {
      const val = p.host || '-'
      portChips.push({ label: val, tip: `外部端口，映射 <${p.container}>/${p.protocol}`, insert: val })
    })
  }
  groups.push({ title: '外部', chips: portChips })
  return groups
})

// 变量框折叠(默认展开);折叠后编辑器占满 YAML 区高度
const varCollapsed = ref(false)

// 行内新增变量(同网关片段页变量框)
const addVarVisible = ref(false)
const addDrafts = ref<{ key: string; value: string; description: string }[]>([])
function openAddVar() {
  addDrafts.value = [{ key: '', value: '', description: '' }]
  addVarVisible.value = true
}
function addVarDraft() { addDrafts.value.push({ key: '', value: '', description: '' }) }
function removeVarDraft(i: number) { addDrafts.value.splice(i, 1) }
async function saveAddVar() {
  const rows = addDrafts.value.map((r) => ({ ...r, key: r.key.trim().toUpperCase() })).filter((r) => r.key !== '' || r.value !== '' || r.description !== '')
  if (rows.length === 0) return messageApi.warning('请至少填写一个变量')
  for (const r of rows) {
    if (!r.key) return messageApi.warning('变量名不能为空')
    if (r.key.startsWith('GB_')) return messageApi.warning(`「${r.key}」不能以 GB_ 开头(保留前缀)`)
  }
  try {
    for (const r of rows) await createVariable(r.key, r.value, r.description)
    messageApi.success(`已创建 ${rows.length} 个变量`)
    addVarVisible.value = false
    loadContainerVars()
  } catch (e: any) {
    messageApi.error(e.message)
  }
}

function editorContent(): string {
  return yamlText.value || serializeCompose(formState.value)
}

function setEditorContent(y: string) {
  editorRef.value?.setDoc(y)
}

function onYamlChange(_v: string) {
  if (settingYaml) return
  yamlDirty = true
  scheduleYamlToForm()
}

// --- 双向同步(Generation Lock) ---

/** 序列化为 YAML,并跳过未命名的环境变量与空端口(不改动 formState)。
 * 健康检查默认:localhost 后端口 = 该 SER 第一个发布端口(变量 ${GB_SER_<No>_PORT_1});
 * 无发布端口则该 SER 不进行健康检查(不写 healthcheck)。 */
function serializeForYaml(): string {
  const clean: ComposeState = JSON.parse(JSON.stringify(formState.value))
  clean.services = normalizeServiceKeys(clean.services)
  const svcNames = Object.keys(clean.services)
  svcNames.forEach((name) => {
    const svc = clean.services[name]
    const env = svc.environment
    if (env && '' in env) { const e = { ...env }; delete e['']; svc.environment = e }
    if (svc.ports) svc.ports = svc.ports.filter((p) => parsePortRow(p).container !== '')
    // 不再自动注入 healthcheck:镜像不一定内置 curl/wget(如 soulteary/flare),
    // 自动注入的 curl 探测会导致无 curl 镜像判定 unhealthy。需要时用户显式配置
    // (运行配置→健康检查,或 YAML 手写),此前的默认注入已污染已有项目(见 flare)。
  })
  return serializeCompose(clean)
}

/** 表单 → YAML。禁止 @input 实时触发,只在 @blur / 显式同步时调用。
 * Edit 区有未同步修改(yamlDirty)时以 Edit 区为准:先把其内容并回表单,
 * 但不覆盖编辑器文本(避免表单操作覆盖用户在 YAML 中的独立编辑)。 */
function formToYaml() {
  if (yamlDirty) {
    yamlToForm()
    return
  }
  const v = ++stateVersion
  const y = serializeForYaml()
  if (v !== stateVersion) return // 期间有更新的同步,丢弃本任务
  settingYaml = true
  yamlText.value = y
  nextTick(() => { settingYaml = false })
}

/** YAML → 表单。语法错误时不覆盖 formState(docs §7.2)。 */
function yamlToForm() {
  const v = ++stateVersion
  let s: ComposeState
  try {
    s = parseCompose(yamlText.value)
  } catch {
    return // 语法错误:表单维持上一次成功解析的状态
  }
  if (v !== stateVersion) return
  formState.value = normalizeServiceKeys(s)
  yamlDirty = false
  if (!formState.value.services[activeService.value]) activeService.value = Object.keys(formState.value.services)[0] || ''
  reloadRows()
}

/** 编辑器 onChange → 400ms 防抖后同步到表单。 */
let editorDebounce: number | undefined
function scheduleYamlToForm() {
  if (editorDebounce) clearTimeout(editorDebounce)
  editorDebounce = window.setTimeout(yamlToForm, 400)
}

function currentYAML(): string {
  // Edit 区有未同步改动时以 Edit 区为权威:原样保存,并同步回表单供后续操作。
  // 这样用户在 YAML 中独立编辑的内容(含表单未覆盖的字段)不会被表单序列化丢弃。
  if (yamlDirty) {
    yamlToForm()
    return yamlText.value
  }
  return serializeForYaml()
}

watch(() => props.show, async (v) => {
  if (v) {
    loading.value = true
    validateError.value = ''
    deployLines.value = []
    deployError.value = ''
    deployDone.value = false
    // 上一会话可能残留「未同步」标记,打开新项目前必须重置,
    // 否则 formToYaml 的 dirty 分支会阻止新内容写入编辑器。
    yamlDirty = false
    projectName.value = props.project || ''
    displayName.value = ''
    try {
      let yaml = ''
      if (props.project) {
        const d = await getCompose(props.project)
        displayName.value = d.displayName
        projectDir.value = d.projectDir || ''
        yaml = d.yaml
      } else {
        projectDir.value = ''
        yaml = defaultTemplate()
      }
      formState.value = normalizeServiceKeys(parseCompose(yaml))
      activeService.value = Object.keys(formState.value.services)[0] || ''
      reloadRows()
      // 托管且可编辑:立即把规范化(键=应用名)后的 YAML 同步到编辑器;
      // 外部只读项目保留原始文件内容展示。
      if (props.readOnly) yamlText.value = yaml
      else formToYaml()
    } catch (e: any) {
      messageApi.error('读取配置失败 — ' + e.message)
      formState.value = { services: {} }
    } finally {
      loading.value = false
    }
    await nextTick()
    editorRef.value?.initEditor()
    loadContainerVars()
    // 拉取系统信息与已有镜像/域名列表(镜像选择、域名访问、资源上限用)
    dockerInfo().then((i) => { info.value = i }).catch(() => {})
    listImages().then((imgs) => { images.value = imgs }).catch(() => {})
    listDomains().then((ds) => { domains.value = ds }).catch(() => {})
    listFragments().then((fs) => { fragList.value = fs }).catch(() => {})
    loadPortProtocols()
    listCapabilities('proxy-protocols').then((caps) => { capabilities.value = caps }).catch(() => {})
  } else {
    closeDeploy()
  }
})

// --- 服务增删 ---

function addService() {
  let n = 1
  while (formState.value.services[`Ser-${n}`]) n++
  const name = `Ser-${n}`
  formState.value.services[name] = { image: '' }
  activeService.value = name
  formToYaml()
}

function removeService(name: string) {
  delete formState.value.services[name]
  if (activeService.value === name) activeService.value = Object.keys(formState.value.services)[0] || ''
  formToYaml()
}

/** 应用名称(container_name)变化时,把 services 键同步改为新名字(维持 compose 语义)。
 * 同时更新其它服务的 depends_on 引用与 activeService。 */
/** 服务键与应用名统一:services 键 == container_name(应用名)。无应用名保留原键(Ser-N)。
 *  用于解析后与序列化时,使键随应用名变化(仅改键名,不改 service 内容)。 */
function normalizeServiceKeys(s: ComposeState['services']): ComposeState['services'] {
  const rename = new Map<string, string>()
  const used = new Set<string>()
  for (const old of Object.keys(s)) {
    const cn = (s[old].container_name || '').trim()
    let nk = old
    if (cn && cn !== old && /^[a-zA-Z0-9][a-zA-Z0-9._-]*$/.test(cn)) nk = cn
    while (used.has(nk)) nk = nk + '_'
    used.add(nk)
    if (nk !== old) rename.set(old, nk)
  }
  if (rename.size === 0) return s
  const next = {} as ComposeState['services']
  for (const old of Object.keys(s)) {
    const nk = rename.get(old) || old
    const svc = s[old]
    svc.container_name = nk // 键==应用名,保持一致
    if (svc.depends_on?.length) svc.depends_on = svc.depends_on.map((d: string) => rename.get(d) || d)
    next[nk] = svc
  }
  return next
}

function renameService(oldKey: string, newKey: string) {
  const n = (newKey || '').trim()
  if (n === oldKey) return
  if (!n) return // 空:保留 Ser-N 键,不重命名
  if (!/^[a-zA-Z0-9][a-zA-Z0-9._-]*$/.test(n)) {
    messageApi.warning(`「${n}」不适合作服务键(仅允许字母数字 _ . -)`)
    formState.value.services[oldKey].container_name = oldKey
    return
  }
  if (formState.value.services[n]) {
    messageApi.warning(`已有服务名「${n}」`)
    formState.value.services[oldKey].container_name = oldKey
    return
  }
  const next: ComposeState['services'] = {}
  for (const k of Object.keys(formState.value.services)) next[k === oldKey ? n : k] = formState.value.services[k]
  // container_name 与键保持一致,避免 YAML 里键名与 container_name 不一致
  next[n].container_name = n
  for (const k of Object.keys(next)) {
    const svc = next[k]
    if (svc.depends_on?.length) svc.depends_on = svc.depends_on.map((d: string) => (d === oldKey ? n : d))
  }
  formState.value.services = next
  if (activeService.value === oldKey) activeService.value = n
  formToYaml()
}

// --- 校验 / 保存 / 部署 ---

async function doValidate() {
  const project = projectName.value.trim() || props.project || ''
  if (!project) {
    messageApi.warning('请填写 projectName')
    return
  }
  validating.value = true
  validateError.value = ''
  validateWarnings.value = []
  try {
    const res = await validateCompose({ project, yaml: currentYAML() })
    validateWarnings.value = res.warnings || []
    if (res.warnings?.length) {
      messageApi.warning(`校验通过，但有 ${res.warnings.length} 条警告`)
    } else {
      messageApi.success('校验通过')
    }
  } catch (e: any) {
    validateError.value = e.message
    messageApi.error('校验失败')
  } finally {
    validating.value = false
  }
}

async function doSave(): Promise<string | null> {
  const yaml = currentYAML()
  if (!yaml.trim()) {
    messageApi.warning('内容不能为空')
    return null
  }
  saving.value = true
  try {
    if (isNew.value) {
      const p = projectName.value.trim()
      if (!p) {
        messageApi.warning('请填写项目名称')
        return null
      }
      await createCompose(p, { displayName: displayName.value, yaml })
      projectName.value = p
    } else {
      await saveCompose(props.project!, { displayName: displayName.value, yaml })
    }
    emit('saved')
    return projectName.value
  } catch (e: any) {
    validateError.value = e.message
    messageApi.error('保存失败 — ' + e.message)
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
      messageApi.success('部署完成')
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
  stopDrag()
  closeDeploy()
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

// --- 结构化控件:端口映射 / 卷挂载 / 环境变量(P3) ---

interface PortRow { host: string; container: string; protocol: string }
interface MountRow { name: string; target: string; mode: 'default' | 'custom' | 'volume'; readonly: boolean }

const protocolOptions = [
  { label: 'tcp', value: 'tcp' },
  { label: 'udp', value: 'udp' },
]
const mountModeOptions = [
  { label: '默认路径', value: 'default' },
  { label: '自定义路径', value: 'custom' },
  { label: '存储卷', value: 'volume' },
]
const rwOptions = [
  { label: '读写', value: 'rw' },
  { label: '只读', value: 'ro' },
]

function parsePortRow(s: string): PortRow {
  const [protoPart, protocol = 'tcp'] = s.split('/')
  const parts = protoPart.split(':')
  if (parts.length === 1) return { host: '', container: parts[0], protocol }
  if (parts.length === 2) return { host: parts[0], container: parts[1], protocol }
  return { host: `${parts[0]}:${parts[1]}`, container: parts[2], protocol }
}
function serializePortRow(r: PortRow): string {
  let s = r.host ? `${r.host}:${r.container}` : r.container
  if (r.protocol && r.protocol !== 'tcp') s += `/${r.protocol}`
  return s
}
function parseMountRow(s: string): MountRow {
  const parts = s.split(':')
  const src = parts[0] || ''
  const target = parts[1] || ''
  const readonly = parts.length >= 3 && parts[2].includes('ro')
  if (src === '') return { name: '', target, mode: 'default', readonly }
  // 三分类规则(避免读取后凭猜测改写原地址):
  // 1. 默认路径:YAML 已用变量 ${GB_PROJ_FILE} 记录;或相对路径落于项目目录(./<项目目录>/…),
  //    统一用变量保存(剥前缀→文件夹名)
  if (src.startsWith('${GB_PROJ_FILE}')) return { name: src.slice('${GB_PROJ_FILE}'.length).replace(/^\/+|\/+$/g, ''), target, mode: 'default', readonly }
  if (src.startsWith('./') || src.startsWith('../')) {
    // 相对路径:去掉 ./ 前缀后若落在项目目录内 → 默认路径(用变量保存)
    const rel = src.replace(/^\.\.?\/+/, '')
    const pdir = (projectDir.value || '').replace(/^\.\.?\/+/, '')
    if (pdir && (rel === pdir || rel.startsWith(pdir + '/'))) {
      const name = rel.slice(pdir.length).replace(/^\/+|\/+$/g, '')
      return { name, target, mode: 'default', readonly }
    }
    // 项目目录之外的相对路径(如 ../shared)无法用变量表达,原样保留
    return { name: src, target, mode: 'custom', readonly }
  }
  // 2. 自定义:以 / 开头的绝对路径,原样保留
  if (src.startsWith('/')) return { name: src, target, mode: 'custom', readonly }
  // 3. 存储卷:字母/数字开头的卷名,原样保留
  return { name: src, target, mode: 'volume', readonly }
}
function serializeMountRow(r: MountRow): string {
  // 容器内路径未填的挂载行不产出
  if (!r.target) return ''
  let src: string
  if (r.mode === 'default') {
    // 默认路径 = 项目默认地址变量 + 文件夹名:${GB_PROJ_FILE}/<name>/(GB_PROJ_FILE 不含结尾 /)
    const name = r.name.replace(/^\/+|\/+$/g, '')
    src = name ? `\${GB_PROJ_FILE}/${name}/` : `\${GB_PROJ_FILE}/`
  } else {
    src = r.name // 自定义(绝对路径/相对路径)与存储卷均原样写入
  }
  let s = `${src}:${r.target}`
  if (r.readonly) s += ':ro'
  return s
}

function curService() {
  return formState.value.services[activeService.value]
}

function portRows(): PortRow[] { return (curService()?.ports || []).map(parsePortRow) }
function portUpdate(i: number, patch: Partial<PortRow>) {
  const s = curService()
  if (!s) return
  const rows = (s.ports || []).map(parsePortRow)
  rows[i] = { ...rows[i], ...patch }
  s.ports = rows.map(serializePortRow)
}
function portAdd() { const s = curService(); if (s) { s.ports = [...(s.ports || []), ':']; formToYaml() } }
function portRemove(i: number) { const s = curService(); if (s) { s.ports = (s.ports || []).filter((_, idx) => idx !== i); formToYaml() } }

// 挂载 / 环境变量用本地行列表:支持空值、空键,挂载模式显式保存(避免空源时模式被重新推导)
const envList = ref<{ key: string; value: string }[]>([])
const mountList = ref<MountRow[]>([])

function reloadRows() {
  const s = curService()
  envList.value = Object.entries(s?.environment || {}).map(([key, value]) => ({ key, value }))
  mountList.value = (s?.volumes || []).map(parseMountRow)
  // 解析后立即回写:相对项目目录的挂载规整为 ${GB_PROJ_FILE} 变量保存
  syncMount()
  reloadCommand()
  reloadCapAdd()
  reloadDomain()
  reloadRelation()
  initRunDefaults()
}

// 运行配置默认值:仅在 formState 层面给出渲染 fallback(logging)——
// 不自动向 YAML 注入 healthcheck(镜像可能无 curl/wget,避免误判 unhealthy)。
// 健康检查由用户显式配置(运行配置→健康检查,或 YAML 手写)。
function initRunDefaults() {
  const s = curService()
  if (!s) return
  if (!s.logging) s.logging = { driver: 'json-file', options: { 'max-size': '2m', 'max-file': '5' } }
  // shm_size 等于 docker 默认值 64m,不填(ADR-017)
}
function syncEnv() {
  const s = curService()
  if (!s) return
  const env: Record<string, string> = {}
  for (const r of envList.value) if (r.key) env[r.key] = r.value
  s.environment = env
}
function syncMount() {
  const s = curService()
  if (!s) return
  s.volumes = mountList.value.map(serializeMountRow).filter(Boolean)
}
function envAdd() { envList.value.push({ key: '', value: '' }); syncEnv(); formToYaml() }
function envRemove(i: number) { envList.value.splice(i, 1); syncEnv(); formToYaml() }
function mountAdd() { mountList.value.push({ name: '', target: '', mode: 'default', readonly: false }); syncMount(); formToYaml() }
function mountRemove(i: number) { mountList.value.splice(i, 1); syncMount(); formToYaml() }

// command / cap_add 逐条添加（类似环境变量的交互模式）
const commandList = ref<string[]>([])
const capAddList = ref<string[]>([])

function reloadCommand() { commandList.value = [...(curService()?.command || [])] }
function reloadCapAdd() { capAddList.value = [...(curService()?.cap_add || [])] }
function syncCommand() {
  const s = curService()
  if (!s) return
  s.command = commandList.value.filter(Boolean)
}
function syncCapAdd() {
  const s = curService()
  if (!s) return
  s.cap_add = capAddList.value.filter(Boolean)
}
function commandAdd() { commandList.value.push(''); syncCommand(); formToYaml() }
function commandRemove(i: number) { commandList.value.splice(i, 1); syncCommand(); formToYaml() }
function capAddAdd() { capAddList.value.push(''); syncCapAdd(); formToYaml() }
function capAddRemove(i: number) { capAddList.value.splice(i, 1); syncCapAdd(); formToYaml() }

// 域名访问:本地行列表。每个域名行独立绑定「指向端口」与「Caddy 片段」(ADR-026 修订:
// 同一容器经不同域名发布不同端口/片段的服务)。
// 协议/访问端口编码进 site 地址;指向端口 → caddy[.N].reverse_proxy: "{{upstreams N}}";
// 片段 → gatebox.fragments[_N]。
interface DomainAccessRow {
  subMode: 'default' | 'custom' // 子域名:默认(应用名)/自定义
  subdomain: string
  rootDomain: string
  protocols: string[] // 支持的多协议(引用「网关→端口」启用协议;每个协议下所有端口均可用)
  upstream: string // 指向端口:该服务「端口映射」中的容器内部端口(空 = 未指定)
  fragSel: string[] // 行级 Caddy 片段名
}
const domainList = ref<DomainAccessRow[]>([])
const domainOptions = computed(() => domains.value.map((d) => ({ label: d.name, value: d.name })))
// 协议下拉动态引用「网关 → 端口」的协议记录;非 http/https 需能力提供者支持(ADR-036)。
const portProtocols = ref<PortBinding[]>([])
const capabilities = ref<Capability[]>([])
const nonHttpEnabled = computed(() => hasNonHTTPProtocol(capabilities.value))
const protoOptions = computed(() =>
  portProtocols.value
    .filter((p) => p.enabled)
    .map((p) => {
      const http = p.protocol === 'http' || p.protocol === 'https'
      const enabled = http || nonHttpEnabled.value
      const net = ({ udp: 'UDP', both: 'TCP & UDP' } as Record<string, string>)[p.network || 'tcp'] || 'TCP'
      return {
        label: http
          ? `${p.description}（${p.ports.join(' / ')}）`
          : `${p.description}（${net} · ${p.ports.join(' / ')}）${enabled ? '' : ' · 需启用 TCP/UDP 扩展'}`,
        value: p.protocol,
        disabled: !enabled,
      }
    }),
)
async function loadPortProtocols() {
  try {
    portProtocols.value = await listPorts()
  } catch {
    portProtocols.value = []
  }
}

// 「添加协议」:走网关→端口创建(新增协议后即可在域名访问中选择)
const protoModalShow = ref(false)
const protoSaving = ref(false)
const protoForm = ref({ protocol: '', description: '', ports: [] as (number | string)[], enabled: true })
function openNewProtocol() {
  protoForm.value = { protocol: '', description: '', ports: [], enabled: true }
  protoModalShow.value = true
}
// parseProtoPorts 归一化 tags 输入(字符串/数字)为升序端口;校验 1~65535 且无重复。
function parseProtoPorts(list: (number | string)[]): { ok: boolean; ports: number[]; msg: string } {
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
async function saveNewProtocol() {
  const f = protoForm.value
  if (!/^[a-z][a-z0-9]{0,31}$/.test(f.protocol)) {
    messageApi.warning('协议名需为小写字母/数字')
    return
  }
  const parsed = parseProtoPorts(f.ports)
  if (!parsed.ok) {
    messageApi.warning(parsed.msg)
    return
  }
  protoSaving.value = true
  try {
    await createPort({ protocol: f.protocol, description: f.description, ports: parsed.ports, enabled: f.enabled })
    messageApi.success('协议已添加，可在域名访问中选择')
    protoModalShow.value = false
    await loadPortProtocols()
  } catch (e: any) {
    messageApi.error(e.message)
  } finally {
    protoSaving.value = false
  }
}

// 每行「指向端口」下拉选项:仅该服务「端口映射」中的容器内部端口(无自动/自定义)。
function rowUpstreamOptions(): { label: string; value: string }[] {
  const opts: { label: string; value: string }[] = []
  for (const p of portRows()) {
    if (!p.container) continue
    const hostLabel = p.host ? `:${p.host}` : '(随机)'
    opts.push({ label: `宿主${hostLabel} → 容器 ${p.container}`, value: p.container })
  }
  return opts
}

// Caddy 片段备选(组件级,按名引用)。
const fragList = ref<FragmentView[]>([])
const fragOptions = computed(() => fragList.value.map((f) => ({ label: f.name, value: f.name })))
// 默认启用(且非隐藏)的片段名——新建域名行预勾选,与「创建代理」的 preselect 一致。
function defaultFragNames(): string[] {
  return fragList.value.filter((f) => f.defaultEnabled && !f.defaultHidden).map((f) => f.name)
}

// upstreamTemplateRe 提取 {{upstreams [https] <port>}} 中的容器内部端口(回显用)。
const upstreamTemplateRe = /\{\{\s*upstreams(?:\s+(https?))?(?:\s+(\d+))?\s*\}\}/

function reloadDomain() {
  const s = curService()
  const routes = s?.caddyRoutes || []
  const byHost = new Map<string, DomainAccessRow>()
  const order: string[] = []
  for (const r of routes) {
    const host = (r.domain || '').replace(/^https?:\/\//, '')
    const rootD = longestRootDomain(host)
    const sub = rootD ? host.slice(0, host.length - rootD.length - 1) : (host.split('.')[0] || '')
    const root = rootD || host.split('.').slice(1).join('.')
    let row = byHost.get(host)
    if (!row) {
      row = { subMode: 'custom', subdomain: sub, rootDomain: root, protocols: [], upstream: '', fragSel: [] }
      byHost.set(host, row)
      order.push(host)
    }
    // 同一域名支持多协议:http/https 及其端口并到一行
    const proto = r.proto || 'https'
    if (!row.protocols.includes(proto)) row.protocols.push(proto)
    // 行级指向端口与片段(同域名多协议共享,取首个非空)
    const m = r.upstreamRef ? upstreamTemplateRe.exec(r.upstreamRef) : null
    if (m && m[2]) row.upstream = m[2]
    if (r.fragmentNames?.length && row.fragSel.length === 0) row.fragSel = [...r.fragmentNames]
  }
  domainList.value = order.map((h) => byHost.get(h)!)
}
function syncDomain() {
  const s = curService()
  if (!s) return
  // 每(域名,协议)一条路由(协议下所有端口由网关端口表驱动全部监听,无需选端口)
  const routes: CaddyRoute[] = []
  for (const r of domainList.value) {
    const host = [(r.subMode === 'default' ? appName() : r.subdomain), r.rootDomain].filter(Boolean).join('.')
    for (const proto of r.protocols) {
      const route: CaddyRoute = { domain: host, proto }
      if (r.upstream) route.upstreamRef = `{{upstreams ${r.upstream}}}`
      if (r.fragSel.length) route.fragmentNames = [...r.fragSel]
      routes.push(route)
    }
  }
  s.caddyRoutes = routes
}

// 行摘要:该域名的协议与端口(引用网关端口表全部端口)。
function domainSummary(r: DomainAccessRow): string {
  return r.protocols
    .map((proto) => {
      const p = portProtocols.value.find((x) => x.protocol === proto)
      const ports = p && p.enabled && p.ports.length ? p.ports.join('/') : (proto === 'http' ? '80' : '443')
      return `${proto}://${r.subMode === 'default' ? appName() : r.subdomain}${r.rootDomain ? '.' + r.rootDomain : ''}（${ports}）`
    })
    .join(' · ')
}
function domainAdd() {
  // 指向端口只能从本服务「端口映射」中选择;默认取首个映射的容器内部端口。
  const first = portRows().find((p) => p.container)
  domainList.value.push({ subMode: 'default', subdomain: '', rootDomain: '', protocols: ['https'], upstream: first?.container || '', fragSel: defaultFragNames() })
  syncDomain()
  formToYaml()
}
function domainRemove(i: number) { domainList.value.splice(i, 1); syncDomain(); formToYaml() }

// 片段列表异步加载完成后,给仍为空的行补上默认启用片段(与创建代理一致)。
watch(fragList, () => {
  const d = defaultFragNames()
  if (!d.length) return
  let changed = false
  domainList.value.forEach((r) => { if (!r.fragSel.length) { r.fragSel = [...d]; changed = true } })
  if (changed) { syncDomain(); formToYaml() }
})

// longestRootDomain 按受管域名列表做最长后缀匹配(与后端 DERIVE matchRootDomain 对齐)。
function longestRootDomain(host: string): string {
  let best = ''
  for (const d of domains.value) {
    const name = d.name
    if ((host === name || host.endsWith('.' + name)) && name.length > best.length) best = name
  }
  return best
}
function healthCheckPlaceholder(): string {
  // 约定变量名:${GB_SER_<服务No>_PORT_<端口No>}
  return `curl -f http://localhost:${gbVar(`SER_${serviceNo(activeService.value)}_PORT_1`)}/ || exit 1`
}

// 镜像选择 / 拉取
const imageSelectShow = ref(false)
const pullShow = ref(false)
const pullName = ref('')
const pullTag = ref('')
const pullArch = ref('')
const pullActive = ref(false)
const pullPercent = ref(0)
const pullStatus = ref('')
let pullSocket: WebSocket | null = null

function openImageSelect() { imageSelectShow.value = true }
function selectImage(name: string) {
  const s = curService()
  if (s) { s.image = name; formToYaml() }
  imageSelectShow.value = false
}
function openPull() {
  pullName.value = ''
  pullTag.value = ''
  pullArch.value = info.value?.imagePlatform || ''
  pullActive.value = false
  pullPercent.value = 0
  pullStatus.value = ''
  pullShow.value = true
}
function pullRef() {
  const name = pullName.value.trim()
  if (!name) return ''
  const tag = pullTag.value.trim()
  // tag 留空默认 latest,保证 image 始终带 tag(docker 会自己补 :latest,但 Edit 区应明确)
  return tag ? `${name}:${tag}` : `${name}:latest`
}
function startPull() {
  const ref = pullRef()
  if (!ref) { messageApi.warning('请填写镜像名'); return }
  if (pullActive.value) return
  pullActive.value = true
  pullPercent.value = 0
  pullStatus.value = '正在连接…'
  pullSocket = new WebSocket(wsURL('/docker/images/pull', { ref, platform: pullArch.value.trim() }))
  pullSocket.onmessage = (ev) => {
    const p = JSON.parse(ev.data) as any
    if (p.error) { messageApi.error('拉取失败 — ' + p.error); closePull(); return }
    pullPercent.value = p.percent
    pullStatus.value = p.status
    if (p.done) {
      messageApi.success('镜像拉取完成')
      const s = curService()
      if (s) { s.image = ref; formToYaml() }
      closePull()
      listImages().then((imgs) => { images.value = imgs }).catch(() => {})
    }
  }
  pullSocket.onerror = () => { messageApi.error('拉取连接失败'); closePull() }
}
function closePull() {
  pullActive.value = false
  if (pullSocket) { pullSocket.onmessage = null; pullSocket.close(); pullSocket = null }
  pullShow.value = false
}

// 关联配置:启动依赖(depends_on)/网络/宿主机 hosts/设备
const networkJoinShow = ref(false)
const networksAll = ref<NetworkView[]>([])
const networkList = ref<{ name: string; alias: string; ipv4: string }[]>([])
const hostList = ref<{ hostname: string; ip: string }[]>([])
const deviceList = ref<{ hostPath: string; containerPath: string }[]>([])

const serviceOptions = computed(() =>
  Object.keys(formState.value.services)
    .filter((n) => n !== activeService.value)
    .map((n) => ({ label: formState.value.services[n].container_name || n, value: n })),
)

function reloadRelation() {
  const s = curService()
  networkList.value = (s?.networks || []).map((n) => ({ name: n.name, alias: (n.aliases || []).join(','), ipv4: n.ipv4 || '' }))
  hostList.value = (s?.extra_hosts || []).map((h) => { const i = h.indexOf(':'); return { hostname: i >= 0 ? h.slice(0, i) : h, ip: i >= 0 ? h.slice(i + 1) : '' } })
  deviceList.value = (s?.devices || []).map((d) => { const i = d.indexOf(':'); return { hostPath: i >= 0 ? d.slice(0, i) : d, containerPath: i >= 0 ? d.slice(i + 1) : '' } })
}
function syncNetworks() {
  const s = curService()
  if (!s) return
  s.networks = networkList.value.map((n) => ({ name: n.name, aliases: n.alias ? [n.alias] : undefined, ipv4: n.ipv4 || undefined }))
}
function syncHosts() {
  const s = curService()
  if (!s) return
  s.extra_hosts = hostList.value.filter((h) => h.hostname).map((h) => `${h.hostname}:${h.ip}`)
}
function syncDevices() {
  const s = curService()
  if (!s) return
  s.devices = deviceList.value.filter((d) => d.hostPath && d.containerPath).map((d) => `${d.hostPath}:${d.containerPath}`)
}
function openNetworkJoin() {
  listNetworks().then((ns) => { networksAll.value = ns; networkJoinShow.value = true }).catch((e) => messageApi.error('读取网络失败 — ' + e.message))
}
function networkAdd(name: string) { networkList.value.push({ name, alias: '', ipv4: '' }); syncNetworks(); formToYaml(); networkJoinShow.value = false }
function networkRemove(i: number) { networkList.value.splice(i, 1); syncNetworks(); formToYaml() }
function hostAdd() { hostList.value.push({ hostname: '', ip: '' }); syncHosts(); formToYaml() }
function hostRemove(i: number) { hostList.value.splice(i, 1); syncHosts(); formToYaml() }
function deviceAdd() { deviceList.value.push({ hostPath: '', containerPath: '' }); syncDevices(); formToYaml() }
function deviceRemove(i: number) { deviceList.value.splice(i, 1); syncDevices(); formToYaml() }
function networkSubnet(name: string): string { return networksAll.value.find((n) => n.name === name)?.subnet || '-' }

watch(activeService, reloadRows)

// 内存上限(字节 → GB),资源限制展示用
function fmtMemGB(): string {
  const b = info.value?.memTotal
  if (!b) return '-'
  return (b / 1024 / 1024 / 1024).toFixed(1)
}
</script>

<template>
  <a-drawer
    :open="show"
    placement="right"
    :width="1400"
    :mask-closable="!deploying"
    :body-style="{ padding: 0 }"
    @close="emit('update:show', false)"
  >
    <template #title>
      <span class="dw-drawer-title">{{ isNew ? '创建应用' : `编辑 ${displayName || projectName}` }}</span>
    </template>
    <contextHolder />
    <div style="display: flex; flex-direction: column; height: 100%">
      <div style="padding: 6px 24px; border-bottom: 1px solid #eee; display: flex; align-items: center; justify-content: flex-end; gap: 8px; flex-shrink: 0">
        <a-button size="small" :type="showForm ? 'primary' : 'default'" ghost @click="showForm = !showForm">
          {{ showForm ? '隐藏表单' : '显示表单' }}
        </a-button>
        <a-button size="small" :type="showYaml ? 'primary' : 'default'" ghost @click="showYaml = !showYaml">
          {{ showYaml ? '隐藏 YAML' : '显示 YAML' }}
        </a-button>
      </div>
      <div style="flex: 1; overflow: hidden; display: flex; flex-direction: column; gap: 8px; padding: 16px 24px">
      <div style="display: flex; gap: 12px">
        <div style="flex: 1">
          <div class="dw-label">项目名称<span class="dw-required">*</span><span style="color: #999; font-size: 12px">（{{ isNew ? '创建后不可改' : '只读' }}）</span></div>
          <a-input v-model:value="projectName" :disabled="!isNew" placeholder="my-app" :status="isNew && !projectName.trim() ? 'error' : ''" />
        </div>
        <div style="flex: 1">
          <div class="dw-label">展示名</div>
          <a-input v-model:value="displayName" :disabled="readOnly" placeholder="家庭影院" />
        </div>
      </div>

      <a-alert v-if="validateError" type="error" show-icon style="margin-bottom: 0">
        {{ validateError }}
      </a-alert>
      <a-alert v-if="validateWarnings.length" type="warning" show-icon style="margin-bottom: 0">
        <div v-for="(w, i) in validateWarnings" :key="i">{{ w }}</div>
      </a-alert>
      <a-alert v-if="readOnly" type="info" show-icon>
        外部编排只读。编辑需在源文件中进行或先「接管」。
      </a-alert>

      <!-- 主区域:表单 | 分割线 | YAML -->
      <div ref="mainAreaEl" style="flex: 1; min-height: 0; display: flex">
        <!-- 表单区 -->
        <div
          v-show="showForm"
          :style="formAreaStyle"
          style="min-width: 0; border: 1px solid #e0e0e0; display: flex; flex-direction: column; overflow: hidden"
        >
          <!-- 服务 tab 栏(选中 tab 白底连体、未选中灰底、+服务虚线框,下方有分隔线) -->
          <div style="display: flex; align-items: flex-end; gap: 4px; padding: 6px 8px 0; border-bottom: 1px solid #e0e0e0; background: #fafafa">
            <div
              v-for="(svc, name) in formState.services"
              :key="name"
              @click="activeService = name"
              :style="activeService === name
                ? { border: '1px solid #e0e0e0', borderBottom: '1px solid #fff', marginBottom: '-1px', background: '#fff', color: '#333', fontWeight: '600' }
                : { border: '1px solid #e0e0e0', borderBottom: 'none', background: '#f0f0f0', color: '#666' }"
              style="display: inline-flex; align-items: center; gap: 6px; padding: 5px 10px; border-radius: 4px 4px 0 0; cursor: pointer; font-size: 13px; flex-shrink: 0"
            >
              <span>{{ formState.services[name].container_name || name }}</span>
              <a-popconfirm v-if="editable" :title="`确认删除服务 ${name}？`" @confirm="removeService(name)">
                <span @click.stop style="font-size: 11px; line-height: 1; opacity: 0.7">✕</span>
              </a-popconfirm>
            </div>
            <div
              v-if="editable"
              @click="addService"
              style="padding: 5px 10px; border: 1px dashed #d0d0d0; border-radius: 4px 4px 0 0; cursor: pointer; color: #666; font-size: 13px; flex-shrink: 0"
            >+ 服务</div>
          </div>
          <!-- 表单内容 -->
          <div v-if="activeService && formState.services[activeService]" style="flex: 1; overflow: auto; padding: 10px 12px; display: flex; flex-direction: column; gap: 8px">

          <div>
            <div class="dw-section">基本信息</div>
            <div>
              <div class="dw-label">镜像</div>
              <div style="display: flex; gap: 6px; align-items: center">
                <a-input size="small" :value="formState.services[activeService].image" disabled placeholder="选择或拉取镜像" style="flex: 1" />
                <a-button size="small" :disabled="!editable" @click="openImageSelect">选择镜像</a-button>
                <a-button size="small" :disabled="!editable" @click="openPull">拉取镜像</a-button>
              </div>
            </div>
            <div style="display: flex; gap: 10px; margin-top: 8px">
              <div style="flex: 1">
                <div class="dw-label">应用名称(容器名)</div>
                <a-input size="small" :value="formState.services[activeService].container_name" :disabled="!editable" :placeholder="`Ser-${serviceNo(activeService.value)}（应用名）`" @update:value="(v: any) => { formState.services[activeService].container_name = v; formToYaml() }" @blur="() => renameService(activeService.value, formState.services[activeService].container_name)" />
              </div>
              <div style="flex: 1">
                <div class="dw-label">重启策略</div>
                <a-select size="small" v-model:value="formState.services[activeService].restart" :options="restartOptions" :disabled="!editable" style="width: 100%" @blur="formToYaml" />
              </div>
            </div>
          </div>

          <!-- 二级 tab:基本配置/运行配置/关联配置 -->
          <div style="display: flex; gap: 0; border-bottom: 1px solid #e0e0e0">
            <div
              v-for="t in configTabs"
              :key="t.key"
              @click="configTab = t.key"
              :style="configTab === t.key ? { color: '#1677ff', fontWeight: '600', borderBottom: '2px solid #1677ff' } : { color: '#666' }"
              style="padding: 6px 14px; cursor: pointer; font-size: 14px; margin-bottom: -1px"
            >{{ t.label }}</div>
          </div>

          <!-- 基本配置 -->
          <div v-if="configTab === 'basic'" style="display: flex; flex-direction: column; gap: 8px">

            <div>
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">环境变量</div>
                <a-button v-if="editable" size="small" type="primary" @click="envAdd">+ 添加变量</a-button>
              </div>
              <div v-for="(row, i) in envList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <a-input size="small" v-model:value="row.key" :disabled="!editable" placeholder="变量名" style="flex: 1" @blur="() => { syncEnv(); formToYaml() }" />
                <a-input size="small" v-model:value="row.value" :disabled="!editable" placeholder="变量值" style="flex: 1" @blur="() => { syncEnv(); formToYaml() }" />
                <a-button v-if="editable" size="small" type="text" danger @click="envRemove(i)">✕</a-button>
              </div>
            </div>

            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">挂载或存储卷</div>
                <a-button v-if="editable" size="small" type="primary" @click="mountAdd">+ 添加目录</a-button>
              </div>
              <div v-for="(row, i) in mountList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <a-select size="small" v-model:value="row.mode" :options="mountModeOptions" :disabled="!editable" style="width: 100px" @change="() => { syncMount(); formToYaml() }" />
                <a-input size="small" v-model:value="row.name" :disabled="!editable" :placeholder="row.mode === 'default' ? '文件夹名，如 logs → ' + gbVar('PROJ_FILE') + '/logs/' : row.mode === 'custom' ? '绝对路径，如 /var/run/docker.sock' : '卷名'" style="flex: 1" @blur="() => { syncMount(); formToYaml() }" />
                <span style="color: #999">:</span>
                <a-input size="small" v-model:value="row.target" :disabled="!editable" placeholder="容器内路径" style="flex: 1" @blur="() => { syncMount(); formToYaml() }" />
                <a-select size="small" :value="row.readonly ? 'ro' : 'rw'" :options="rwOptions" :disabled="!editable" style="width: 80px" @change="(v: any) => { row.readonly = v === 'ro'; syncMount(); formToYaml() }" />
                <a-button v-if="editable" size="small" type="text" danger @click="mountRemove(i)">✕</a-button>
              </div>
            </div>

            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">端口映射</div>
                <a-button v-if="editable" size="small" type="primary" @click="portAdd">+ 添加端口</a-button>
              </div>
              <div v-for="(row, i) in portRows()" :key="i" style="margin-bottom: 4px">
                <div style="display: flex; gap: 6px; align-items: center">
                  <!-- 端口映射前标数字(端口序号),用于对照变量排号 -->
                  <span style="font-size: 11px; font-family: monospace; color: #bbb; width: 18px; text-align: right; flex-shrink: 0">{{ i + 1 }}</span>
                  <a-input size="small" :value="row.host" :disabled="!editable" placeholder="外部端口(留空随机)" style="flex: 1" @update:value="(v: any) => portUpdate(i, { host: v })" @blur="formToYaml" />
                  <span style="color: #999">:</span>
                  <a-input size="small" :value="row.container" :disabled="!editable" placeholder="内部端口" style="flex: 1" @update:value="(v: any) => portUpdate(i, { container: v })" @blur="formToYaml" />
                  <a-select size="small" :value="row.protocol" :options="protocolOptions" :disabled="!editable" style="width: 80px" @change="(v: any) => { portUpdate(i, { protocol: v }); formToYaml() }" />
                  <a-button v-if="editable" size="small" type="text" danger @click="portRemove(i)">✕</a-button>
                </div>
              </div>
            </div>

            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">域名访问</div>
                <a-button v-if="editable" size="small" type="primary" @click="domainAdd">+ 添加域名访问</a-button>
                <a-button v-if="editable" size="small" @click="openNewProtocol">+ 添加协议</a-button>
              </div>
              <!-- 每个域名行:可多协议(每协议下所有端口均可用),独立绑定指向端口与 Caddy 片段 -->
              <div v-for="(row, i) in domainList" :key="i" style="margin-bottom: 10px; padding: 8px 10px; background: #fafafa; border-radius: 6px">
                <div style="display: flex; gap: 6px; align-items: center; flex-wrap: wrap">
                  <a-select size="small" v-model:value="row.subMode" :options="[{ label: '默认名称', value: 'default' }, { label: '自定义', value: 'custom' }]" :disabled="!editable" style="width: 96px" @change="() => { syncDomain(); formToYaml() }" />
                  <a-input size="small" v-model:value="row.subdomain" :disabled="!editable || row.subMode === 'default'" :placeholder="row.subMode === 'default' ? appName() : '子域名'" style="flex: 1; max-width: 130px" @blur="() => { syncDomain(); formToYaml() }" />
                  <span style="color: #999">.</span>
                  <a-select size="small" v-model:value="row.rootDomain" :options="domainOptions" :disabled="!editable" placeholder="根域名" style="flex: 1; max-width: 150px" @change="() => { syncDomain(); formToYaml() }" />
                  <a-select size="small" v-model:value="row.protocols" mode="multiple" :options="protoOptions" :disabled="!editable" placeholder="访问协议" style="flex: 1; min-width: 220px" @change="() => { syncDomain(); formToYaml() }" />
                  <a-popconfirm v-if="editable" title="确认删除该域名访问？" @confirm="domainRemove(i)">
                      <a-button size="small" type="text" danger>✕</a-button>
                    </a-popconfirm>
                </div>
                <!-- 行级:指向端口 + Caddy 片段(不同域名可发布不同端口/片段的服务) -->
                <div style="display: flex; align-items: center; gap: 10px; margin-top: 6px; flex-wrap: wrap">
                  <div style="display: flex; align-items: center; gap: 6px">
                    <span class="dw-label">指向端口</span>
                    <a-select size="small" v-model:value="row.upstream" :options="rowUpstreamOptions()" :disabled="!editable" placeholder="选择端口映射" style="width: 240px" @change="() => { syncDomain(); formToYaml() }" />
                  </div>
                  <div style="display: flex; align-items: center; gap: 6px; flex: 1; min-width: 200px">
                    <span class="dw-label">Caddy 片段</span>
                    <a-select size="small" mode="multiple" v-model:value="row.fragSel" :options="fragOptions" :disabled="!editable" placeholder="按片段名引用(网关→Caddy片段)" style="flex: 1" @change="() => { syncDomain(); formToYaml() }" />
                  </div>
                </div>
                <!-- 提示移到控件下方:默认子域名/访问地址说明 -->
                <div style="font-size: 12px; color: #888; margin-top: 2px">
                  <span v-if="row.subMode === 'default'">子域名默认=应用名 {{ gbVar(`SER_${serviceNo(activeService.value)}_NAME`) }}（{{ appName() }}）</span>
                  <span v-if="row.protocols.length">· 访问地址 {{ domainSummary(row) }}</span>
                  <span v-if="!row.upstream"> · 指向端口未指定:请选择本服务的端口映射</span>
                </div>
              </div>
            </div>
          </div>

          <!-- 运行配置 -->
          <div v-else-if="configTab === 'runtime'" style="display: flex; flex-direction: column; gap: 8px">
            <div>
              <div class="dw-section">日志配置</div>
              <div style="display: flex; gap: 10px; margin-top: 6px">
                <div style="flex: 1">
                  <div class="dw-label">日志格式</div>
                  <a-select size="small" :value="formState.services[activeService].logging?.driver || 'json-file'" :options="logDriverOptions" :disabled="!editable" @change="(v: any) => { const s = formState.services[activeService]; const lg = s.logging || {}; lg.driver = v; s.logging = lg; formToYaml() }" />
                </div>
                <div style="flex: 1">
                  <div class="dw-label">单文件大小(MiB)</div>
                  <a-input size="small" :value="formState.services[activeService].logging?.options?.['max-size'] || '2'" :disabled="!editable" @update:value="(v: any) => { const s = formState.services[activeService]; const lg = s.logging || {}; lg.options = { ...(lg.options || {}), 'max-size': v }; s.logging = lg; formToYaml() }" />
                </div>
                <div style="flex: 1">
                  <div class="dw-label">最大保留数</div>
                  <a-input size="small" :value="formState.services[activeService].logging?.options?.['max-file'] || '5'" :disabled="!editable" @update:value="(v: any) => { const s = formState.services[activeService]; const lg = s.logging || {}; lg.options = { ...(lg.options || {}), 'max-file': v }; s.logging = lg; formToYaml() }" />
                </div>
              </div>
            </div>

            <div class="cf-sec">
              <div class="dw-section">健康检查</div>
              <div style="display: flex; flex-direction: column; gap: 6px; margin-top: 6px">
                <div class="dw-label">执行脚本</div>
                <a-input size="small" :value="formState.services[activeService].healthcheck?.test" :disabled="!editable" :placeholder="healthCheckPlaceholder()" @update:value="(v: any) => { const hc = formState.services[activeService].healthcheck || {}; hc.test = v; formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                <Typography.Text type="secondary" style="font-size: 12px">
                  默认不注入健康检查(镜像可能无 curl/wget,自动注入会误判 unhealthy);留空则不写 test,仅在镜像内置探测命令时配置。
                </Typography.Text>
                <div style="display: flex; gap: 10px">
                  <div style="flex: 1">
                    <div class="dw-label">间隔(秒)</div>
                    <a-input size="small" :value="formState.services[activeService].healthcheck?.interval || '30'" :disabled="!editable" @update:value="(v: any) => { const hc = formState.services[activeService].healthcheck || {}; hc.interval = v; formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                  </div>
                  <div style="flex: 1">
                    <div class="dw-label">超时(秒)</div>
                    <a-input size="small" :value="formState.services[activeService].healthcheck?.timeout || '10'" :disabled="!editable" @update:value="(v: any) => { const hc = formState.services[activeService].healthcheck || {}; hc.timeout = v; formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                  </div>
                  <div style="flex: 1">
                    <div class="dw-label">重试次数</div>
                    <a-input size="small" :value="formState.services[activeService].healthcheck?.retries ?? '3'" :disabled="!editable" @update:value="(v: any) => { const hc = formState.services[activeService].healthcheck || {}; hc.retries = Number(v); formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                  </div>
                </div>
              </div>
            </div>

            <div class="cf-sec">
              <div class="dw-section">资源限制</div>
              <div style="display: flex; gap: 10px; margin-top: 6px">
                <div style="flex: 1">
                  <div class="dw-label">共享内存(MiB)</div>
                  <a-input size="small" :value="formState.services[activeService].shm_size || ''" :disabled="!editable" placeholder="64" @update:value="(v: any) => { formState.services[activeService].shm_size = v }" @blur="formToYaml" />
                </div>
                <div style="flex: 1">
                  <div class="dw-label">CPU 配额(最大 {{ info?.ncpu || '-' }} 核)</div>
                  <a-input size="small" :value="formState.services[activeService].cpus || ''" :disabled="!editable" placeholder="如 2" @update:value="(v: any) => { formState.services[activeService].cpus = v }" @blur="formToYaml" />
                </div>
                <div style="flex: 1">
                  <div class="dw-label">内存(最大 {{ fmtMemGB() }} G)</div>
                  <a-input size="small" :value="formState.services[activeService].mem_limit || ''" :disabled="!editable" placeholder="如 512m" @update:value="(v: any) => { formState.services[activeService].mem_limit = v }" @blur="formToYaml" />
                </div>
              </div>
            </div>

            <div class="cf-sec">
              <div class="dw-section">用户与权限</div>
              <div style="display: flex; gap: 10px; margin-top: 6px">
                <div style="flex: 1">
                  <div class="dw-label">user(PUID:PGID)</div>
                  <a-input size="small" v-model:value="formState.services[activeService].user" :disabled="!editable" placeholder="如 1000:1000" @blur="formToYaml" style="margin-top: 6px" />
                </div>
                <div style="flex: 1">
                  <div class="dw-label">关联宿主机 PID</div>
                  <div style="display: flex; align-items: center; gap: 6px; margin-top: 6px">
                    <a-switch :checked="formState.services[activeService].pid === 'host'" :disabled="!editable" @change="(v: any) => { formState.services[activeService].pid = v ? 'host' : ''; formToYaml() }" />
                    <Typography.Text type="secondary" style="font-size: 12px">{{ formState.services[activeService].pid === 'host' ? '已开启' : '已关闭' }}</Typography.Text>
                  </div>
                </div>
              </div>
            </div>

            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">启动命令 command</div>
                <a-button v-if="editable" size="small" type="primary" @click="commandAdd">+ 添加参数</a-button>
              </div>
              <div v-for="(row, i) in commandList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <a-input size="small" v-model:value="commandList[i]" :disabled="!editable" :placeholder="i === 0 ? '如 nginx -g daemon off' : '参数 ' + (i + 1)" style="flex: 1" @blur="() => { syncCommand(); formToYaml() }" />
                <a-button v-if="editable" size="small" type="text" danger @click="commandRemove(i)">✕</a-button>
              </div>
              <Typography.Text v-if="commandList.length === 0" type="secondary" style="font-size: 12px; margin-top: 4px">不填写则使用镜像默认入口点</Typography.Text>
            </div>

            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">Linux 能力 cap_add</div>
                <a-button v-if="editable" size="small" type="primary" @click="capAddAdd">+ 添加能力</a-button>
              </div>
              <div v-for="(row, i) in capAddList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <a-input size="small" v-model:value="capAddList[i]" :disabled="!editable" placeholder="如 SYS_ADMIN, NET_ADMIN" style="flex: 1" @blur="() => { syncCapAdd(); formToYaml() }" />
                <a-button v-if="editable" size="small" type="text" danger @click="capAddRemove(i)">✕</a-button>
              </div>
              <Typography.Text v-if="capAddList.length === 0" type="secondary" style="font-size: 12px; margin-top: 4px">添加容器所需的 Linux 能力</Typography.Text>
            </div>
          </div>

          <!-- 关联配置 -->
          <div v-else style="display: flex; flex-direction: column; gap: 8px">
            <div>
              <div class="dw-section">启动依赖 depends_on</div>
              <a-select size="small" multiple :value="formState.services[activeService].depends_on || []" :options="serviceOptions" :disabled="!editable" placeholder="选择本编排内服务" style="margin-top: 6px" @change="(v: any) => { formState.services[activeService].depends_on = v; formToYaml() }" />
            </div>
            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">关联网络</div>
                <a-button v-if="editable" size="small" type="primary" @click="openNetworkJoin">加入现有网络</a-button>
              </div>
              <div v-for="(row, i) in networkList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <a-input size="small" :value="row.name" disabled style="width: 130px" />
                <a-input size="small" v-model:value="row.alias" :disabled="!editable" placeholder="容器别名" style="flex: 1" @blur="() => { syncNetworks(); formToYaml() }" />
                <a-input size="small" v-model:value="row.ipv4" :disabled="!editable" :placeholder="'IPV4 地址（参考段 ' + networkSubnet(row.name) + '）'" style="flex: 1" @blur="() => { syncNetworks(); formToYaml() }" />
                <a-popconfirm v-if="editable" title="确认删除该网络配置？" @confirm="networkRemove(i)">
                  <a-button size="small" type="text" danger>✕</a-button>
                </a-popconfirm>
              </div>
            </div>
            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">关联宿主机网络</div>
                <a-button v-if="editable" size="small" type="primary" @click="hostAdd">+ 添加宿主机网络</a-button>
              </div>
              <div v-for="(row, i) in hostList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <a-input size="small" v-model:value="row.hostname" :disabled="!editable" placeholder="Hostname" style="flex: 1" @blur="() => { syncHosts(); formToYaml() }" />
                <a-input size="small" v-model:value="row.ip" :disabled="!editable" placeholder="IP 地址" style="flex: 1" @blur="() => { syncHosts(); formToYaml() }" />
                <a-popconfirm v-if="editable" title="确认删除该 host？" @confirm="hostRemove(i)">
                  <a-button size="small" type="text" danger>✕</a-button>
                </a-popconfirm>
              </div>
            </div>
            <div class="cf-sec">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <div class="dw-section">关联设备</div>
                <a-button v-if="editable" size="small" type="primary" @click="deviceAdd">+ 添加关联设备</a-button>
              </div>
              <div v-for="(row, i) in deviceList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <a-input size="small" v-model:value="row.hostPath" :disabled="!editable" placeholder="设备路径，如 /dev/tty0" style="flex: 1" @blur="() => { syncDevices(); formToYaml() }" />
                <span style="color: #999">:</span>
                <a-input size="small" v-model:value="row.containerPath" :disabled="!editable" placeholder="容器内路径，如 /dev/tty0" style="flex: 1" @blur="() => { syncDevices(); formToYaml() }" />
                <a-popconfirm v-if="editable" title="确认删除该设备？" @confirm="deviceRemove(i)">
                  <a-button size="small" type="text" danger>✕</a-button>
                </a-popconfirm>
              </div>
            </div>
            <div class="cf-sec">
              <div style="display: flex; gap: 10px">
                <div style="flex: 1">
                  <div class="dw-section">network_mode</div>
                  <a-input v-model:value="formState.services[activeService].network_mode" :disabled="!editable" @blur="formToYaml" style="margin-top: 6px" />
                </div>
              </div>
            </div>
          </div>

          <!-- 未识别字段只读预览 -->
          <a-collapse v-if="formState.services[activeService]._rawConfigs">
            <a-collapse-panel key="raw">
              <template #header>未识别字段（只读，在 YAML 中编辑）</template>
              <pre style="font-size: 12px; background: #f7f7f7; padding: 8px; border-radius: 4px; overflow: auto">{{ JSON.stringify(formState.services[activeService]._rawConfigs, null, 2) }}</pre>
            </a-collapse-panel>
          </a-collapse>
          </div>
        </div>

        <!-- 可拖动分割线 -->
        <div
          v-show="showForm && showYaml"
          @mousedown="startDrag"
          style="width: 7px; flex-shrink: 0; cursor: col-resize; user-select: none; background: #e8e8e8"
        ></div>

        <!-- YAML 区 -->
        <div v-show="showYaml" style="flex: 1; min-width: 0; display: flex; flex-direction: column; overflow: hidden">
          <!-- 容器变量框:可折叠(标题行 + 系统变量 + 分隔线 + 项目变量) -->
          <div style="border: 1px solid #e0e0e6; border-radius: 6px; padding: 4px 10px 6px; margin-bottom: 8px; flex-shrink: 0">
            <div style="display: flex; align-items: center; gap: 6px; margin-bottom: 4px">
              <span style="color: #333; font-size: 12px; font-weight: 600">容器变量</span>
              <Button type="text" size="small" style="font-size: 12px; padding: 0 4px; color: #888" @click="varCollapsed = !varCollapsed">
                {{ varCollapsed ? '▶ 展开' : '▼ 收起' }}
              </Button>
            </div>
            <template v-if="!varCollapsed">
            <div style="display: flex; flex-wrap: wrap; gap: 4px; align-items: center">
              <span style="color: #999; font-size: 12px">系统变量：</span>
              <Tooltip v-for="bv in containerBuiltinVars" :key="bv.key" :title="bv.description">
                <Tag size="small" style="cursor: pointer; font-family: monospace; font-size: 12px; background: #f0f0f0" @click="insertContainerVar(bv.key)">{{ bv.key }}</Tag>
              </Tooltip>
              <Tooltip v-for="v in containerVars" :key="v.key" :title="'值：' + (v.value || '（空）') + (v.description ? ' · ' + v.description : '')">
                <Tag size="small" style="cursor: pointer; font-family: monospace; font-size: 12px; background: #f0f0f0" @click="insertContainerVar(v.key)">{{ v.key }}</Tag>
              </Tooltip>
              <div style="flex: 1"></div>
              <Button v-if="!addVarVisible" type="link" size="small" @click="openAddVar" style="font-size: 12px; padding: 0 4px">＋ 新增</Button>
            </div>

            <!-- 行内新增变量 -->
            <div v-if="addVarVisible" style="margin-top: 6px; border: 1px solid #e0e0e6; border-radius: 4px; padding: 8px; background: #fafafa">
              <div style="display: flex; gap: 6px; align-items: center; padding: 0 2px 4px; color: #888; font-size: 12px">
                <span style="flex: 1">变量名</span>
                <span style="flex: 1">值</span>
                <span style="flex: 1.2">说明</span>
                <span style="width: 28px"></span>
              </div>
              <div v-for="(r, i) in addDrafts" :key="i" style="display: flex; gap: 6px; align-items: center; margin-bottom: 4px">
                <Input v-model:value="r.key" size="small" placeholder="如 APP_PORT" style="flex: 1" @update:value="(val: string) => { r.key = val.toUpperCase() }" />
                <Input v-model:value="r.value" size="small" placeholder="值" style="flex: 1" />
                <Input v-model:value="r.description" size="small" placeholder="说明" style="flex: 1.2" />
                <Button size="small" type="text" danger @click="removeVarDraft(i)" style="font-size: 11px">✕</Button>
              </div>
              <div style="display: flex; gap: 6px; margin-top: 6px">
                <Button size="small" @click="addVarDraft">+ 添加行</Button>
                <div style="flex: 1"></div>
                <Button size="small" @click="addVarVisible = false">取消</Button>
                <Button size="small" type="primary" @click="saveAddVar">确定</Button>
              </div>
            </div>

            <!-- 分隔线:系统变量 / 项目变量 -->
            <div style="border-top: 1px dashed #e0e0e6; margin: 6px 0"></div>

            <div style="color: #999; font-size: 12px; margin-bottom: 4px">项目变量（点击插入，悬停看值）：</div>
            <div style="display: flex; flex-direction: column; gap: 3px">
              <div v-for="g in projectVarChips" :key="g.title" style="display: flex; flex-wrap: wrap; gap: 4px; align-items: center">
                <span style="color: #999; font-size: 12px; width: 60px; flex-shrink: 0">{{ g.title }}：</span>
                <Tooltip v-for="(c, ci) in g.chips" :key="g.title + ':' + ci" :title="c.tip">
                  <Tag size="small" style="cursor: pointer; font-family: monospace; font-size: 12px; background: #eaf3ff; color: #1677ff" @click="insertText(c.insert)">{{ c.label }}</Tag>
                </Tooltip>
                <span v-if="!g.chips.length" style="color: #bbb; font-size: 12px">-</span>
              </div>
            </div>
            </template>
          </div>
          <div style="flex: 1; min-height: 0; display: flex; flex-direction: column">
            <CodeEditor ref="editorRef" :model-value="yamlText" language="yaml" height="100%"
              :editable="editable" @update:model-value="(v: string) => { yamlText = v; onYamlChange(v) }" />
          </div>
        </div>
      </div>

      <div v-if="deploying || deployDone || deployError" style="max-height: 160px; overflow: auto; font-size: 12px; font-family: monospace; background: #f7f7f7; border-radius: 4px; padding: 8px">
        <div v-for="(l, i) in deployLines" :key="i" style="line-height: 1.6">
          <a-tag size="small" :color="l.status === 'Done' ? 'success' : l.status === 'Error' ? 'error' : 'default'" style="margin-right: 6px">
            {{ l.status }}
          </a-tag>
          <span>{{ l.id }}</span>
          <span style="color: #888; margin-left: 6px">{{ l.text }}</span>
        </div>
        <Typography.Text v-if="deployError" type="danger" style="font-size: 12px">{{ deployError }}</Typography.Text>
        <Typography.Text v-if="deployDone" type="success" style="font-size: 12px">✓ 部署完成</Typography.Text>
      </div>
      </div>
    </div>

    <template #footer>
      <div class="dw-footer">
        <a-button size="small" @click="emit('update:show', false)">关闭</a-button>
        <template v-if="editable">
          <a-button size="small" :loading="validating" @click="doValidate">校验</a-button>
          <a-button size="small" :loading="saving" @click="doSave">保存</a-button>
          <a-button size="small" type="primary" :loading="saving || deploying" :disabled="deploying" @click="doSaveAndDeploy">
            保存并部署
          </a-button>
        </template>
      </div>
    </template>
  </a-drawer>

  <!-- 选择镜像 -->
  <a-modal :open="imageSelectShow" title="选择镜像" :width="480" :footer="null" @cancel="imageSelectShow = false">
    <div style="max-height: 400px; overflow: auto">
      <div v-for="img in images" :key="img.id" style="display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; cursor: pointer; border-bottom: 1px solid #f0f0f0" @click="selectImage(img.names[0])">
        <span>{{ img.names[0] || '<无标签>' }}</span>
        <Typography.Text type="secondary" style="font-size: 12px">{{ img.arch }}</Typography.Text>
      </div>
    </div>
  </a-modal>

  <!-- 添加协议(网关→端口创建) -->
  <a-modal :open="protoModalShow" :title="'添加协议端口'" :confirm-loading="protoSaving" @ok="saveNewProtocol" @cancel="protoModalShow = false">
    <div style="display: flex; flex-direction: column; gap: 10px">
      <div>
        <div class="dw-label">协议<span style="color: #ff4d4f">*</span></div>
        <a-input v-model:value="protoForm.protocol" placeholder="如 mysql / mqtt / ssh" />
      </div>
      <div>
        <div class="dw-label">说明</div>
        <a-input v-model:value="protoForm.description" placeholder="如 MySQL" />
      </div>
      <div>
        <div class="dw-label">实际端口(回车/逗号新增,可多个)</div>
        <a-select v-model:value="protoForm.ports" mode="tags" :open="false" placeholder="如 3306" :token-separators="[',', ' ']" style="width: 100%" />
      </div>
      <div style="font-size: 12px; color: #888">HTTP/HTTPS 为系统默认项;其它协议当前只登记展示,反向代理入口暂不支持。</div>
    </div>
  </a-modal>

  <!-- 拉取镜像 -->
  <a-modal :open="pullShow" title="拉取镜像" :width="480" :mask-closable="!pullActive" :keyboard="!pullActive" @cancel="pullShow = false">
    <div style="display: flex; flex-direction: column; gap: 8px">
      <a-input size="small" v-model:value="pullName" placeholder="nginx 或 registry.example.com/foo" :disabled="pullActive" />
      <div style="display: flex; gap: 12px">
        <a-input size="small" v-model:value="pullTag" placeholder="版本(latest)" :disabled="pullActive" style="flex: 1" />
        <a-input size="small" v-model:value="pullArch" placeholder="架构" :disabled="pullActive" style="flex: 1" />
      </div>
      <a-progress v-if="pullActive || pullPercent > 0" :percent="Math.round(pullPercent)" />
      <Typography.Text v-if="pullStatus" type="secondary" style="font-size: 12px">{{ pullStatus }}</Typography.Text>
    </div>
    <template #footer>
      <a-space justify="end">
        <a-button v-if="pullActive" @click="closePull">终止</a-button>
        <a-button v-else type="primary" @click="startPull">拉取</a-button>
        <a-button :disabled="pullActive" @click="pullShow = false">关闭</a-button>
      </a-space>
    </template>
  </a-modal>

  <!-- 加入现有网络 -->
  <a-modal :open="networkJoinShow" title="加入现有网络" :width="560" :footer="null" @cancel="networkJoinShow = false">
    <div style="max-height: 400px; overflow: auto">
      <div v-for="n in networksAll" :key="n.id" style="display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; border-bottom: 1px solid #f0f0f0">
        <div>
          <div>{{ n.name }}</div>
          <Typography.Text type="secondary" style="font-size: 12px">{{ n.driver }} · {{ n.subnet || '-' }}</Typography.Text>
        </div>
        <a-button size="small" @click="networkAdd(n.name)">加入</a-button>
      </div>
    </div>
  </a-modal>
</template>

<style scoped>
/* EDIT(YAML)编辑器滚动兜底:确保 .cm-scroller 可滚动且出现滚动条 */
:deep(.cm-editor) {
  height: 100%;
}
:deep(.cm-scroller) {
  overflow: auto !important;
  min-height: 0;
}
:deep(.cm-content) {
  min-height: 100%;
}
/* 编排表单紧凑化(仅样式) */
.cf-sec {
  border-top: 1px solid #f0f0f0;
  padding-top: 10px;
}
.cf-sec .dw-label {
  margin-bottom: 2px;
}
</style>
