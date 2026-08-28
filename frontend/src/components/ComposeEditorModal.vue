<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount, computed } from 'vue'
import {
  NDrawer, NModal, NInput, NButton, NSpace, NAlert, NText, NTag, NPopconfirm, NSwitch, NProgress, NIcon,
  NCollapse, NCollapseItem, NInputNumber, NSelect, useMessage,
} from 'naive-ui'
import { CopyOutline, SearchOutline, ArrowUndoOutline, ArrowRedoOutline } from '@vicons/ionicons5'
import {
  getCompose, createCompose, saveCompose, validateCompose, wsURL,
  dockerInfo, listImages, listNetworks,
  type DeployProgress, type DockerInfo, type ImageView, type NetworkView,
} from '../api/docker'
import { listDomains, type Domain } from '../api/domains'
import { parseCompose, serializeCompose, type ComposeState, type ServiceConfig } from '../utils/compose'
import { Compartment } from '@codemirror/state'
// maple-mono 字体:按需引入 latin 子集的 400/700 字重
import '@fontsource/maple-mono/latin-400.css'
import '@fontsource/maple-mono/latin-700.css'

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

const isNew = computed(() => !props.project)
const editable = computed(() => !props.readOnly)

// 系统信息 / 已有镜像 / 域名列表(镜像选择、域名访问、资源上限用)
const info = ref<DockerInfo | null>(null)
const images = ref<ImageView[]>([])
const domains = ref<Domain[]>([])

const editorEl = ref<HTMLElement | null>(null)
let editorView: any = null
let cmModule: any = null
let editorDebounce: number | undefined
/** 程序化写入编辑器时置位,避免触发 yamlToForm 导致列表被重新推导(丢模式/空键) */
let settingEditor = false
// 编辑器外观:主题(默认深色)/字号/查找替换
const themeCompartment = new Compartment()
const fontSizeCompartment = new Compartment()
const fontFamilyCompartment = new Compartment()
const editorTheme = ref<'dark' | 'light'>('dark')
const editorFontSize = ref(13)
const editorFontFamily = ref('Maple Mono')
const fontSizeOptions = [12, 13, 14, 16, 18].map((n) => ({ label: `${n}px`, value: n }))
const fontFamilyOptions = [
  { label: 'Maple Mono', value: 'Maple Mono' },
  { label: 'JetBrains Mono', value: 'JetBrains Mono' },
  { label: 'Fira Code', value: 'Fira Code' },
  { label: 'Cascadia Code', value: 'Cascadia Code' },
  { label: 'Source Code Pro', value: 'Source Code Pro' },
  { label: 'IBM Plex Mono', value: 'IBM Plex Mono' },
  { label: '系统等宽', value: 'monospace' },
]

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
  if (v) nextTick(() => editorView?.requestMeasure())
})

const restartOptions = [
  { label: '不重启', value: 'no' },
  { label: '总是', value: 'always' },
  { label: '失败时', value: 'on-failure' },
  { label: '除非手动停止', value: 'unless-stopped' },
]

function defaultTemplate(): string {
  return `services:
  Ser:
    image: ''
    restart: unless-stopped
`
}

async function loadCodeMirror() {
  if (cmModule) return cmModule
  const [cm, langYaml, state, search, oneDark, view] = await Promise.all([
    import('codemirror'),
    import('@codemirror/lang-yaml'),
    import('@codemirror/state'),
    import('@codemirror/search'),
    import('@codemirror/theme-one-dark'),
    import('@codemirror/view'),
  ])
  cmModule = { cm, langYaml, state, search, oneDark, view }
  return cmModule
}

async function ensureEditor() {
  if (editorView || !editorEl.value) return
  const { cm, langYaml, state, search, oneDark, view } = await loadCodeMirror()
  editorView = new cm.EditorView({
    doc: serializeCompose(formState.value),
    extensions: [
      cm.basicSetup,
      langYaml.yaml(),
      state.EditorState.tabSize.of(2),
      cm.EditorView.editable.of(editable.value),
      cm.EditorView.lineWrapping,
      search.search({ top: true }),
      state.EditorState.phrases.of({ 'Find': '查找', 'Replace': '替换', 'replace': '替换', 'replace all': '全部替换', 'next': '下一个', 'previous': '上一个', 'all': '全部', 'match case': '区分大小写', 'by word': '全字匹配', 'regexp': '正则', 'close': '关闭' }),
      themeCompartment.of(themedExtensions(cm, oneDark)),
      fontSizeCompartment.of(cm.EditorView.theme({ '&': { fontSize: `${editorFontSize.value}px` } })),
      fontFamilyCompartment.of(cm.EditorView.theme({ '.cm-content': { fontFamily: `${editorFontFamily.value}, ui-monospace, SFMono-Regular, Menlo, monospace` } })),
      cm.EditorView.theme({ '&': { height: '100%' } }),
      cm.EditorView.theme({ '.cm-search .cm-textfield': { width: '200px' } }),
      whitespaceMarkers(view, state),
      cm.EditorView.updateListener.of((update: any) => {
        if (update.docChanged && !settingEditor) { yamlDirty = true; scheduleYamlToForm() }
      }),
    ],
    parent: editorEl.value,
  })
}

// 空格显示为点、制表符显示为箭头
function whitespaceMarkers(view: any, state: any) {
  const { ViewPlugin, Decoration, WidgetType } = view
  const { RangeSetBuilder } = state
  const mk = (txt: string) => class extends WidgetType {
    eq() { return true }
    toDOM() {
      const s = document.createElement('span')
      s.textContent = txt
      s.style.color = '#808080'
      s.style.opacity = '0.55'
      return s
    }
  }
  const SpaceWidget = mk('·')
  const TabWidget = mk('→')
  return ViewPlugin.fromClass(class {
    decorations: any
    constructor(view: any) { this.decorations = this.build(view) }
    update(u: any) { if (u.docChanged || u.viewportChanged) this.decorations = this.build(u.view) }
    build(view: any) {
      const b = new RangeSetBuilder()
      for (const { from, to } of view.visibleRanges) {
        const text = view.state.doc.sliceString(from, to)
        for (let i = 0; i < text.length; i++) {
          const c = text.charCodeAt(i)
          if (c === 32) b.add(from + i, from + i + 1, Decoration.replace({ widget: new SpaceWidget() }))
          else if (c === 9) b.add(from + i, from + i + 1, Decoration.replace({ widget: new TabWidget() }))
        }
      }
      return b.finish()
    }
  }, { decorations: (v: any) => v.decorations })
}

function destroyEditor() {
  editorView?.destroy()
  editorView = null
}

// 主题扩展:oneDark + 查找面板按钮颜色(随主题切换反色)
function themedExtensions(cm: any, oneDark: any) {
  const btn = cm.EditorView.theme({
    '.cm-search button, .cm-search .cm-button': { color: editorTheme.value === 'dark' ? '#abb2bf' : '#333' },
  })
  return editorTheme.value === 'dark' ? [oneDark.oneDark, btn] : [btn]
}
function toggleTheme() {
  editorTheme.value = editorTheme.value === 'dark' ? 'light' : 'dark'
  if (editorView && cmModule) {
    editorView.dispatch({ effects: themeCompartment.reconfigure(themedExtensions(cmModule.cm, cmModule.oneDark)) })
  }
}
function setFontSize(px: number) {
  editorFontSize.value = px
  if (editorView && cmModule) {
    editorView.dispatch({ effects: fontSizeCompartment.reconfigure(cmModule.cm.EditorView.theme({ '&': { fontSize: `${px}px` } })) })
  }
}
function setFontFamily(family: string) {
  editorFontFamily.value = family
  if (editorView && cmModule) {
    editorView.dispatch({ effects: fontFamilyCompartment.reconfigure(cmModule.cm.EditorView.theme({ '.cm-content': { fontFamily: `${family}, ui-monospace, SFMono-Regular, Menlo, monospace` } })) })
  }
}
function undoEdit() { if (editorView && cmModule) cmModule.cm.undo(editorView) }
function redoEdit() { if (editorView && cmModule) cmModule.cm.redo(editorView) }
function openSearch() {
  if (editorView && cmModule) cmModule.search.openSearchPanel(editorView)
}
function copyYaml() {
  navigator.clipboard.writeText(editorContent()).then(() => message.success('已复制')).catch(() => message.error('复制失败'))
}

function editorContent(): string {
  return editorView ? editorView.state.doc.toString() : serializeCompose(formState.value)
}

function setEditorContent(y: string) {
  if (!editorView) return
  const doc = editorView.state.doc
  if (doc.toString() !== y) {
    settingEditor = true
    editorView.dispatch({ changes: { from: 0, to: doc.length, insert: y } })
    settingEditor = false
  }
}

// --- 双向同步(Generation Lock) ---

/** 序列化为 YAML,并跳过未命名的环境变量与空端口(不改动 formState)。 */
function serializeForYaml(): string {
  const clean: ComposeState = JSON.parse(JSON.stringify(formState.value))
  for (const name of Object.keys(clean.services)) {
    const svc = clean.services[name]
    const env = svc.environment
    if (env && '' in env) { const e = { ...env }; delete e['']; svc.environment = e }
    if (svc.ports) svc.ports = svc.ports.filter((p) => parsePortRow(p).container !== '')
  }
  return serializeCompose(clean)
}

/** 表单 → YAML。禁止 @input 实时触发,只在 @blur / 显式同步时调用。 */
function formToYaml() {
  const v = ++stateVersion
  const y = serializeForYaml()
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
  yamlDirty = false
  if (!s.services[activeService.value]) activeService.value = Object.keys(s.services)[0] || ''
  reloadRows()
}

/** 编辑器 onChange → 400ms 防抖后同步到表单。 */
function scheduleYamlToForm() {
  if (editorDebounce) clearTimeout(editorDebounce)
  editorDebounce = window.setTimeout(yamlToForm, 400)
}

function currentYAML(): string {
  // 两栏并排:保存时若 YAML 有未同步的改动,先强制同步到表单再序列化
  if (yamlDirty) yamlToForm()
  return serializeForYaml()
}

watch(() => props.show, async (v) => {
  if (v) {
    loading.value = true
    validateError.value = ''
    deployLines.value = []
    deployError.value = ''
    deployDone.value = false
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
      reloadRows()
    } catch (e: any) {
      message.error('读取配置失败 — ' + e.message)
      formState.value = { services: {} }
    } finally {
      loading.value = false
    }
    await nextTick()
    await ensureEditor()
    // 拉取系统信息与已有镜像/域名列表(镜像选择、域名访问、资源上限用)
    dockerInfo().then((i) => { info.value = i }).catch(() => {})
    listImages().then((imgs) => { images.value = imgs }).catch(() => {})
    listDomains().then((ds) => { domains.value = ds }).catch(() => {})
  } else {
    closeDeploy()
    destroyEditor()
  }
})

// --- 服务增删 ---

function addService() {
  let name = 'Ser'
  let i = 2
  while (formState.value.services[name]) name = `Ser${i++}`
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
  stopDrag()
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
  if (src.startsWith('../')) return { name: src.slice(3), target, mode: 'default', readonly }
  if (src.startsWith('./')) return { name: src.slice(2), target, mode: 'default', readonly }
  if (src.startsWith('/')) return { name: src, target, mode: 'custom', readonly }
  return { name: src, target, mode: 'volume', readonly }
}
function serializeMountRow(r: MountRow): string {
  const src = r.mode === 'default' ? `./${r.name}` : r.name
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
  reloadDomain()
  reloadRelation()
  initRunDefaults()
}

// 运行配置默认值:写入 formState 使其呈现到 YAML(用户不碰也给默认)
function initRunDefaults() {
  const s = curService()
  if (!s) return
  if (!s.logging) s.logging = { driver: 'json-file', options: { 'max-size': '2m', 'max-file': '5' } }
  if (!s.healthcheck) s.healthcheck = { interval: '30s', timeout: '10s', retries: 3 }
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
  s.volumes = mountList.value.map(serializeMountRow)
}
function envAdd() { envList.value.push({ key: '', value: '' }); syncEnv(); formToYaml() }
function envRemove(i: number) { envList.value.splice(i, 1); syncEnv(); formToYaml() }
function mountAdd() { mountList.value.push({ name: '', target: '', mode: 'default', readonly: false }); syncMount(); formToYaml() }
function mountRemove(i: number) { mountList.value.splice(i, 1); syncMount(); formToYaml() }

// 域名访问:本地行列表(子域名/根域名/指向端口/访问端口),仅 domain 落 caddy label
interface DomainAccessRow {
  subdomain: string
  rootDomain: string
  targetPort: string
  accessMode: 'web' | 'custom'
  accessPort: string
}
const domainList = ref<DomainAccessRow[]>([])
const domainOptions = computed(() => domains.value.map((d) => ({ label: d.name, value: d.name })))

function reloadDomain() {
  domainList.value = (curService()?.caddyRoutes || []).map((r) => {
    const parts = (r.domain || '').split('.')
    return { subdomain: parts[0] || '', rootDomain: parts.slice(1).join('.') || '', targetPort: '', accessMode: 'web' as const, accessPort: '' }
  })
}
function syncDomain() {
  const s = curService()
  if (!s) return
  s.caddyRoutes = domainList.value.map((r) => ({ domain: [r.subdomain, r.rootDomain].filter(Boolean).join('.') }))
}
function domainAdd() { domainList.value.push({ subdomain: '', rootDomain: '', targetPort: '', accessMode: 'web', accessPort: '' }); syncDomain(); formToYaml() }
function domainRemove(i: number) { domainList.value.splice(i, 1); syncDomain(); formToYaml() }
function portExternalOptions() { return portRows().map((p) => ({ label: p.host || '随机', value: p.host })) }
// 第一组端口映射的内部端口(健康检查默认脚本用)
function firstInternalPort(): string {
  const p = portRows()[0]
  return p?.container || '8080'
}
function healthCheckPlaceholder(): string {
  return `配置健康检查脚本，不填写默认 curl -f http://localhost:${firstInternalPort()}/ || exit 1`
}
function domainAccessPort(rootDomain: string): string {
  const d = domains.value.find((x) => x.name === rootDomain)
  return d?.credentialId ? '443' : '80'
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
  return tag ? `${name}:${tag}` : name
}
function startPull() {
  const ref = pullRef()
  if (!ref) { message.warning('请填写镜像名'); return }
  if (pullActive.value) return
  pullActive.value = true
  pullPercent.value = 0
  pullStatus.value = '正在连接…'
  pullSocket = new WebSocket(wsURL('/docker/images/pull', { ref, platform: pullArch.value.trim() }))
  pullSocket.onmessage = (ev) => {
    const p = JSON.parse(ev.data) as any
    if (p.error) { message.error('拉取失败 — ' + p.error); closePull(); return }
    pullPercent.value = p.percent
    pullStatus.value = p.status
    if (p.done) {
      message.success('镜像拉取完成')
      const s = curService()
      if (s) { s.image = ref; formToYaml() }
      closePull()
      listImages().then((imgs) => { images.value = imgs }).catch(() => {})
    }
  }
  pullSocket.onerror = () => { message.error('拉取连接失败'); closePull() }
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
  listNetworks().then((ns) => { networksAll.value = ns; networkJoinShow.value = true }).catch((e) => message.error('读取网络失败 — ' + e.message))
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
  <n-drawer
    :show="show"
    placement="right"
    width="min(1400px, 100vw)"
    :mask-closable="!deploying"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div style="display: flex; flex-direction: column; height: 100%">
      <div style="padding: 14px 24px; border-bottom: 1px solid #eee; display: flex; align-items: center; gap: 8px; flex-shrink: 0">
        <span style="font-size: 16px; font-weight: 600">{{ isNew ? '创建应用' : `编辑 ${displayName || projectName}` }}</span>
        <div style="flex: 1"></div>
        <n-button size="small" :type="showForm ? 'primary' : 'default'" ghost @click="showForm = !showForm">
          {{ showForm ? '隐藏表单' : '显示表单' }}
        </n-button>
        <n-button size="small" :type="showYaml ? 'primary' : 'default'" ghost @click="showYaml = !showYaml">
          {{ showYaml ? '隐藏 YAML' : '显示 YAML' }}
        </n-button>
        <n-button quaternary circle size="small" @click="emit('update:show', false)">✕</n-button>
      </div>
      <div style="flex: 1; overflow: hidden; display: flex; flex-direction: column; gap: 12px; padding: 16px 24px">
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
              <n-popconfirm v-if="editable" @positive-click="removeService(name)">
                <template #trigger>
                  <span @click.stop style="font-size: 11px; line-height: 1; opacity: 0.7">✕</span>
                </template>
                确认删除服务 {{ name }}？
              </n-popconfirm>
            </div>
            <div
              v-if="editable"
              @click="addService"
              style="padding: 5px 10px; border: 1px dashed #d0d0d0; border-radius: 4px 4px 0 0; cursor: pointer; color: #666; font-size: 13px; flex-shrink: 0"
            >+ 服务</div>
          </div>
          <!-- 表单内容 -->
          <div v-if="activeService && formState.services[activeService]" style="flex: 1; overflow: auto; padding: 12px; display: flex; flex-direction: column; gap: 10px">

          <div>
            <div style="font-size: 15px; font-weight: 600; margin-bottom: 8px">基本信息</div>
            <div style="display: flex; gap: 10px">
              <div style="flex: 2">
                <n-text depth="3" style="font-size: 13px">镜像</n-text>
                <div style="display: flex; gap: 6px; align-items: center">
                  <n-input size="small" :value="formState.services[activeService].image" disabled placeholder="选择或拉取镜像" style="flex: 1" />
                  <n-button size="small" :disabled="!editable" @click="openImageSelect">选择镜像</n-button>
                  <n-button size="small" :disabled="!editable" @click="openPull">拉取镜像</n-button>
                </div>
              </div>
              <div style="flex: 1">
                <n-text depth="3" style="font-size: 13px">应用名称(容器名)</n-text>
                <n-input size="small" v-model:value="formState.services[activeService].container_name" :disabled="!editable" @blur="formToYaml" />
              </div>
            </div>
            <div style="display: flex; gap: 10px; margin-top: 10px; align-items: center">
              <div style="flex: 1">
                <n-text depth="3" style="font-size: 13px">重启策略</n-text>
                <n-select size="small" v-model:value="formState.services[activeService].restart" :options="restartOptions" :disabled="!editable" @blur="formToYaml" />
              </div>
              <div style="flex: 1">
                <n-text depth="3" style="font-size: 13px">关联宿主机 PID</n-text>
                <n-switch :value="formState.services[activeService].pid === 'host'" :disabled="!editable" @update:value="(v) => { formState.services[activeService].pid = v ? 'host' : ''; formToYaml() }" style="margin-top: 6px" />
              </div>
            </div>
          </div>

          <!-- 二级 tab:基本配置/运行配置/关联配置 -->
          <div style="display: flex; gap: 0; border-bottom: 1px solid #e0e0e0">
            <div
              v-for="t in configTabs"
              :key="t.key"
              @click="configTab = t.key"
              :style="configTab === t.key ? { color: '#4098fc', fontWeight: '600', borderBottom: '2px solid #4098fc' } : { color: '#666' }"
              style="padding: 6px 14px; cursor: pointer; font-size: 14px; margin-bottom: -1px"
            >{{ t.label }}</div>
          </div>

          <!-- 基本配置 -->
          <div v-if="configTab === 'basic'" style="display: flex; flex-direction: column; gap: 12px">

            <div>
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <n-text style="font-size: 14px; font-weight: 600">环境变量</n-text>
                <n-button v-if="editable" size="tiny" type="primary" quaternary @click="envAdd">+ 添加变量</n-button>
              </div>
              <div v-for="(row, i) in envList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <n-input size="small" v-model:value="row.key" :disabled="!editable" placeholder="变量名" style="flex: 1" @blur="() => { syncEnv(); formToYaml() }" />
                <n-input size="small" v-model:value="row.value" :disabled="!editable" placeholder="变量值" style="flex: 1" @blur="() => { syncEnv(); formToYaml() }" />
                <n-button v-if="editable" size="tiny" quaternary type="error" @click="envRemove(i)">✕</n-button>
              </div>
            </div>

            <div style="border-top: 1px solid #f0f0f0; padding-top: 12px">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <n-text style="font-size: 14px; font-weight: 600">挂载或存储卷</n-text>
                <n-button v-if="editable" size="tiny" type="primary" quaternary @click="mountAdd">+ 添加目录</n-button>
              </div>
              <div v-for="(row, i) in mountList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <n-select size="small" v-model:value="row.mode" :options="mountModeOptions" :disabled="!editable" style="width: 100px" @update:value="() => { syncMount(); formToYaml() }" />
                <n-input size="small" v-model:value="row.name" :disabled="!editable" :placeholder="row.mode === 'default' ? '文件夹名，如 logs' : row.mode === 'custom' ? '绝对路径，如 /var/run/docker.sock' : '卷名'" style="flex: 1" @blur="() => { syncMount(); formToYaml() }" />
                <span style="color: #999">:</span>
                <n-input size="small" v-model:value="row.target" :disabled="!editable" placeholder="容器内路径" style="flex: 1" @blur="() => { syncMount(); formToYaml() }" />
                <n-select size="small" :value="row.readonly ? 'ro' : 'rw'" :options="rwOptions" :disabled="!editable" style="width: 80px" @update:value="(v) => { row.readonly = v === 'ro'; syncMount(); formToYaml() }" />
                <n-button v-if="editable" size="tiny" quaternary type="error" @click="mountRemove(i)">✕</n-button>
              </div>
            </div>

            <div style="border-top: 1px solid #f0f0f0; padding-top: 12px">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <n-text style="font-size: 14px; font-weight: 600">端口映射</n-text>
                <n-button v-if="editable" size="tiny" type="primary" quaternary @click="portAdd">+ 添加端口</n-button>
              </div>
              <div v-for="(row, i) in portRows()" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <n-input size="small" :value="row.host" :disabled="!editable" placeholder="外部端口(留空随机)" style="flex: 1" @update:value="(v) => portUpdate(i, { host: v })" @blur="formToYaml" />
                <span style="color: #999">:</span>
                <n-input size="small" :value="row.container" :disabled="!editable" placeholder="内部端口" style="flex: 1" @update:value="(v) => portUpdate(i, { container: v })" @blur="formToYaml" />
                <n-select size="small" :value="row.protocol" :options="protocolOptions" :disabled="!editable" style="width: 80px" @update:value="(v) => { portUpdate(i, { protocol: v }); formToYaml() }" />
                <n-button v-if="editable" size="tiny" quaternary type="error" @click="portRemove(i)">✕</n-button>
              </div>
            </div>

            <div style="border-top: 1px solid #f0f0f0; padding-top: 12px">
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <n-text style="font-size: 14px; font-weight: 600">域名访问</n-text>
                <n-button v-if="editable" size="tiny" type="primary" quaternary @click="domainAdd">+ 添加域名访问</n-button>
              </div>
              <div v-for="(row, i) in domainList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <n-input size="small" v-model:value="row.subdomain" :disabled="!editable" placeholder="子域名，不填为应用名称" style="flex: 1" @blur="() => { syncDomain(); formToYaml() }" />
                <span style="color: #999">.</span>
                <n-select size="small" v-model:value="row.rootDomain" :options="domainOptions" :disabled="!editable" placeholder="根域名" style="flex: 1" @update:value="() => { syncDomain(); formToYaml() }" />
                <n-select size="small" v-model:value="row.accessMode" :options="[{ label: 'WEB', value: 'web' }, { label: '其他', value: 'custom' }]" :disabled="!editable" style="width: 80px" @update:value="() => { syncDomain(); formToYaml() }" />
                <n-input v-if="row.accessMode === 'custom'" size="small" v-model:value="row.accessPort" :disabled="!editable" placeholder="端口号" style="width: 90px" @blur="() => { syncDomain(); formToYaml() }" />
                <n-input v-else size="small" :value="domainAccessPort(row.rootDomain)" disabled style="width: 60px" />
                <n-select size="small" v-model:value="row.targetPort" :options="portExternalOptions()" :disabled="!editable" placeholder="应用端口" style="width: 110px" @update:value="() => { syncDomain(); formToYaml() }" />
                <span v-if="!row.targetPort" style="color: #d03050; font-size: 12px">请填写端口映射</span>
                <n-popconfirm v-if="editable" @positive-click="domainRemove(i)">
                  <template #trigger>
                    <n-button size="tiny" quaternary type="error">✕</n-button>
                  </template>
                  确认删除该域名访问？
                </n-popconfirm>
              </div>
            </div>
          </div>

          <!-- 运行配置 -->
          <div v-else-if="configTab === 'runtime'" style="display: flex; flex-direction: column; gap: 12px">
            <div>
              <n-text style="font-size: 14px; font-weight: 600">日志配置</n-text>
              <div style="display: flex; gap: 10px; margin-top: 6px">
                <div style="flex: 1">
                  <n-text depth="3" style="font-size: 13px">日志格式</n-text>
                  <n-select size="small" :value="formState.services[activeService].logging?.driver || 'json-file'" :options="logDriverOptions" :disabled="!editable" @update:value="(v) => { const s = formState.services[activeService]; const lg = s.logging || {}; lg.driver = v; s.logging = lg; formToYaml() }" />
                </div>
                <div style="flex: 1">
                  <n-text depth="3" style="font-size: 13px">单文件大小(MiB)</n-text>
                  <n-input size="small" :value="formState.services[activeService].logging?.options?.['max-size'] || '2'" :disabled="!editable" @update:value="(v) => { const s = formState.services[activeService]; const lg = s.logging || {}; lg.options = { ...(lg.options || {}), 'max-size': v }; s.logging = lg; formToYaml() }" />
                </div>
                <div style="flex: 1">
                  <n-text depth="3" style="font-size: 13px">最大保留数</n-text>
                  <n-input size="small" :value="formState.services[activeService].logging?.options?.['max-file'] || '5'" :disabled="!editable" @update:value="(v) => { const s = formState.services[activeService]; const lg = s.logging || {}; lg.options = { ...(lg.options || {}), 'max-file': v }; s.logging = lg; formToYaml() }" />
                </div>
              </div>
            </div>

            <div>
              <n-text style="font-size: 14px; font-weight: 600">健康检查</n-text>
              <div style="display: flex; flex-direction: column; gap: 6px; margin-top: 6px">
                <n-text depth="3" style="font-size: 13px">执行脚本</n-text>
                <n-input size="small" :value="formState.services[activeService].healthcheck?.test" :disabled="!editable" :placeholder="healthCheckPlaceholder()" @update:value="(v) => { const hc = formState.services[activeService].healthcheck || {}; hc.test = v; formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                <div style="display: flex; gap: 10px">
                  <div style="flex: 1">
                    <n-text depth="3" style="font-size: 13px">间隔(秒)</n-text>
                    <n-input size="small" :value="formState.services[activeService].healthcheck?.interval || '30'" :disabled="!editable" @update:value="(v) => { const hc = formState.services[activeService].healthcheck || {}; hc.interval = v; formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                  </div>
                  <div style="flex: 1">
                    <n-text depth="3" style="font-size: 13px">超时(秒)</n-text>
                    <n-input size="small" :value="formState.services[activeService].healthcheck?.timeout || '10'" :disabled="!editable" @update:value="(v) => { const hc = formState.services[activeService].healthcheck || {}; hc.timeout = v; formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                  </div>
                  <div style="flex: 1">
                    <n-text depth="3" style="font-size: 13px">重试次数</n-text>
                    <n-input size="small" :value="formState.services[activeService].healthcheck?.retries ?? '3'" :disabled="!editable" @update:value="(v) => { const hc = formState.services[activeService].healthcheck || {}; hc.retries = Number(v); formState.services[activeService].healthcheck = hc }" @blur="formToYaml" />
                  </div>
                </div>
              </div>
            </div>

            <div>
              <n-text style="font-size: 14px; font-weight: 600">资源限制</n-text>
              <div style="display: flex; gap: 10px; margin-top: 6px">
                <div style="flex: 1">
                  <n-text depth="3" style="font-size: 13px">共享内存(MiB)</n-text>
                  <n-input size="small" :value="formState.services[activeService].shm_size || ''" :disabled="!editable" placeholder="64" @update:value="(v) => { formState.services[activeService].shm_size = v }" @blur="formToYaml" />
                </div>
                <div style="flex: 1">
                  <n-text depth="3" style="font-size: 13px">CPU 配额(最大 {{ info?.ncpu || '-' }} 核)</n-text>
                  <n-input size="small" :value="formState.services[activeService].cpus || ''" :disabled="!editable" placeholder="如 2" @update:value="(v) => { formState.services[activeService].cpus = v }" @blur="formToYaml" />
                </div>
                <div style="flex: 1">
                  <n-text depth="3" style="font-size: 13px">内存(最大 {{ fmtMemGB() }} G)</n-text>
                  <n-input size="small" :value="formState.services[activeService].mem_limit || ''" :disabled="!editable" placeholder="如 512m" @update:value="(v) => { formState.services[activeService].mem_limit = v }" @blur="formToYaml" />
                </div>
              </div>
            </div>
          </div>

          <!-- 关联配置 -->
          <div v-else style="display: flex; flex-direction: column; gap: 12px">
            <div>
              <n-text style="font-size: 14px; font-weight: 600">启动依赖 depends_on</n-text>
              <n-select size="small" multiple :value="formState.services[activeService].depends_on || []" :options="serviceOptions" :disabled="!editable" placeholder="选择本编排内服务" style="margin-top: 6px" @update:value="(v) => { formState.services[activeService].depends_on = v; formToYaml() }" />
            </div>
            <div>
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <n-text style="font-size: 14px; font-weight: 600">关联网络</n-text>
                <n-button v-if="editable" size="tiny" type="primary" quaternary @click="openNetworkJoin">加入现有网络</n-button>
              </div>
              <div v-for="(row, i) in networkList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <n-input size="small" :value="row.name" disabled style="width: 130px" />
                <n-input size="small" v-model:value="row.alias" :disabled="!editable" placeholder="容器别名" style="flex: 1" @blur="() => { syncNetworks(); formToYaml() }" />
                <n-input size="small" v-model:value="row.ipv4" :disabled="!editable" :placeholder="'IPV4 地址（参考段 ' + networkSubnet(row.name) + '）'" style="flex: 1" @blur="() => { syncNetworks(); formToYaml() }" />
                <n-popconfirm v-if="editable" @positive-click="networkRemove(i)">
                  <template #trigger><n-button size="tiny" quaternary type="error">✕</n-button></template>
                  确认删除该网络配置？
                </n-popconfirm>
              </div>
            </div>
            <div>
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <n-text style="font-size: 14px; font-weight: 600">关联宿主机网络</n-text>
                <n-button v-if="editable" size="tiny" type="primary" quaternary @click="hostAdd">+ 添加宿主机网络</n-button>
              </div>
              <div v-for="(row, i) in hostList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <n-input size="small" v-model:value="row.hostname" :disabled="!editable" placeholder="Hostname" style="flex: 1" @blur="() => { syncHosts(); formToYaml() }" />
                <n-input size="small" v-model:value="row.ip" :disabled="!editable" placeholder="IP 地址" style="flex: 1" @blur="() => { syncHosts(); formToYaml() }" />
                <n-popconfirm v-if="editable" @positive-click="hostRemove(i)">
                  <template #trigger><n-button size="tiny" quaternary type="error">✕</n-button></template>
                  确认删除该 host？
                </n-popconfirm>
              </div>
            </div>
            <div>
              <div style="display: flex; align-items: center; gap: 8px; margin-bottom: 6px">
                <n-text style="font-size: 14px; font-weight: 600">关联设备</n-text>
                <n-button v-if="editable" size="tiny" type="primary" quaternary @click="deviceAdd">+ 添加关联设备</n-button>
              </div>
              <div v-for="(row, i) in deviceList" :key="i" style="display: flex; gap: 6px; align-items: center; margin-top: 6px">
                <n-input size="small" v-model:value="row.hostPath" :disabled="!editable" placeholder="设备路径，如 /dev/tty0" style="flex: 1" @blur="() => { syncDevices(); formToYaml() }" />
                <span style="color: #999">:</span>
                <n-input size="small" v-model:value="row.containerPath" :disabled="!editable" placeholder="容器内路径，如 /dev/tty0" style="flex: 1" @blur="() => { syncDevices(); formToYaml() }" />
                <n-popconfirm v-if="editable" @positive-click="deviceRemove(i)">
                  <template #trigger><n-button size="tiny" quaternary type="error">✕</n-button></template>
                  确认删除该设备？
                </n-popconfirm>
              </div>
            </div>
            <div style="display: flex; gap: 10px">
              <div style="flex: 1">
                <n-text style="font-size: 14px; font-weight: 600">network_mode</n-text>
                <n-input v-model:value="formState.services[activeService].network_mode" :disabled="!editable" @blur="formToYaml" style="margin-top: 6px" />
              </div>
              <div style="flex: 1">
                <n-text style="font-size: 14px; font-weight: 600">user(PUID:PGID)</n-text>
                <n-input v-model:value="formState.services[activeService].user" :disabled="!editable" @blur="formToYaml" style="margin-top: 6px" />
              </div>
            </div>
            <div style="display: flex; gap: 10px">
              <div style="flex: 1">
                <n-text style="font-size: 14px; font-weight: 600">command</n-text>
                <n-input v-model:value="formState.services[activeService].command" :disabled="!editable" @blur="formToYaml" style="margin-top: 6px" />
              </div>
              <div style="flex: 1">
                <n-text style="font-size: 14px; font-weight: 600">cap_add</n-text>
                <n-input :value="listToText(formState.services[activeService].cap_add)" type="textarea" :autosize="{ minRows: 1 }" :disabled="!editable" placeholder="一行一个" @update:value="(v) => { formState.services[activeService].cap_add = textToList(v) }" @blur="formToYaml" style="margin-top: 6px" />
              </div>
            </div>
          </div>

          <!-- 未识别字段只读预览 -->
          <n-collapse v-if="formState.services[activeService]._rawConfigs">
            <n-collapse-item title="未识别字段（只读，在 YAML 中编辑）" name="raw">
              <pre style="font-size: 12px; background: #f7f7f7; padding: 8px; border-radius: 4px; overflow: auto">{{ JSON.stringify(formState.services[activeService]._rawConfigs, null, 2) }}</pre>
            </n-collapse-item>
          </n-collapse>
          </div>
        </div>

        <!-- 可拖动分割线 -->
        <div
          v-show="showForm && showYaml"
          @mousedown="startDrag"
          style="width: 7px; flex-shrink: 0; cursor: col-resize; user-select: none; background: #e8e8e8"
        ></div>

        <!-- YAML 区 -->
        <div v-show="showYaml" style="flex: 1; min-width: 0; display: flex; flex-direction: column; border: 1px solid #e0e0e0; overflow: hidden">
          <div :style="{ background: editorTheme === 'dark' ? '#282c34' : '#fafafa', color: editorTheme === 'dark' ? '#abb2bf' : '#333', borderBottom: '1px solid ' + (editorTheme === 'dark' ? '#181a1f' : '#e0e0e0') }" style="display: flex; align-items: center; gap: 6px; padding: 4px 8px">
            <n-button size="tiny" quaternary @click="undoEdit">
              <n-icon :component="ArrowUndoOutline" :color="editorTheme === 'dark' ? '#abb2bf' : '#333'" />
            </n-button>
            <n-button size="tiny" quaternary @click="redoEdit">
              <n-icon :component="ArrowRedoOutline" :color="editorTheme === 'dark' ? '#abb2bf' : '#333'" />
            </n-button>
            <n-button size="tiny" quaternary @click="openSearch">
              <n-icon :component="SearchOutline" :color="editorTheme === 'dark' ? '#abb2bf' : '#333'" />
            </n-button>
            <n-button size="tiny" quaternary @click="toggleTheme">{{ editorTheme === 'dark' ? '☀️' : '🌙' }}</n-button>
            <n-select size="tiny" :value="editorFontSize" :options="fontSizeOptions" style="width: 72px" @update:value="(v: number) => setFontSize(v)" />
            <n-select size="tiny" :value="editorFontFamily" :options="fontFamilyOptions" style="width: 110px" @update:value="(v: string) => setFontFamily(v)" />
            <div style="flex: 1"></div>
            <n-button size="tiny" quaternary @click="copyYaml">
              <n-icon :component="CopyOutline" :color="editorTheme === 'dark' ? '#abb2bf' : '#333'" />
            </n-button>
          </div>
          <div ref="editorEl" style="flex: 1; min-height: 0" />
        </div>
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

      <div style="padding: 14px 24px; border-top: 1px solid #eee; display: flex; justify-content: flex-end; gap: 8px; flex-shrink: 0">
        <template v-if="editable">
          <n-button size="small" :loading="validating" @click="doValidate">校验</n-button>
          <n-button size="small" :loading="saving" @click="doSave">保存</n-button>
          <n-button size="small" type="primary" :loading="saving || deploying" :disabled="deploying" @click="doSaveAndDeploy">
            保存并部署
          </n-button>
        </template>
        <n-button size="small" @click="emit('update:show', false)">关闭</n-button>
      </div>
    </div>
  </n-drawer>

  <!-- 选择镜像 -->
  <n-modal v-model:show="imageSelectShow" preset="card" title="选择镜像" style="width: 480px">
    <div style="max-height: 400px; overflow: auto">
      <div v-for="img in images" :key="img.id" style="display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; cursor: pointer; border-bottom: 1px solid #f0f0f0" @click="selectImage(img.names[0])">
        <span>{{ img.names[0] || '<无标签>' }}</span>
        <n-text depth="3" style="font-size: 12px">{{ img.arch }}</n-text>
      </div>
    </div>
  </n-modal>

  <!-- 拉取镜像 -->
  <n-modal v-model:show="pullShow" preset="card" title="拉取镜像" style="width: 480px" :mask-closable="!pullActive">
    <div style="display: flex; flex-direction: column; gap: 12px">
      <n-input size="small" v-model:value="pullName" placeholder="nginx 或 registry.example.com/foo" :disabled="pullActive" />
      <div style="display: flex; gap: 12px">
        <n-input size="small" v-model:value="pullTag" placeholder="版本(latest)" :disabled="pullActive" style="flex: 1" />
        <n-input size="small" v-model:value="pullArch" placeholder="架构" :disabled="pullActive" style="flex: 1" />
      </div>
      <n-progress v-if="pullActive || pullPercent > 0" type="line" :percentage="Math.round(pullPercent)" />
      <n-text v-if="pullStatus" depth="3" style="font-size: 12px">{{ pullStatus }}</n-text>
    </div>
    <template #footer>
      <n-space justify="end">
        <n-button v-if="pullActive" type="warning" @click="closePull">终止</n-button>
        <n-button v-else type="primary" @click="startPull">拉取</n-button>
        <n-button :disabled="pullActive" @click="pullShow = false">关闭</n-button>
      </n-space>
    </template>
  </n-modal>

  <!-- 加入现有网络 -->
  <n-modal v-model:show="networkJoinShow" preset="card" title="加入现有网络" style="width: 560px">
    <div style="max-height: 400px; overflow: auto">
      <div v-for="n in networksAll" :key="n.id" style="display: flex; justify-content: space-between; align-items: center; padding: 8px 12px; border-bottom: 1px solid #f0f0f0">
        <div>
          <div>{{ n.name }}</div>
          <n-text depth="3" style="font-size: 12px">{{ n.driver }} · {{ n.subnet || '-' }}</n-text>
        </div>
        <n-button size="tiny" @click="networkAdd(n.name)">加入</n-button>
      </div>
    </div>
  </n-modal>
</template>
