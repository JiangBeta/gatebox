<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import {
  Drawer, Input, Radio, Switch, Button, Select, Checkbox, Typography, Tooltip, AutoComplete, message,
} from 'ant-design-vue'
import {
  PlusOutlined, DeleteOutlined, AppstoreOutlined, GlobalOutlined, CloudServerOutlined, SettingOutlined,
} from '@ant-design/icons-vue'
import {
  createApp, createService, createServiceDefault, updateService, listGroups, listFragments, listPorts,
  type FragmentView, type GroupView, type ServiceItem, type PortBinding,
} from '../api/gateway'
import { listDomains, createDomain } from '../api/domains'
import { listCredentials, type DNSCredential } from '../api/credentials'
import { listCapabilities, hasNonHTTPProtocol, type Capability } from '../api/capabilities'
import { runTask } from '../stores/tasks'
import PortFormModal from './PortFormModal.vue'

const props = defineProps<{
  show: boolean
  /** 编辑目标:传入时打开编辑模式并回填;否则为创建。 */
  editTarget?: { service: ServiceItem; group: GroupView } | null
  /** 指定应用:传入时打开「在该应用中添加服务」模式。 */
  targetApp?: GroupView | null
}>()
const emit = defineEmits<{ 'update:show': [boolean]; saved: [] }>()

const [messageApi, contextHolder] = message.useMessage()

// 防连点：两次提交间隔过近直接忽略（保存已改为后台任务，立即返回）。
let lastSubmit = 0

// ---- 类型 ----
interface DomainRow {
  // protocol 取「端口页」的协议名(http/https 可用;其它协议 L4 后置、置灰)。
  protocol: string; subdomain: string; rootDomain: string
}
function emptyDomain(): DomainRow {
  return { protocol: 'https', subdomain: '', rootDomain: '' }
}

interface ServiceRow {
  name: string; description: string; type: 'reverse_proxy' | 'file_server'
  domains: DomainRow[]
  upstreams: { addr: string; port: string }[]; upstreamProto: 'http' | 'https'
  root: string; rootMode: 'default' | 'custom'; browse: boolean; healthCheck: boolean
  fragmentIds: string[]; excludeFragmentIds: string[]
}
function emptyUpstream() { return { addr: '', port: '' } }
function emptyService(): ServiceRow {
  return {
    name: '', description: '', type: 'reverse_proxy', domains: [emptyDomain()],
    upstreams: [emptyUpstream()], upstreamProto: 'http',
    root: '', rootMode: 'default', browse: false, healthCheck: true, fragmentIds: [], excludeFragmentIds: [],
  }
}

// ---- 状态 ----
const mode = ref<'new' | 'existing'>('new')
const existingAppId = ref('')
const appName = ref('')
const appDesc = ref('')
const services = ref<ServiceRow[]>([emptyService()])
const activeService = ref(0)

const fragments = ref<FragmentView[]>([])
const ports = ref<PortBinding[]>([])
const capabilities = ref<Capability[]>([])
const domainOptions = ref<{ label: string; value: string }[]>([])
const existingApps = ref<GroupView[]>([])

// 能力注册表驱动:是否已有提供者支持非 HTTP 协议(TCP/UDP 等)。
// 前端不判断任何插件 ID（ADR-036 I1）。
const nonHttpEnabled = computed(() => hasNonHTTPProtocol(capabilities.value))

function isHttpDomain(d: DomainRow) { return d.protocol === 'https' || d.protocol === 'http' }

// 域名协议下拉:选项来自「端口页」;http/https 始终可用,其它协议需对应能力提供者启用。
const protocolOptions = computed(() => {
  if (!ports.value.length) {
    return [{ value: 'https', label: 'HTTPS' }, { value: 'http', label: 'HTTP' }]
  }
  return ports.value.map((p) => {
    const ports = p.ports.length ? '（' + p.ports.join('/') + '）' : ''
    const http = p.protocol === 'http' || p.protocol === 'https'
    const enabled = http || nonHttpEnabled.value
    // 纯字符串 label:Ant Design Vue Select 的 options 不渲染 VNode label(否则置灰项整条不显示)。
    return {
      value: p.protocol,
      disabled: !enabled,
      label: `${p.protocol.toUpperCase()}${ports}${enabled ? '' : ' · 需启用 TCP/UDP 扩展'}`,
      title: enabled ? '' : '需安装并启用支持 TCP/UDP 的扩展,之后该协议即可使用',
    }
  })
})

// 目标协议:仅 HTTP/HTTPS(上游 L7)。L4 的传输网络(tcp/udp)由「端口」页协议的「网络」决定。
const upstreamProtoOptions = [
  { value: 'http', label: 'HTTP' },
  { value: 'https', label: 'HTTPS' },
]

// 域名协议对应的 L4 网络标签(端口页记录)。
function domainNetLabel(proto: string): string {
  const p = ports.value.find((x) => x.protocol === proto)
  return ({ udp: 'UDP', both: 'TCP & UDP' } as Record<string, string>)[p?.network || 'tcp'] || 'TCP'
}
// 是否存在 HTTP 域名行(决定是否显示「目标协议」;纯 L4 时隐藏)。
const hasHttpDomain = computed(() => services.value[0]?.domains.some((d) => isHttpDomain(d)) ?? true)

// 忽略自带证书校验:HTTPS 后端由生成器强制启用,「其他选项」中显示为勾选且禁用。
const SKIP_VERIFY_ID = 'frag-skip-verify'
const skipVerifyOn = computed(() =>
  services.value[0]?.type === 'reverse_proxy' && services.value[0]?.upstreamProto === 'https',
)

// 域名行「+ 新增协议」弹层
const portModalShow = ref(false)
const pendingDomainIdx = ref<number | null>(null)
function openPortModal(di: number) {
  pendingDomainIdx.value = di
  portModalShow.value = true
}
async function onPortSaved(p: PortBinding) {
  await loadMeta()
  const svc = services.value[0]
  const idx = pendingDomainIdx.value
  if (svc && idx !== null && svc.domains[idx]) svc.domains[idx].protocol = p.protocol
  pendingDomainIdx.value = null
}
const appOptions = computed(() => existingApps.value.map((a) => ({ value: a.name })))
// 输入的应用名若匹配到已有应用,保存时按「选择已有应用」处理;否则按新建。
const resolvedAppId = computed(() => {
  const name = appName.value.trim()
  if (!name) return ''
  const hit = existingApps.value.find((a) => a.name === name)
  return hit ? hit.id : ''
})

// 片段过滤:由后端 defaultHidden 驱动(ADR-033),自动管理/不可选片段不显示在「其他选项」
const visibleFragments = computed(() => fragments.value.filter((f) => !f.defaultHidden))

async function loadMeta() {
  try {
    const [frags, groups, doms, prts, caps] = await Promise.all([
      listFragments().catch(() => []), listGroups().catch(() => []), listDomains().catch(() => []),
      listPorts().catch(() => []), listCapabilities('proxy-protocols').catch(() => []),
    ])
    fragments.value = frags
    ports.value = prts
    capabilities.value = caps
    existingApps.value = groups.filter((g) => g.source === 'app' && g.editable)
    domainOptions.value = doms.filter((d) => d.name.trim()).map((d) => ({ label: d.name, value: d.name }))
  } catch { /* 忽略元数据加载失败 */ }
}

function preselectDefaults(svc: ServiceRow) {
  svc.fragmentIds = fragments.value.filter((f) => f.defaultEnabled && !f.defaultHidden).map((f) => f.id)
  svc.excludeFragmentIds = []
}
function toggleFragment(svc: ServiceRow, id: string, checked: boolean) {
  if (id === SKIP_VERIFY_ID) return // 自动管理,不可手动切换
  const frag = fragments.value.find((f) => f.id === id)
  const isDefault = frag?.defaultEnabled && !frag.defaultHidden
  if (checked) { svc.fragmentIds.push(id); svc.excludeFragmentIds = svc.excludeFragmentIds.filter((x) => x !== id) }
  else { svc.fragmentIds = svc.fragmentIds.filter((x) => x !== id); if (isDefault) svc.excludeFragmentIds.push(id) }
}

function addDomain(svc: ServiceRow) { svc.domains.push(emptyDomain()) }
function removeDomain(svc: ServiceRow, i: number) {
  if (svc.domains.length <= 1) return messageApi.warning('至少保留一行域名')
  svc.domains.splice(i, 1)
}

function addService() {
  const s = emptyService(); preselectDefaults(s)
  services.value.push(s); activeService.value = services.value.length - 1
}
function addUpstream(svc: ServiceRow) { svc.upstreams.push(emptyUpstream()) }
function removeUpstream(svc: ServiceRow, i: number) {
  if (svc.upstreams.length <= 1) return messageApi.warning('至少保留一个后端')
  svc.upstreams.splice(i, 1)
}
function removeService(i: number) {
  if (services.value.length <= 1) return messageApi.warning('至少保留一个服务')
  services.value.splice(i, 1)
  if (activeService.value >= services.value.length) activeService.value = services.value.length - 1
}
function svcTypeLabel(t: string) { return t === 'reverse_proxy' ? '反代' : '静态' }
function domainHostText(d: DomainRow) {
  return d.subdomain ? `${d.subdomain}.${d.rootDomain}` : d.rootDomain
}

const rootModeLabels = [
  { label: '默认', value: 'default' },
  { label: '自定义', value: 'custom' },
]
const DEFAULT_ROOT = '<%GB_STATIC_ROOT%>/<%GB_APP%>/'

// ---- "增加域名" → 域名管理抽屉 ----
const showDomainDrawer = ref(false)
const domainForm = ref({ name: '', credentialId: '' })
const credentials = ref<DNSCredential[]>([])
const pendingDomainRow = ref<DomainRow | null>(null)
function openDomainDrawer(row?: DomainRow) {
  pendingDomainRow.value = row || null; domainForm.value = { name: '', credentialId: '' }
  listCredentials().then((c) => { credentials.value = c }).catch(() => {})
  showDomainDrawer.value = true
}
async function saveNewDomain() {
  if (!domainForm.value.name.trim()) return messageApi.warning('请输入域名')
  try {
    const d = await createDomain(domainForm.value)
    domainOptions.value.push({ label: d.name, value: d.name })
    if (pendingDomainRow.value) pendingDomainRow.value.rootDomain = d.name
    showDomainDrawer.value = false; messageApi.success('已添加')
  } catch (e: any) { messageApi.error(e.message) }
}
function onRootDomainChange(d: DomainRow, val: string) {
  if (val === '__add_domain__') { d.rootDomain = ''; openDomainDrawer(d) }
}

function buildServiceInput(svc: ServiceRow) {
  // HTTP 域名行需 rootDomain;非 HTTP(L4)协议行只需协议名。
  const validDomains = svc.domains.filter((d) => (isHttpDomain(d) ? d.rootDomain : true))
  const input: any = {
    name: svc.name.trim(), description: svc.description, type: svc.type,
    domains: validDomains.map((d) => ({
      protocol: d.protocol, subdomain: d.subdomain.trim(), rootDomain: d.rootDomain.trim(),
    })),
    fragmentIds: svc.fragmentIds, excludeFragmentIds: svc.excludeFragmentIds,
  }
  if (svc.type === 'reverse_proxy') {
    const defPort = svc.upstreamProto === 'https' ? '443' : '80'
    // 多上游负载均衡:每行地址+端口为一后端(caddy 按 round-robin 分摊)。
    input.upstream = svc.upstreams
      .filter((u) => u.addr.trim())
      .map((u) => `${u.addr.trim()}:${u.port || defPort}`)
    input.upstreamProto = svc.upstreamProto; input.healthUri = svc.healthCheck ? '/' : ''
  } else {
    input.root = svc.rootMode === 'custom' ? svc.root.trim() : DEFAULT_ROOT
    input.browse = svc.browse
  }
  return input
}

function save(andContinue = false) {
  const now = Date.now()
  if (now - lastSubmit < 300) return
  lastSubmit = now

  if (editingSvc.value && !existingAppId.value) return messageApi.warning('缺少应用上下文')
  const editing = editingSvc.value
  const appId = editing
    ? existingAppId.value
    : props.targetApp
      ? props.targetApp.id
      : resolvedAppId.value || ''
  for (const svc of services.value) {
    if (!svc.name.trim()) return messageApi.warning('每个服务都需要服务名')
    if (!svc.domains.length) return messageApi.warning(`服务「${svc.name}」至少需要一个域名`)
    for (const d of svc.domains) {
      if (isHttpDomain(d) && !d.rootDomain) return messageApi.warning(`服务「${svc.name}」的域名行缺少主域名`)
    }
    const hasL4 = svc.domains.some((d) => !isHttpDomain(d))
    if (hasL4 && svc.type !== 'reverse_proxy') return messageApi.warning('非 HTTP 协议仅支持反向代理(L4)')
    if (svc.type === 'reverse_proxy' && !svc.upstreams.filter((u) => u.addr.trim()).length) return messageApi.warning(`服务「${svc.name}」需要后端地址`)
    if (svc.type === 'file_server' && svc.rootMode === 'custom' && !svc.root.trim()) return messageApi.warning(`服务「${svc.name}」需要静态根目录`)
  }

  // 冻结表单快照：随后立即关闭/重置抽屜，后台任务只使用快照，保证后续操作不受影响。
  const snapshots = services.value.map(buildServiceInput)
  const newAppName = appName.value.trim()
  const newAppDesc = appDesc.value
  const label = editing
    ? `保存代理「${editing.name}」`
    : newAppName && !appId
      ? `创建应用「${newAppName}」`
      : snapshots.length === 1
        ? `创建代理「${services.value[0].name.trim()}」`
        : `创建 ${snapshots.length} 个代理`

  // 后台执行：reloadCaddy / acme.sh DNS-01 可能耗时数分钟，不阻塞用户。
  const task = runTask(label, async () => {
    if (editing) {
      await updateService(editing.id, snapshots[0])
    } else if (appId) {
      for (const input of snapshots) await createService(appId, input)
    } else if (newAppName) {
      await createApp({ name: newAppName, description: newAppDesc, services: snapshots })
    } else {
      // 应用可不选:创建到「默认」归属(ADR-018 修订)
      for (const input of snapshots) await createServiceDefault(input)
    }
  })

  // 立即进入下一步：继续添加则重置表单，否则关闭抽屉。
  if (andContinue) {
    // 保留应用上下文,重置服务表单继续添加
    const s0 = emptyService(); preselectDefaults(s0)
    services.value = [s0]; activeService.value = 0
  } else {
    emit('update:show', false)
  }

  const okMsg = editing ? '已保存' : '已创建'
  task.done.then(
    // 用全局 message:后台任务完成时组件可能已随路由卸载。
    () => { message.success(okMsg); emit('saved') },
    (e: any) => message.error(e?.message || '保存失败'),
  )
}

const editingSvc = computed(() => props.editTarget?.service || null)
const editingGroup = computed(() => props.editTarget?.group || null)
const dialogTitle = computed(() => {
  if (editingSvc.value) return '编辑代理'
  if (props.targetApp) return `向「${props.targetApp.name}」添加服务`
  return '代理规则'
})

/** 编辑回填:把已有 service 展开到表单行。upstream 数组还原为多后端行。 */
function fillFromService(row: ServiceItem): ServiceRow {
  const domains = (row.domains || []).map((d) => ({
    protocol: d.protocol || 'https',
    subdomain: d.subdomain || '',
    rootDomain: d.rootDomain || '',
  }))
  const upstreams = (row.upstream || ['']).map((up) => {
    const idx = up.lastIndexOf(':')
    if (idx > 0) return { addr: up.slice(0, idx), port: up.slice(idx + 1) }
    return { addr: up, port: '' }
  })
  return {
    name: row.name, description: row.description || '', type: row.type,
    domains,
    upstreams: upstreams.length ? upstreams : [emptyUpstream()],
    upstreamProto: (row.upstreamProto as 'http' | 'https') || 'http',
    root: row.root || '', rootMode: row.root && row.root !== DEFAULT_ROOT ? 'custom' : 'default',
    browse: !!row.browse, healthCheck: row.healthUri !== '', // '' = 关闭
    fragmentIds: [...(row.fragmentIds || [])], excludeFragmentIds: [...(row.excludeFragmentIds || [])],
  }
}

watch(() => props.show, async (s) => {
  if (s) {
    await loadMeta()
    const target = props.editTarget
    if (target) {
      mode.value = 'existing'
      existingAppId.value = target.group.id
      appName.value = target.group.name
      appDesc.value = ''
      const s0 = fillFromService(target.service)
      if (!s0.fragmentIds.length) preselectDefaults(s0)
      services.value = [s0]; activeService.value = 0
    } else if (props.targetApp) {
      mode.value = 'existing'
      existingAppId.value = props.targetApp.id
      appName.value = props.targetApp.name
      appDesc.value = ''
      const s0 = emptyService(); preselectDefaults(s0)
      services.value = [s0]; activeService.value = 0
    } else {
      mode.value = 'new'; existingAppId.value = ''; appName.value = ''; appDesc.value = ''
      const s0 = emptyService(); preselectDefaults(s0)
      services.value = [s0]; activeService.value = 0
    }
  } else {
    // 关闭复位,不干扰下次打开(下次按 editTarget 或新建重新初始化)
  }
})
watch(fragments, (frags) => {
  if (frags.length) services.value.forEach((s) => { if (!s.fragmentIds.length) preselectDefaults(s) })
})
</script>

<template>
  <contextHolder />
  <Drawer :open="show" placement="right" :width="960" @close="emit('update:show', false)">
    <template #title>
      <span class="dw-drawer-title">{{ dialogTitle }}</span>
    </template>

    <div class="pf">
      <!-- 归属应用 -->
      <section class="pf-sec">
        <div class="dw-section"><AppstoreOutlined class="pf-ico" />归属应用</div>
        <template v-if="editingSvc || props.targetApp">
          <Input :value="editingGroup?.name || props.targetApp?.name || ''" disabled />
        </template>
        <template v-else>
          <div class="pf-grid">
            <div class="pf-field">
              <div class="dw-label">应用名称<span class="dw-required">*</span></div>
              <AutoComplete
                v-model:value="appName"
                :options="appOptions"
                placeholder="输入应用名，或从下拉选择已有应用"
                style="width: 100%"
                :filter-option="(input: string, option: any) => option.value.toLowerCase().includes(input.toLowerCase())"
              />
            </div>
            <div class="pf-field">
              <div class="dw-label">应用描述</div>
              <Input v-model:value="appDesc" placeholder="可选" />
            </div>
          </div>
          <div class="dw-desc">输入可筛选已有应用；不匹配则自动新建应用；留空时归属「默认」。</div>
        </template>
      </section>

      <template v-if="services[0]">
        <!-- 服务信息 -->
        <section class="pf-sec">
          <div class="dw-section"><CloudServerOutlined class="pf-ico" />服务信息</div>
          <div class="pf-grid">
            <div class="pf-field">
              <div class="dw-label">服务名称<span class="dw-required">*</span></div>
              <Input v-model:value="services[0].name" placeholder="如 jellyfin" />
            </div>
            <div class="pf-field">
              <div class="dw-label">服务描述</div>
              <Input v-model:value="services[0].description" placeholder="可选" />
            </div>
          </div>
        </section>

        <!-- 发布域名 -->
        <section class="pf-sec">
          <div class="pf-sec-head">
            <div class="dw-section"><GlobalOutlined class="pf-ico" />发布域名</div>
            <Button size="small" type="primary" ghost @click="addDomain(services[0])">
              <PlusOutlined /> 添加域名
            </Button>
          </div>
          <div v-for="(d, di) in services[0].domains" :key="di" class="pf-domain-row">
            <div class="pf-field pf-proto">
              <div class="dw-label">协议</div>
              <Select v-model:value="d.protocol" :options="protocolOptions" style="width: 100%" />
            </div>
            <template v-if="isHttpDomain(d)">
              <div class="pf-field pf-sub">
                <div class="dw-label">二级域名</div>
                <Input v-model:value="d.subdomain" placeholder="可留空" />
              </div>
              <span class="pf-dot">.</span>
              <div class="pf-field pf-root">
                <div class="dw-label">主域名</div>
                <Select
                  v-model:value="d.rootDomain"
                  show-search
                  placeholder="选择主域名"
                  style="width: 100%"
                  :options="domainOptions.length ? [...domainOptions, { label: '+ 增加域名', value: '__add_domain__' }] : [{ label: '+ 增加域名', value: '__add_domain__' }]"
                  @update:value="(v: string) => onRootDomainChange(d, v)"
                />
              </div>
            </template>
            <div v-else class="pf-l4">
              L4 代理（{{ domainNetLabel(d.protocol) }}）：监听端口取该协议在「端口」页登记的端口
            </div>
            <div class="pf-domain-actions">
              <Tooltip title="新增协议/端口">
                <Button @click="openPortModal(di)"><PlusOutlined /></Button>
              </Tooltip>
              <Tooltip title="删除该域名">
                <Button type="text" danger :disabled="services[0].domains.length <= 1" @click="removeDomain(services[0], di)">
                  <DeleteOutlined />
                </Button>
              </Tooltip>
            </div>
          </div>
          <div v-if="services[0].domains.some(d => d.rootDomain)" class="pf-preview">
            访问地址预览：
            <Typography.Text
              v-for="(d, di) in services[0].domains.filter(dd => dd.rootDomain)"
              :key="di"
              code
              style="margin-right: 8px"
            >{{ domainHostText(d) }}</Typography.Text>
          </div>
        </section>

        <!-- 服务端信息 -->
        <section class="pf-sec">
          <div class="dw-section"><CloudServerOutlined class="pf-ico" />服务端信息</div>
          <div class="pf-grid">
            <div class="pf-field">
              <div class="dw-label">代理类型</div>
              <Radio.Group v-model:value="services[0].type" button-style="solid">
                <Radio.Button value="reverse_proxy">反向代理</Radio.Button>
                <Radio.Button value="file_server">静态文件</Radio.Button>
              </Radio.Group>
            </div>
            <div v-if="services[0].type === 'reverse_proxy' && hasHttpDomain" class="pf-field">
              <div class="dw-label">目标协议</div>
              <Select v-model:value="services[0].upstreamProto" :options="upstreamProtoOptions" style="width: 100%" />
            </div>
          </div>

          <template v-if="services[0].type === 'reverse_proxy'">
            <div class="pf-sec-head">
              <div class="dw-label" style="margin-bottom: 0">后端地址<span class="dw-required">*</span></div>
              <Button size="small" type="primary" ghost @click="addUpstream(services[0])">
                <PlusOutlined /> 添加后端
              </Button>
            </div>
            <div v-for="(u, ui) in services[0].upstreams" :key="ui" class="pf-upstream-row">
              <span class="pf-idx">{{ ui + 1 }}</span>
              <Input v-model:value="u.addr" placeholder="主机，如 127.0.0.1" style="flex: 1" />
              <span class="pf-colon">:</span>
              <Input v-model:value="u.port" placeholder="端口，如 8096" style="width: 140px" />
              <Tooltip title="删除该后端">
                <Button type="text" danger :disabled="services[0].upstreams.length <= 1" @click="removeUpstream(services[0], ui)">
                  <DeleteOutlined />
                </Button>
              </Tooltip>
            </div>
            <div class="dw-desc">多个后端由 caddy 轮询分发；目标为 https 时自动带入忽略证书校验</div>
            <div class="pf-inline">
              <Switch v-model:checked="services[0].healthCheck" />
              <span class="dw-label" style="margin-bottom: 0">健康检查</span>
              <span class="dw-desc">开启后探测 /，失败标红</span>
            </div>
          </template>

          <template v-else>
            <div class="pf-inline">
              <span class="dw-label" style="margin-bottom: 0">静态根目录<span class="dw-required">*</span></span>
              <Radio.Group v-model:value="services[0].rootMode">
                <Radio.Button value="default">默认</Radio.Button>
                <Radio.Button value="custom">自定义</Radio.Button>
              </Radio.Group>
              <Input
                v-model:value="services[0].root"
                :disabled="services[0].rootMode === 'default'"
                :placeholder="services[0].rootMode === 'default' ? DEFAULT_ROOT : '/srv/www 或 <%GB_STATIC_ROOT%>/...'"
                :style="services[0].rootMode === 'default' ? { color: '#999', fontFamily: 'monospace' } : undefined"
                style="flex: 1; min-width: 220px"
              />
            </div>
            <div v-if="services[0].rootMode === 'default'" class="dw-desc">
              默认使用全局静态根目录（<%GB_STATIC_ROOT%>）下的应用子目录（<%GB_APP%>）
            </div>
            <div class="pf-inline">
              <Switch v-model:checked="services[0].browse" />
              <span class="dw-label" style="margin-bottom: 0">显示文件列表</span>
              <span class="dw-desc">开启后可浏览目录结构</span>
            </div>
          </template>
        </section>

        <!-- 其他选项 -->
        <section class="pf-sec">
          <div class="dw-section"><SettingOutlined class="pf-ico" />其他选项（Caddy 片段）</div>
          <div class="pf-frag">
            <Checkbox
              v-for="f in visibleFragments"
              :key="f.id"
              :checked="f.id === SKIP_VERIFY_ID ? skipVerifyOn : services[0].fragmentIds.includes(f.id)"
              :disabled="f.id === SKIP_VERIFY_ID"
              @update:checked="(c: boolean) => toggleFragment(services[0], f.id, c)"
            >
              <span>{{ f.name }}</span>
              <Typography.Text v-if="f.id === SKIP_VERIFY_ID" type="secondary" style="font-size: 12px">（HTTPS 后端自动启用）</Typography.Text>
              <Typography.Text v-else-if="f.defaultEnabled" type="secondary" style="font-size: 12px">（默认开）</Typography.Text>
            </Checkbox>
            <Typography.Text v-if="!visibleFragments.length" type="secondary" style="font-size: 13px; grid-column: 1 / -1">暂无片段</Typography.Text>
          </div>
        </section>
      </template>
    </div>

    <!-- 底部按钮 -->
    <template #footer>
      <div class="dw-footer">
        <Button @click="emit('update:show', false)">取消</Button>
        <Button v-if="!editingSvc" @click="save(true)">继续添加</Button>
        <Button type="primary" @click="save(false)">{{ editingSvc ? '保存' : '确定' }}</Button>
      </div>
    </template>
  </Drawer>

  <!-- 域名管理抽屉(嵌套) -->
  <Drawer :open="showDomainDrawer" placement="right" :width="480" :z-index="2100" @close="showDomainDrawer = false">
    <template #title>
      <span class="dw-drawer-title">添加域名</span>
    </template>
    <div style="display: flex; flex-direction: column; gap: 12px">
      <div>
        <div class="dw-label">域名</div>
        <Input v-model:value="domainForm.name" placeholder="如 neob.cn" />
      </div>
      <div>
        <div class="dw-label">DNS 凭证</div>
        <Select v-model:value="domainForm.credentialId"
          :options="credentials.map((c) => ({ label: c.name, value: c.id }))"
          placeholder="选择凭证" allow-clear />
      </div>
    </div>
    <template #footer>
      <div class="dw-footer">
        <Button @click="showDomainDrawer = false">取消</Button>
        <Button type="primary" @click="saveNewDomain">保存</Button>
      </div>
    </template>
  </Drawer>

  <!-- 域名行「+」:新增协议/端口(与端口页共用同一弹层) -->
  <PortFormModal v-model:show="portModalShow" @saved="onPortSaved" />
</template>

<style scoped>
.pf {
  display: flex;
  flex-direction: column;
  gap: 18px;
}
.pf-sec {
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.pf-sec + .pf-sec {
  border-top: 1px solid #f0f0f0;
  padding-top: 18px;
}
.pf-sec-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.pf-ico {
  margin-right: 6px;
  color: #1677ff;
}
.pf-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 0 12px;
}
.pf-field {
  display: flex;
  flex-direction: column;
}
.pf-domain-row {
  display: flex;
  gap: 8px;
  align-items: flex-end;
  padding: 10px 12px;
  background: #fafafa;
  border: 1px solid #f0f0f0;
  border-radius: 8px;
}
.pf-proto {
  width: 160px;
  flex-shrink: 0;
}
.pf-sub {
  width: 150px;
  flex-shrink: 0;
}
.pf-root {
  flex: 1;
  min-width: 170px;
}
.pf-dot {
  display: inline-flex;
  align-items: center;
  height: 32px;
  color: #999;
}
.pf-l4 {
  display: flex;
  align-items: center;
  flex: 1;
  min-width: 200px;
  height: 32px;
  font-size: 12px;
  color: #999;
}
.pf-domain-actions {
  display: flex;
  gap: 4px;
}
.pf-upstream-row {
  display: flex;
  align-items: center;
  gap: 8px;
}
.pf-idx {
  width: 18px;
  flex-shrink: 0;
  text-align: right;
  font-size: 11px;
  font-family: monospace;
  color: #bbb;
}
.pf-colon {
  color: #999;
}
.pf-inline {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}
.pf-preview {
  font-size: 12px;
  color: #888;
}
.pf-frag {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 6px 16px;
}
.pf-frag :deep(.ant-checkbox-wrapper) {
  margin-inline-start: 0;
  font-size: 13px;
}
</style>
