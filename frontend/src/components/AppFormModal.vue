<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import {
  Drawer, Input, InputNumber, Radio, Switch, Button, Select, Checkbox, Typography, Tooltip, AutoComplete, Form, Divider, message,
} from 'ant-design-vue'
import {
  createApp, createService, createServiceDefault, updateService, listGroups, listFragments,
  type FragmentView, type GroupView, type ServiceItem,
} from '../api/gateway'
import { listDomains, createDomain } from '../api/domains'
import { listCredentials, type DNSCredential } from '../api/credentials'

const props = defineProps<{
  show: boolean
  /** 编辑目标:传入时打开编辑模式并回填;否则为创建。 */
  editTarget?: { service: ServiceItem; group: GroupView } | null
  /** 指定应用:传入时打开「在该应用中添加服务」模式。 */
  targetApp?: GroupView | null
}>()
const emit = defineEmits<{ 'update:show': [boolean]; saved: [] }>()

const [messageApi, contextHolder] = message.useMessage()
const saving = ref(false)

// ---- 类型 ----
interface DomainRow {
  protocol: 'https' | 'http'; subdomain: string; rootDomain: string
  customPort: boolean; port: number | null
}
function emptyDomain(): DomainRow {
  return { protocol: 'https', subdomain: '', rootDomain: '', customPort: false, port: null }
}
function defaultPortFor(p: string) { return p === 'https' ? 443 : 80 }

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
const domainOptions = ref<{ label: string; value: string }[]>([])
const existingApps = ref<GroupView[]>([])
const appOptions = computed(() => existingApps.value.map((a) => ({ value: a.name })))
// 输入的应用名若匹配到已有应用,保存时按「选择已有应用」处理;否则按新建。
const resolvedAppId = computed(() => {
  const name = appName.value.trim()
  if (!name) return ''
  const hit = existingApps.value.find((a) => a.name === name)
  return hit ? hit.id : ''
})

// 片段过滤:健康检查(已有独立开关)和忽略证书校验(HTTPS 时自动注入)不显示在列表中
const HIDDEN_FRAGMENT_IDS = new Set(['frag-healthcheck', 'frag-skip-verify'])
const visibleFragments = computed(() => fragments.value.filter((f) => !HIDDEN_FRAGMENT_IDS.has(f.id)))

async function loadMeta() {
  try {
    const [frags, groups, doms] = await Promise.all([
      listFragments().catch(() => []), listGroups().catch(() => []), listDomains().catch(() => []),
    ])
    fragments.value = frags
    existingApps.value = groups.filter((g) => g.source === 'app' && g.editable)
    domainOptions.value = doms.filter((d) => d.name.trim()).map((d) => ({ label: d.name, value: d.name }))
  } catch { /* 忽略元数据加载失败 */ }
}

function preselectDefaults(svc: ServiceRow) {
  svc.fragmentIds = fragments.value.filter((f) => f.defaultEnabled && !f.defaultHidden).map((f) => f.id)
  svc.excludeFragmentIds = []
}
function toggleFragment(svc: ServiceRow, id: string, checked: boolean) {
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
  const h = d.subdomain ? `${d.subdomain}.${d.rootDomain}` : d.rootDomain
  return d.customPort && d.port ? `${h}:${d.port}` : h
}
function onProtocolChange(d: DomainRow) { if (!d.customPort) d.port = null }

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
  const validDomains = svc.domains.filter((d) => d.rootDomain)
  const input: any = {
    name: svc.name.trim(), description: svc.description, type: svc.type,
    domains: validDomains.map((d) => ({
      protocol: d.protocol, subdomain: d.subdomain.trim(), rootDomain: d.rootDomain.trim(),
      customPort: d.customPort, port: d.customPort ? d.port || undefined : undefined,
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

async function save(andContinue = false) {
  if (editingSvc.value && !existingAppId.value) return messageApi.warning('缺少应用上下文')
  const appId = editingSvc.value
    ? existingAppId.value
    : props.targetApp
      ? props.targetApp.id
      : resolvedAppId.value || ''
  for (const svc of services.value) {
    if (!svc.name.trim()) return messageApi.warning('每个服务都需要服务名')
    if (!svc.domains.filter((d) => d.rootDomain).length) return messageApi.warning(`服务「${svc.name}」至少需要一个域名`)
    if (svc.type === 'reverse_proxy' && !svc.upstreams.filter((u) => u.addr.trim()).length) return messageApi.warning(`服务「${svc.name}」需要后端地址`)
    if (svc.type === 'file_server' && svc.rootMode === 'custom' && !svc.root.trim()) return messageApi.warning(`服务「${svc.name}」需要静态根目录`)
  }
  saving.value = true
  try {
    if (editingSvc.value) {
      await updateService(editingSvc.value.id, buildServiceInput(services.value[0]))
      messageApi.success('已保存')
    } else if (appId) {
      for (const svc of services.value) await createService(appId, buildServiceInput(svc))
      messageApi.success('已创建')
    } else if (appName.value.trim()) {
      await createApp({ name: appName.value.trim(), description: appDesc.value, services: services.value.map(buildServiceInput) })
      messageApi.success('已创建')
    } else {
      // 应用可不选:创建到「默认」归属(ADR-018 修订)
      for (const svc of services.value) await createServiceDefault(buildServiceInput(svc))
      messageApi.success('已创建')
    }
    if (andContinue) {
      // 保留应用上下文,重置服务表单继续添加
      const s0 = emptyService(); preselectDefaults(s0)
      services.value = [s0]; activeService.value = 0
      emit('saved')
      return
    }
    emit('saved'); emit('update:show', false)
  } catch (e: any) { messageApi.error(e.message || '保存失败') } finally { saving.value = false }
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
    protocol: (d.protocol as 'https' | 'http') || 'https',
    subdomain: d.subdomain || '',
    rootDomain: d.rootDomain || '',
    customPort: !!d.customPort,
    port: d.port ?? null,
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

      <!-- 内容 -->
      <div>
        <Form layout="vertical" :colon="false">
          <template v-if="editingSvc || props.targetApp">
            <Form.Item label="所属应用">
              <Input :value="(editingGroup?.name || props.targetApp?.name || '')" size="small" disabled />
            </Form.Item>
          </template>
          <template v-else>
            <Form.Item label="应用名称" required>
              <AutoComplete
                v-model:value="appName"
                :options="appOptions"
                size="small"
                placeholder="输入应用名，或从下拉选择已有应用"
                style="width: 320px; max-width: 100%"
                :filter-option="(input: string, option: any) => option.value.toLowerCase().includes(input.toLowerCase())"
              />
              <div class="dw-desc" style="margin-top: 6px">输入可筛选已有应用；不匹配则自动新建应用；留空时归属「默认」。</div>
            </Form.Item>
            <Form.Item label="应用描述">
              <Input v-model:value="appDesc" size="small" placeholder="可选" />
            </Form.Item>
          </template>

          <Divider />

          <template v-if="services[0]">
            <Form.Item label="服务名称" required>
              <Input v-model:value="services[0].name" size="small" placeholder="如 jellyfin" />
            </Form.Item>
            <Form.Item label="服务描述">
              <Input v-model:value="services[0].description" size="small" placeholder="可选" />
            </Form.Item>
          </template>

          <Divider orientation="left">域名</Divider>

          <template v-if="services[0]">

                <Form.Item label="域名（可多行，也可只填一行）">
                  <div style="display:flex; flex-direction:column; gap:6px">
                    <div v-for="(d, di) in services[0].domains" :key="di" style="display:flex; gap:6px; align-items:center; flex-wrap:wrap">
                      <Select v-model:value="d.protocol" :options="[{ label: 'HTTPS', value: 'https' }, { label: 'HTTP', value: 'http' }]"
                        size="small" style="width:88px" @update:value="() => onProtocolChange(d)" />
                      <Input v-model:value="d.subdomain" size="small" placeholder="二级域名" style="width:100px" />
                      <Typography.Text type="secondary">.</Typography.Text>
                      <Select v-model:value="d.rootDomain" size="small" show-search placeholder="主域名" style="width:180px"
                        :options="domainOptions.length ? [...domainOptions, { label: '+ 增加域名', value: '__add_domain__' }] : [{ label: '+ 增加域名', value: '__add_domain__' }]"
                        @update:value="(v: string) => onRootDomainChange(d, v)" />
                      <Select :value="d.customPort ? 'custom' : 'default'" size="small" style="width:110px"
                        :options="[{ label: '默认端口', value: 'default' }, { label: '自定义', value: 'custom' }]"
                        @update:value="(v: string) => { d.customPort = v === 'custom'; if (!d.customPort) d.port = null }" />
                      <InputNumber
                        :value="d.customPort ? d.port : defaultPortFor(d.protocol)"
                        @update:value="(v: number | null) => { if (d.customPort) d.port = v }"
                        :disabled="!d.customPort" :min="1" :max="65535" :precision="0" :step="1"
                        size="small" placeholder="端口" style="width:90px" />
                      <Button size="small" type="text" danger @click="removeDomain(services[0], di)" :disabled="services[0].domains.length <= 1">✕</Button>
                    </div>
                  </div>
                  <div style="margin-top:6px">
                    <Button size="small" type="dashed" @click="addDomain(services[0])">+ 添加域名</Button>
                  </div>
                  <div v-if="services[0].domains.some(d => d.rootDomain)" style="font-size:12px; color:#888; margin-top:6px">
                    访问地址预览：
                    <Typography.Text code v-for="(d, di) in services[0].domains.filter(dd => dd.rootDomain)" :key="di" style="margin-right:8px">{{ domainHostText(d) }}</Typography.Text>
                  </div>
                </Form.Item>

                <Divider orientation="left">服务端信息</Divider>

                <Form.Item label="代理类型">
                  <Radio.Group v-model:value="services[0].type" size="small">
                    <Radio.Button value="reverse_proxy">反向代理</Radio.Button>
                    <Radio.Button value="file_server">静态文件</Radio.Button>
                  </Radio.Group>
                </Form.Item>

                  <template v-if="services[0].type === 'reverse_proxy'">
                    <Form.Item label="目标协议">
                      <Select v-model:value="services[0].upstreamProto" size="small"
                        :options="[{ label: 'HTTP', value: 'http' }, { label: 'HTTPS', value: 'https' }]" style="width:200px" />
                    </Form.Item>
                    <Form.Item label="后端地址（多个自动负载均衡）" required>
                      <div style="display:flex; flex-direction:column; gap:6px">
                        <div v-for="(u, ui) in services[0].upstreams" :key="ui" style="display:flex; gap:6px; align-items:center">
                          <Input v-model:value="u.addr" size="small" placeholder="如 127.0.0.1 或 192.168.1.10" style="flex:1; max-width:300px" />
                          <Input v-model:value="u.port" size="small" placeholder="端口，如 8096" style="width:120px" />
                          <Button size="small" type="text" danger @click="removeUpstream(services[0], ui)" :disabled="services[0].upstreams.length <= 1">✕</Button>
                        </div>
                      </div>
                      <div style="margin-top:6px">
                        <Button size="small" type="dashed" @click="addUpstream(services[0])">+ 添加后端</Button>
                      </div>
                      <div class="dw-desc">多个后端由 caddy 轮询分发；目标为 https 时自动带入忽略证书校验</div>
                    </Form.Item>
                    <Form.Item label="健康检查">
                      <div style="display:flex; align-items:center; gap:8px">
                        <Switch v-model:checked="services[0].healthCheck" size="small" />
                        <span class="dw-desc">开启后探测 /，失败标红</span>
                      </div>
                    </Form.Item>
                  </template>
                  <template v-else>
                    <Form.Item label="静态根目录" required>
                      <div style="margin-bottom:8px">
                        <Radio.Group v-model:value="services[0].rootMode" size="small">
                          <Radio.Button value="default">默认</Radio.Button>
                          <Radio.Button value="custom">自定义</Radio.Button>
                        </Radio.Group>
                      </div>
                      <Input v-model:value="services[0].root" size="small" :disabled="services[0].rootMode === 'default'"
                        :placeholder="services[0].rootMode === 'default' ? DEFAULT_ROOT : '/srv/www 或 <%GB_STATIC_ROOT%>/...'"
                        :style="services[0].rootMode === 'default' ? { color: '#999', fontFamily: 'monospace' } : undefined" />
                      <div class="dw-desc" style="margin-top: 4px" v-if="services[0].rootMode === 'default'">
                        默认使用全局静态根目录（<%GB_STATIC_ROOT%>）下的应用子目录（<%GB_APP%>）
                      </div>
                    </Form.Item>
                    <Form.Item label="显示文件列表">
                      <div style="display:flex; align-items:center; gap:8px">
                        <Switch v-model:checked="services[0].browse" size="small" />
                        <span class="dw-desc">开启后可浏览目录结构</span>
                      </div>
                    </Form.Item>
                  </template>

                <!-- 其他选项 -->
                  <div style="border-top: 1px solid #f0f0f0; padding-top: 12px">
                    <div style="font-size: 14px; font-weight: 600; margin-bottom: 6px">其他选项（Caddy 片段）</div>
                    <div style="display: grid; grid-template-columns: repeat(3, 1fr); gap: 4px 16px">
                      <Checkbox v-for="f in visibleFragments" :key="f.id"
                        :checked="services[0].fragmentIds.includes(f.id)"
                        @update:checked="(c: boolean) => toggleFragment(services[0], f.id, c)">
                        <span>{{ f.name }}</span>
                        <Typography.Text type="secondary" v-if="f.defaultEnabled" style="font-size: 12px">（默认开）</Typography.Text>
                      </Checkbox>
                      <Typography.Text type="secondary" v-if="!visibleFragments.length" style="font-size: 13px; grid-column: 1 / -1">暂无片段</Typography.Text>
                    </div>
                  </div>

              </template>
            </Form>
        </div>

      <!-- 底部按钮 -->
      <template #footer>
      <div class="dw-footer">
        <Button @click="emit('update:show', false)">取消</Button>
        <Button v-if="!editingSvc" :loading="saving" @click="save(true)">继续添加</Button>
        <Button type="primary" :loading="saving" @click="save(false)">{{ editingSvc ? '保存' : '确定' }}</Button>
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
</template>
