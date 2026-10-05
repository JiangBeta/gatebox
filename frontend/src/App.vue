<script setup lang="ts">
import { ref, computed, h, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ConfigProvider, Layout, Menu, Tooltip } from 'ant-design-vue'
import {
  PartitionOutlined, GlobalOutlined, ContainerOutlined, AppstoreOutlined,
  ClusterOutlined, DashboardOutlined, SettingOutlined, ApiOutlined,
  GithubOutlined, BookOutlined, TranslationOutlined,
  BulbOutlined, LogoutOutlined, MenuFoldOutlined, MenuUnfoldOutlined,
} from '@ant-design/icons-vue'
import { useColorScheme } from './app/composables/useColorScheme'
import { listComponents } from './api/components'
import { listPlugins } from './api/plugins'
import { me, logout } from './api/auth'
import { toolRoute } from './utils/toolRoutes'
import { getSystem } from './api/system'
import TopActions from './components/TopActions.vue'

interface TabMeta {
  key: string
  label: string
}

interface MenuNode {
  key: string
  label: string
  icon?: () => unknown
  children?: MenuNode[]
  type?: 'divider'
}

const route = useRoute()
const router = useRouter()

// 亮/暗主题：<html data-theme> 管自定义样式，antdTheme() 管组件。
const { isDark, antdTheme, toggle: toggleTheme } = useColorScheme()

const collapsed = ref(false)

// 控制面鉴权（可选启用）：登录页不套用主布局。
const isLogin = computed(() => route.path === '/login')
/** 开了鉴权才显示「退出」入口：没开鉴权时点了也只是跳登录页，纯误导。 */
const authOn = ref(false)
async function checkAuth() {
  try {
    const s = await me()
    authOn.value = Boolean(s.enabled)
    if (s.enabled && !s.authed && !isLogin.value) router.replace('/login')
  } catch {
    /* 探测失败不阻塞（未启用鉴权时接口也可能异常） */
  }
}
async function onLogout() {
  try {
    await logout()
  } finally {
    router.replace('/login')
  }
}

// 「组件」子菜单 = 插件 ui.nav 贡献（组件类入口由插件提供）。
const serviceItems = ref<MenuNode[]>([])
const pluginItems = ref<MenuNode[]>([])

// 一级导航与 8091 原型/docs/v4.1/views.md §3 对齐：服务 · 域名 · 容器 ·
// 应用商店 · 组件 · 监控 · 设置——原型侧栏就是这 7 项，一项不多。
// V4.0 的「网关运行时」（Caddy 直管旧面）原型没有对应项，挪到侧栏底部的
// 次级入口区，不再挤在一级目录里。
const menuItems = computed<MenuNode[]>(() => {
  const items: MenuNode[] = [
    { key: '/services', label: '服务', icon: () => h(PartitionOutlined) },
    { key: '/domains', label: '域名', icon: () => h(GlobalOutlined) },
    { key: '/deploy', label: '容器', icon: () => h(ContainerOutlined) },
    { key: '/store', label: '应用商店', icon: () => h(AppstoreOutlined) },
  ]
  const children = [...serviceItems.value, ...pluginItems.value]
  items.push({
    key: '/components',
    label: '组件',
    icon: () => h(ClusterOutlined),
    ...(children.length ? { children } : {}),
  })
  items.push(
    { key: '/monitor', label: '监控', icon: () => h(DashboardOutlined) },
    { key: '/settings', label: '设置', icon: () => h(SettingOutlined) },
  )
  return items
})

async function loadServices() {
  try {
    const all = await listComponents()
    // 列出全部独立进程类组件（含未安装），保证管理页入口稳定可达。
    serviceItems.value = all
      .filter((c) => c.Kind === 'process')
      .map((c) => ({ key: `comp:${c.ID}`, label: c.Name }))
  } catch {
    /* 接口异常时菜单降级，不阻塞导航 */
  }
}

// 已注册的插件路由名，避免重复 addRoute。
const registeredPlugins = new Set<string>()

// 插件导航与路由来自 manifest 的 ui.nav 贡献（核心不认插件身份，ADR-039 §3）。
// 仅当插件带 role:ui 制品时注册 iframe 页面；纯声明式插件（无 UI 制品）只登记菜单。
async function loadPlugins() {
  try {
    const plugins = await listPlugins()
    const items: MenuNode[] = []
    for (const p of plugins) {
      if (p.state !== 'enabled') continue
      const nav = p.contributions?.ui?.nav || []
      const ui = (p.artifacts || []).find((a) => a.role === 'ui')
      // L2 远程 ESM 仅 official/verified 可用；其余（含无 UI 制品）不注册页面。
      const trusted = p.channel === 'official' || p.channel === 'verified'
      const isEsm = ui?.format === 'esm'
      const isIframe = ui && ui.format !== 'esm'
      if (!ui || (isEsm && !trusted)) continue
      if (registeredPlugins.has(p.id)) continue
      registeredPlugins.add(p.id)
      const loader = isEsm
        ? () => import('./components/PluginEsmHost.vue')
        : () => import('./components/PluginHost.vue')
      for (const n of nav) {
        items.push({ key: `nav:${n.path}`, label: n.label })
        router.addRoute({
          path: n.path,
          name: `plugin:${p.id}`,
          component: loader,
          props: { id: p.id, title: n.label, entry: ui.entry },
          meta: { title: n.label },
        })
      }
    }
    pluginItems.value = items
  } catch {
    /* 接口异常时菜单降级，不阻塞导航 */
  }
}

// 底部功能区:icon + hover tooltip
const footerItems = [
  { name: 'GitHub', icon: () => h(GithubOutlined), href: 'https://github.com/JiangBeta/gatebox' },
  { name: '文档', icon: () => h(BookOutlined), href: 'https://gatebox.cn' },
  { name: '语言', icon: () => h(TranslationOutlined), disabled: true },
]

// 侧栏品牌旁的版本号取后端真实版本（GET /api/v1/system）。
// 之前硬编码 v0.1.0：构建版本一变它就是假的，比不显示更糟。
const appVersion = ref('dev')
async function loadVersion() {
  try {
    const s = await getSystem()
    if (s.version) appVersion.value = String(s.version)
  } catch {
    /* 读不到就显示 dev，不阻塞布局 */
  }
}

const title = computed(() => (route.meta.title as string) || 'GateBox')
const tabs = computed<TabMeta[]>(() => (route.meta.tabs as TabMeta[]) || [])
const activeTab = computed(() => (route.query.tab as string) || tabs.value[0]?.key || '')
const activeTabLabel = computed(
  () => tabs.value.find((t) => t.key === activeTab.value)?.label ?? '',
)

const selectedKeys = computed<string[]>(() => {
  const comp = (route.query.component as string | undefined) || (route.meta.component as string | undefined)
  if (comp) return [`comp:${comp}`]
  if (pluginItems.value.some((n) => n.key === `nav:${route.path}`)) return [`nav:${route.path}`]
  return [route.path]
})

// 子菜单展开状态：只跟随当前所在的父级，避免折叠/跳转后一直挂着。
const openKeys = ref<string[]>([parentKey(route.path)])

function parentKey(path: string): string {
  for (const p of ['/components', '/services']) {
    if (path === p || path.startsWith(`${p}/`)) return p
  }
  return ''
}

function onMenu(info: { key: string }) {
  const key = String(info.key)
  // 组件子菜单：comp:<id> → 该组件的工具页（组件页附带 component 参数用于聚焦）。
  if (key.startsWith('comp:')) {
    const id = key.slice(5)
    const path = toolRoute(id)
    if (path.startsWith('/components')) router.push({ path: '/components', query: { tab: 'components', component: id } })
    else router.push(path)
    return
  }
  // 插件导航项：nav:<path> → 直接跳转其注册路由。
  if (key.startsWith('nav:')) {
    router.push(key.slice(4))
    return
  }
  router.push(key)
}

// 折叠/展开：折叠时清空已展开子菜单，展开时恢复（Ant Design 官方推荐做法）。
let preOpenKeys: string[] = []
function toggleCollapse() {
  collapsed.value = !collapsed.value
  openKeys.value = collapsed.value ? [] : preOpenKeys
}

// 子菜单弹层固定挂到 body:避免被 Sider 的 overflow:hidden 裁剪（折叠态弹层超出侧栏宽度）。
function getPopupContainer() {
  return document.body
}

function onComponentsChanged() {
  void loadServices()
  void loadPlugins()
}

function onTab(key: string) {
  router.replace({ query: { ...route.query, tab: key } })
}

watch(
  () => route.path,
  (p) => {
    const parent = parentKey(p)
    if (parent) openKeys.value = [parent]
  },
)

onMounted(() => {
  void checkAuth()
  void loadVersion()
  void loadServices()
  void loadPlugins()
  window.addEventListener('gatebox:components-changed', onComponentsChanged)
})
onUnmounted(() => {
  window.removeEventListener('gatebox:components-changed', onComponentsChanged)
})
</script>

<template>
  <ConfigProvider :theme="antdTheme()">
    <router-view v-if="isLogin" />
    <Layout
      v-else
      class="app-root"
    >
      <Layout.Sider
        class="app-sider"
        :width="190"
        :collapsed-width="64"
        :collapsed="collapsed"
      >
        <div
          class="app-brand"
          :class="{ collapsed }"
        >
          <template v-if="!collapsed">
            GateBox <span class="app-brand-version">v{{ appVersion }}</span>
          </template>
          <template v-else>G</template>
        </div>

        <Menu
          mode="inline"
          :selected-keys="selectedKeys"
          :open-keys="openKeys"
          :items="menuItems"
          :get-popup-container="getPopupContainer"
          class="app-menu"
          @click="onMenu"
          @open-change="(keys: string[]) => { openKeys = keys; preOpenKeys = keys }"
        />

        <!-- 侧栏底部：次级入口（网关运行时 = V4.0 直管旧面）+ 外链。
             主题切换在顶栏（原型 .top 的 btnTheme 位置），这里不再重复一份。 -->
        <div class="app-footer">
          <div
            class="app-footer-row"
            :class="{ collapsed }"
          >
            <Tooltip
              v-for="f in footerItems"
              :key="f.name"
              :title="f.name"
              placement="right"
            >
              <a
                v-if="f.href"
                :href="f.href"
                target="_blank"
                class="app-footer-link"
              >
                <component :is="f.icon" />
              </a>
              <span
                v-else
                class="app-footer-link is-disabled"
              >
                <component :is="f.icon" />
              </span>
            </Tooltip>
            <Tooltip
              title="网关运行时（Caddy 直管 · 旧面）"
              placement="right"
            >
              <button
                type="button"
                class="app-footer-link app-footer-btn"
                :class="{ 'is-on': route.path.startsWith('/gateway') }"
                aria-label="网关运行时"
                @click="router.push('/gateway')"
              >
                <ApiOutlined />
              </button>
            </Tooltip>
          </div>
        </div>

        <!-- 退出：最底部。鉴权未启用时不显示（顶栏已有同款入口时才不至于重复）。 -->
        <div
          v-if="authOn"
          class="app-logout"
          :class="{ collapsed }"
          @click="onLogout"
        >
          <LogoutOutlined class="app-logout-icon" />
          <span v-if="!collapsed">退出</span>
        </div>
      </Layout.Sider>

      <Layout
        class="app-main"
        :class="{ collapsed }"
      >
        <Layout.Header class="app-header">
          <div class="header-left">
            <Tooltip
              :title="collapsed ? '展开菜单' : '收起菜单'"
              placement="bottom"
            >
              <span
                class="collapse-btn"
                @click="toggleCollapse"
              >
                <MenuUnfoldOutlined v-if="collapsed" />
                <MenuFoldOutlined v-else />
              </span>
            </Tooltip>
            <!-- 面包屑 = 一级页面 / 当前 tab（views.md §4：不再有页面标题）。
                 原型（index.html .top .crumb）是「页面名 + 次级 tab 名」，
                 tab 用次级灰字跟在后面，不画成一串 / 分隔的路径。 -->
            <nav class="app-crumb">
              <span class="app-crumb-item">{{ title }}</span>
              <span
                v-if="tabs.length > 1 && activeTabLabel"
                class="app-crumb-sub"
              >{{ activeTabLabel }}</span>
            </nav>
          </div>
<div class="header-right">
            <TopActions />
            <Tooltip
              :title="isDark ? '切到亮色' : '切到暗色'"
              placement="bottom"
            >
              <button
                type="button"
                class="app-footer-link app-footer-btn header-icon-btn"
                :aria-label="isDark ? '切到亮色' : '切到暗色'"
                @click="toggleTheme()"
              >
                <BulbOutlined />
              </button>
            </Tooltip>
          </div>
        </Layout.Header>

        <Layout.Content class="app-content">
          <!-- 二级目录：只在内容区顶部（原型 index.html 的 .pagetabs）。
               顶栏不再重复一份——两个地方都能切 tab 会让人以为有两种状态。 -->
          <nav
            v-if="tabs.length > 1"
            class="page-tabs"
          >
            <button
              v-for="t in tabs"
              :key="t.key"
              type="button"
              class="page-tab"
              :class="{ active: activeTab === t.key }"
              @click="onTab(t.key)"
            >
              {{ t.label }}
            </button>
          </nav>
          <router-view />
        </Layout.Content>
      </Layout>
    </Layout>
  </ConfigProvider>
</template>

<style scoped>
/* 全部数值走 design/tokens.css 的变量（ADR-032）：换主题只改变量，页面零改动。 */

.app-root {
  min-height: 100vh;
}

.app-sider {
  overflow: hidden;
  height: 100vh;
  position: fixed;
  left: 0;
  top: 0;
  bottom: 0;
  background: var(--gb-color-sider-bg);
  border-right: 1px solid var(--gb-color-border-secondary);
  display: flex;
  flex-direction: column;
}

/* Sider 内层 children 容器也要撑满高度:否则 footer 会贴到菜单下方而不是屏幕底部 */
:deep(.ant-layout-sider-children) {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.app-brand {
  padding: var(--gb-space-md) var(--gb-space-lg);
  font-size: var(--gb-font-xl);
  font-weight: 700;
  white-space: nowrap;
  overflow: hidden;
  text-align: left;
  /* 侧栏是深底（两种主题都是），品牌字跟 --gb-color-sider-text 走。 */
  color: var(--gb-color-sider-text);
}
.app-brand.collapsed {
  padding: var(--gb-space-md) 0;
  text-align: center;
}
.app-brand-version {
  font-size: var(--gb-font-sm);
  color: var(--gb-color-text-tertiary);
}

.app-menu {
  flex: 1;
  overflow: auto;
  border-right: 0;
}

:deep(.app-sider .ant-menu-item),
:deep(.app-sider .ant-menu-submenu-title) {
  font-size: var(--gb-font-base);
}
/* 二级子菜单项字号更小 */
:deep(.app-sider .ant-menu-sub.ant-menu-inline .ant-menu-item) {
  font-size: var(--gb-font-sm);
  height: 34px;
  line-height: 34px;
}
:deep(.app-sider .ant-menu-sub .ant-menu-item-selected) {
  font-weight: 600;
  background: var(--gb-color-primary-bg);
  box-shadow: inset 3px 0 0 var(--gb-color-primary);
}

.app-footer {
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  padding: var(--gb-space-sm) var(--gb-space-md);
}
.app-footer-row {
  display: flex;
  flex: 1;
  gap: var(--gb-space-sm);
  flex-direction: row;
  align-items: center;
  justify-content: space-between;
}
.app-footer-row.collapsed {
  gap: var(--gb-space-sm);
  flex-direction: column;
  justify-content: center;
}
.app-footer-link {
  color: var(--gb-color-sider-text);
  font-size: var(--gb-font-sm);
  line-height: 1;
  cursor: pointer;
  display: inline-flex;
  border: 0;
  background: transparent;
  padding: 0;
}
.app-footer-link:hover {
  color: #ffffff;
}
.app-footer-link.is-disabled {
  color: var(--gb-color-text-tertiary);
  opacity: 0.6;
  cursor: not-allowed;
}

.app-logout {
  border-top: 1px solid rgba(255, 255, 255, 0.08);
  padding: var(--gb-space-md) 0;
  font-size: var(--gb-font-lg);
  color: var(--gb-color-sider-text);
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: var(--gb-space-sm);
  flex-shrink: 0;
  user-select: none;
}
.app-logout.collapsed {
  padding: var(--gb-space-sm) 0;
}
.app-logout-icon {
  font-size: var(--gb-font-lg);
}
.app-logout.collapsed .app-logout-icon {
  font-size: var(--gb-font-sm);
}

.app-main {
  margin-left: 190px;
  transition: margin-left var(--gb-dur-base, 0.2s);
  background: var(--gb-color-bg-layout);
}
.app-main.collapsed {
  margin-left: 64px;
}

.app-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 var(--gb-space-lg);
  height: 56px;
  background: var(--gb-color-bg-container);
  border-bottom: 1px solid var(--gb-color-border-secondary);
}

.header-left {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
}
.header-right {
  display: flex;
  align-items: center;
  gap: var(--gb-space-md);
}
.header-task {
  align-self: center;
}
.collapse-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: var(--gb-radius-base);
  font-size: var(--gb-font-lg);
  color: var(--gb-color-text-secondary);
  cursor: pointer;
}
.collapse-btn:hover {
  background: var(--gb-color-hover);
  color: var(--gb-color-text);
}

/* 面包屑替代页面标题（views.md §4）：页面名 + 次级 tab 名，原型 .top .crumb */
.app-crumb {
  display: flex;
  align-items: baseline;
  gap: var(--gb-space-sm);
  font-size: var(--gb-font-xl);
  font-weight: 600;
  color: var(--gb-color-text);
}
.app-crumb-sub {
  font-size: var(--gb-font-sm);
  font-weight: 400;
  color: var(--gb-color-text-tertiary);
}

.app-content {
  padding: var(--gb-space-lg);
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-md);
}

/* 顶栏右侧图标按钮：原型 .icon-btn（32×30、描边、圆角 6）。
   不能复用侧栏底部的 .app-footer-link——那套颜色是给深色侧栏设计的。 */
.header-icon-btn {
  width: 32px;
  height: 30px;
  border: 1px solid var(--gb-color-border);
  border-radius: var(--gb-radius-base, 6px);
  background: var(--gb-color-bg-container);
  color: var(--gb-color-text-secondary);
  font-size: var(--gb-font-lg);
  justify-content: center;
}
.header-icon-btn:hover {
  border-color: var(--gb-color-primary);
  color: var(--gb-color-primary);
}
/* 侧栏底部次级入口的选中态（网关运行时） */
.app-footer-btn.is-on {
  color: var(--gb-color-primary);
}

/* 二级目录条：原型 index.html 的 .pagetabs —— 内容区顶部的通栏 tab 条
   （背景同面板色、贴边、按压线在底部），不是顶栏里的一排按钮。 */
.page-tabs {
  display: flex;
  gap: 2px;
  margin: calc(-1 * var(--gb-space-lg)) calc(-1 * var(--gb-space-lg)) var(--gb-space-md);
  padding: 0 var(--gb-space-lg);
  background: var(--gb-color-bg-container);
  border-bottom: 1px solid var(--gb-color-border-secondary);
}
.page-tab {
  border: 0;
  background: transparent;
  padding: 11px 14px;
  margin-bottom: -1px;
  cursor: pointer;
  font-size: var(--gb-font-base);
  color: var(--gb-color-text-secondary);
  border-bottom: 2px solid transparent;
}
.page-tab:hover {
  color: var(--gb-color-primary);
}
.page-tab.active {
  color: var(--gb-color-primary);
  font-weight: 500;
  border-bottom-color: var(--gb-color-primary);
}
</style>