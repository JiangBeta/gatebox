<script setup lang="ts">
import { computed, h, onMounted, ref, type VNode } from 'vue'
import { useRoute } from 'vue-router'
import { LockOutlined } from '@ant-design/icons-vue'
import { Tooltip } from 'ant-design-vue'
import { getCatalog, listObjects, type V41Object } from '@/api/objects'
import { getSystem } from '@/api/system'
import ObjectKindPage from '@/app/components/objects/ObjectKindPage.vue'
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import { statCell } from '@/utils/cell'

/**
 * 服务 · 对象面（ADR-043 §3）：路由 / 服务 / 中间件。
 *
 * 三个 kind 共用一个页面组件，差别只在「多给几列」——保存 → 未生效 → 同步 →
 * 回写状态这条链只有 ObjectKindPage 一处实现，页面不可能各自漏掉。
 *
 * 入口点（entrypoint）不在这里：原型把它放在「设置 → 端口」。
 */
const route = useRoute()

const TABS = [
  { key: 'routes', label: '路由', kind: 'route' },
  { key: 'services', label: '服务', kind: 'service' },
  { key: 'middlewares', label: '中间件', kind: 'middleware' },
] as const

const active = computed(() => {
  const q = route.query.tab as string | undefined
  return TABS.some((t) => t.key === q) ? (q as string) : 'routes'
})
const kind = computed(() => TABS.find((t) => t.key === active.value)?.kind ?? 'route')

/** 关联对象的名字（路由列要显示可读名，不是 id）：域名 / 服务 / 中间件 / 入口点。 */
const domainNames = ref<Record<string, string>>({})
const services = ref<Record<string, V41Object>>({})
const middlewareNames = ref<Record<string, string>>({})
const entrypoints = ref<Record<string, V41Object>>({})
/** 类型目录里服务类型的中文名（reverse_proxy → 反向代理）。拿不到就退回 id。 */
const serviceTypeLabels = ref<Record<string, string>>({})

/** Caddy 版本（路由页顶部统计标签的第一格，来自 /api/v1/system）。 */
const caddyVer = ref('')
async function loadCaddyVersion() {
  try {
    caddyVer.value = (await getSystem()).caddyVersion ?? ''
  } catch {
    caddyVer.value = ''
  }
}

async function loadRefs() {
  try {
    const [domains, svcs, mws, eps, catalog] = await Promise.all([
      listObjects('domain'),
      listObjects('service'),
      listObjects('middleware'),
      listObjects('entrypoint'),
      getCatalog().catch(() => null),
    ])
    const dmap: Record<string, string> = {}
    for (const d of domains) dmap[d.id] = String(d.spec?.name ?? d.key ?? d.id)
    domainNames.value = dmap
    const smap: Record<string, V41Object> = {}
    for (const s of svcs) smap[s.id] = s
    services.value = smap
    const mmap: Record<string, string> = {}
    for (const m of mws) mmap[m.id] = String(m.spec?.name ?? m.key ?? m.id)
    middlewareNames.value = mmap
    const emap: Record<string, V41Object> = {}
    for (const e of eps) emap[e.id] = e
    entrypoints.value = emap
    const tmap: Record<string, string> = {}
    for (const t of catalog?.catalog?.serviceTypes ?? []) tmap[t.id] = t.label
    serviceTypeLabels.value = tmap
  } catch {
    /* 关联对象读不到时列退化成显示 id，不阻塞页面 */
  }
}

function ids(o: V41Object, key: string): string[] {
  const v = o.spec?.[key]
  return Array.isArray(v) ? v.map((x) => String(x)) : []
}

function first(o: V41Object, key: string): string {
  const v = o.spec?.[key]
  return v === undefined || v === null ? '' : String(v)
}

const backendText = (o: V41Object) => {
  const b = (o.spec?.backend ?? {}) as { host?: unknown; port?: unknown }
  const host = b.host ? String(b.host) : '—'
  const port = b.port ? `:${String(b.port)}` : ''
  return `${host}${port}`
}

/** 路由行里引用的服务（L4 / fixed_response 这类没有上游服务，留空）。 */
function serviceOf(o: V41Object): V41Object | undefined {
  return services.value[first(o, 'service')]
}

/** 路由名称列：名称加粗 + 副行「引用的服务名」（原型 .rsub，source 品牌图标暂缺后端数据）。 */
function routeNameRender(o: V41Object): VNode {
  const svc = serviceOf(o)
  const children: VNode[] = [
    h('div', { class: 'row-link' }, String(o.spec?.name ?? o.key ?? o.id)),
  ]
  if (svc) children.push(h('div', { class: 'cell-sub' }, String(svc.spec?.name ?? svc.key ?? '')))
  return h('div', {}, children)
}

// ── 路由列：路由 / TLS / 类型 / 域名 / 入口 / 后端 / 中间件（状态与操作由通用表给）──

/** TLS：auto = 有证书（绿锁），off/false = 关（灰 —）。真正有没有签发看证书页。 */
const tlsColumn: ColumnType<V41Object> = {
  title: 'TLS',
  key: 'tls',
  width: 64,
  customRender: ({ record }) => {
    const t = record.spec?.tls
    const off = t === false || t === 'off' || t === undefined || t === null
    if (off) return statCell('—', 'color:var(--gb-color-text-tertiary)')
    return h(
      'div',
      {},
      h(
        Tooltip,
        { title: t === 'auto' ? '自动签发（acme）' : String(t) },
        { default: () => h('span', { style: 'color:#22c55e;display:inline-flex' }, [h(LockOutlined)]) },
      ),
    )
  },
}

const routeTypeColumn: ColumnType<V41Object> = {
  title: '类型',
  key: 'type',
  width: 120,
  customRender: ({ record }) => {
    const svc = serviceOf(record)
    if (!svc) return statCell('—', 'color:var(--gb-color-text-tertiary)')
    const t = first(svc, 'type')
    return statCell(serviceTypeLabels.value[t] ?? t ?? '—')
  },
}

/** 域名：subdomain.roots 拼成完整域名，多个时最多列 2 个 + 「+N」（tooltip 全量）。 */
const routeDomainColumn: ColumnType<V41Object> = {
  title: '域名',
  key: 'domains',
  width: 220,
  customRender: ({ record }) => {
    const sub = first(record, 'subdomain')
    const domains = ids(record, 'roots').map((r) => `${sub ? `${sub}.` : ''}${domainNames.value[r] ?? r}`)
    if (!domains.length) return statCell('—', 'color:var(--gb-color-text-tertiary)')
    const shown = domains.slice(0, 2).map((d) => h('code', {}, d))
    if (domains.length > 2) {
      shown.push(
        h(
          Tooltip,
          { title: domains.join('、') },
          { default: () => h('span', { style: 'color:var(--gb-color-primary)' }, `+${domains.length - 2}`) },
        ),
      )
    }
    return h('div', { class: 'cell-list' }, shown)
  },
}

/** 入口：协议大写 + 端口标签（端口来自路由覆盖或入口点首个端口）。 */
const routeEntrypointColumn: ColumnType<V41Object> = {
  title: '入口',
  key: 'entrypoint',
  width: 120,
  customRender: ({ record }) => {
    const epId = first(record, 'entrypoint')
    if (!epId) return statCell('—', 'color:var(--gb-color-text-tertiary)')
    const ep = entrypoints.value[epId]
    const protocol = String((ep?.spec?.protocol as string) ?? epId).toUpperCase()
    const own = record.spec?.port ? String(record.spec.port) : ''
    const ports = Array.isArray(ep?.spec?.ports) ? (ep.spec.ports as unknown[]).map(String) : []
    const port = own || ports[0] || ''
    return h('div', {}, [
      h('b', {}, protocol),
      port ? h('span', { class: 'cell-chip' }, port) : null,
    ])
  },
}

/** 后端：地址 + 副行「引用服务名」（服务没引用时副行空，原型同款）。 */
const routeBackendColumn: ColumnType<V41Object> = {
  title: '后端',
  key: 'backend',
  width: 240,
  customRender: ({ record }) => {
    const svc = serviceOf(record)
    const children: VNode[] = [h('code', {}, backendText(record))]
    const hint = svc ? (svc.status?.host ? `${String(svc.status.host)} · ` : '') + String(svc.spec?.name ?? svc.key) : ''
    if (hint) children.push(h('div', { class: 'cell-sub' }, hint))
    return h('div', {}, children)
  },
}

/** 中间件：名字标签串，带说明 tooltip；空则 —。 */
const routeMiddlewareColumn: ColumnType<V41Object> = {
  title: '中间件',
  key: 'middlewares',
  width: 180,
  customRender: ({ record }) => {
    const list = ids(record, 'middlewares')
    if (!list.length) return statCell('—', 'color:var(--gb-color-text-tertiary)')
    return h(
      'div',
      { class: 'cell-chips' },
      list.map((m) => h('span', { class: 'cell-chip' }, middlewareNames.value[m] ?? m)),
    )
  },
}

const routeColumns: ColumnType<V41Object>[] = [
  tlsColumn,
  routeTypeColumn,
  routeDomainColumn,
  routeEntrypointColumn,
  routeBackendColumn,
  routeMiddlewareColumn,
]

const serviceColumns: ColumnType<V41Object>[] = [
  { title: '启用', key: 'enabled', width: 76, customRender: ({ record }) => statCell(record.spec?.enabled === false ? '否' : '是') },
  { title: '类型', key: 'type', width: 120, customRender: ({ record }) => {
      const t = first(record, 'type')
      return statCell(serviceTypeLabels.value[t] ?? t ?? '—')
    } },
  { title: '说明', key: 'description', width: 200, ellipsis: true, customRender: ({ record }) => statCell(first(record, 'description') || '—') },
  { title: '上游地址', key: 'upstream', width: 200, ellipsis: true, customRender: ({ record }) => statCell(String(record.status?.upstreamAddr ?? backendText(record))) },
]

const middlewareColumns: ColumnType<V41Object>[] = [
  { title: '类型', key: 'type', width: 140, customRender: ({ record }) => statCell(first(record, 'type') || '—') },
  { title: '说明', key: 'description', width: 220, ellipsis: true, customRender: ({ record }) => statCell(first(record, 'description') || '—') },
  { title: '默认启用', key: 'defaultEnabled', width: 96, customRender: ({ record }) => statCell(record.spec?.defaultEnabled ? '是' : '否') },
  { title: '引用数', key: 'refCount', width: 88, customRender: ({ record }) => statCell(String(Number((record.status?.refCount as number) ?? 0) || 0)) },
]

const columns = computed<ColumnType<V41Object>[]>(() => {
  switch (kind.value) {
    case 'route':
      return routeColumns
    case 'service':
      return serviceColumns
    default:
      return middlewareColumns
  }
})

const DESCRIPTIONS: Record<string, string> = {
  route: '路由把一个入口点 + 域名绑定到一个服务；保存后由同步链路渲染进 Caddyfile。',
  service: '服务是可被路由引用的后端（反向代理 / 静态文件）；路由也可以自己持有后端。',
  middleware: '中间件在路由上按序执行；type=code 是自由文本逃生舱（Caddy 原文逐字透传）。',
}
const TITLES: Record<string, string> = {
  route: '路由',
  service: '服务',
  middleware: '中间件',
}

onMounted(() => {
  void loadRefs()
  void loadCaddyVersion()
})
</script>

<template>
  <div class="config-page">
    <ObjectKindPage
      :key="kind"
      :kind="kind"
      :title="TITLES[kind]"
      :description="DESCRIPTIONS[kind]"
      :extra-columns="columns"
      :name-render="kind === 'route' ? routeNameRender : undefined"
    >
      <!-- 路由页顶部统计标签（原型 .lstats）：Caddy 版本 / 路由数 / 已生效 / 证书告警。 -->
      <template
        v-if="kind === 'route'"
        #stats="{ objects }"
      >
        <div class="route-stats">
          <span
            v-if="caddyVer"
            class="route-stats-tag"
          >Caddy {{ caddyVer }}</span>
          <span class="route-stats-tag">路由 {{ objects.length }}</span>
          <span class="route-stats-tag">已生效 {{ objects.filter((o) => o.status?.state === '已生效').length }}</span>
          <span class="route-stats-tag">证书告警 {{ objects.filter((o) => o.status?.state === '证书告警').length }}</span>
        </div>
      </template>
    </ObjectKindPage>
  </div>
</template>

<style scoped>
.config-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-md);
}
/* 顶部统计标签串（原型 .lstats） */
.route-stats {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-bottom: var(--gb-space-sm);
}
.route-stats-tag {
  font-size: var(--gb-font-xs);
  color: var(--gb-color-text-secondary);
  background: var(--gb-color-fill-quaternary);
  border-radius: var(--gb-radius-sm);
  padding: 1px 6px;
}
</style>