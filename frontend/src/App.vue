<script setup lang="ts">
import { ref, computed, h, onMounted, onUnmounted, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ConfigProvider, Layout, Menu, Tooltip,
} from 'ant-design-vue'
import {
  DashboardOutlined, ApiOutlined, ContainerOutlined, GlobalOutlined,
  SettingOutlined, GithubOutlined, BookOutlined, TranslationOutlined,
  BulbOutlined, LogoutOutlined, MenuFoldOutlined, MenuUnfoldOutlined,
  ClusterOutlined, AppstoreOutlined,
} from '@ant-design/icons-vue'
import theme from './theme'
import { listComponents } from './api/components'
import { listPlugins } from './api/plugins'
import { toolRoute } from './utils/toolRoutes'
import TaskCenter from './components/TaskCenter.vue'

interface TabMeta {
  key: string
  label: string
}

interface MenuNode {
  key: string
  label: string
  icon?: () => unknown
  children?: MenuNode[]
}

const route = useRoute()
const router = useRouter()

const collapsed = ref(false)
const openKeys = ref<string[]>(['/services'])

// 「服务」子菜单 = 独立进程类组件（tailscale / flame）+ 插件 ui.nav 贡献。
const serviceItems = ref<MenuNode[]>([])
const pluginItems = ref<MenuNode[]>([])

const menuItems = computed<MenuNode[]>(() => {
  const items: MenuNode[] = [
    { key: '/dashboard', label: '仪表盘', icon: () => h(DashboardOutlined) },
    { key: '/gateway', label: '网关', icon: () => h(ApiOutlined) },
    { key: '/docker', label: '容器', icon: () => h(ContainerOutlined) },
    { key: '/domain', label: '域名', icon: () => h(GlobalOutlined) },
  ]
  const children = [...serviceItems.value, ...pluginItems.value]
  items.push({
    key: '/services',
    label: '服务',
    icon: () => h(ClusterOutlined),
    ...(children.length ? { children } : {}),
  })
  items.push(
    { key: '/extensions', label: '扩展', icon: () => h(AppstoreOutlined) },
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
      const hasUI = (p.artifacts || []).some((a) => a.role === 'ui')
      for (const n of nav) {
        items.push({ key: `nav:${n.path}`, label: n.label })
        if (hasUI && !registeredPlugins.has(p.id)) {
          registeredPlugins.add(p.id)
          router.addRoute({
            path: n.path,
            name: `plugin:${p.id}`,
            component: () => import('./components/PluginHost.vue'),
            props: { id: p.id, title: n.label },
            meta: { title: n.label },
          })
        }
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
  { name: '主题', icon: () => h(BulbOutlined), disabled: true },
]

const activeKey = computed(() => route.path)
// 服务子页（?component=）或带 meta.component 的专用页（如 /services/mosdns）选中对应子菜单项。
const selectedKeys = computed<string[]>(() => {
  const comp = (route.query.component as string | undefined) || (route.meta.component as string | undefined)
  if (comp) return [`comp:${comp}`]
  if (pluginItems.value.some((n) => n.key === `nav:${route.path}`)) return [`nav:${route.path}`]
  return [activeKey.value]
})
const title = computed(() => (route.meta.title as string) || 'GateBox')
const tabs = computed<TabMeta[]>(() => (route.meta.tabs as TabMeta[]) || [])
const activeTab = computed(() => (route.query.tab as string) || tabs.value[0]?.key || '')

function onMenu(info: { key: string }) {
  const key = String(info.key)
  // 服务子菜单：comp:<id> → 该组件的工具页（服务页附带 component 参数用于聚焦）。
  if (key.startsWith('comp:')) {
    const id = key.slice(5)
    const path = toolRoute(id)
    if (path === '/services') router.push({ path, query: { component: id } })
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

function onOpenChange(keys: string[]) {
  openKeys.value = keys
}

// 折叠/展开：折叠时清空已展开子菜单，展开时恢复上次展开项（Ant Design 官方推荐做法）。
let preOpenKeys: string[] = ['/services']
watch(openKeys, (_val, oldVal) => { preOpenKeys = oldVal || [] })
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

onMounted(() => {
  void loadServices()
  void loadPlugins()
  window.addEventListener('gatebox:components-changed', onComponentsChanged)
})
onUnmounted(() => {
  window.removeEventListener('gatebox:components-changed', onComponentsChanged)
})
</script>

<template>
  <ConfigProvider :theme="theme">
    <Layout style="min-height: 100vh">
      <Layout.Sider
        class="app-sider"
        :width="220"
        :collapsed-width="56"
        :collapsed="collapsed"
        :style="{
          overflow: 'hidden',
          height: '100vh',
          position: 'fixed',
          left: 0,
          top: 0,
          bottom: 0,
          background: '#fff',
          borderRight: '1px solid #f0f0f0',
          display: 'flex',
          flexDirection: 'column',
        }"
      >
        <div
          :style="{
            padding: collapsed ? '16px 0' : '16px 20px',
            fontSize: '20px',
            fontWeight: 700,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
            textAlign: collapsed ? 'center' : 'left',
          }"
        >
          <template v-if="!collapsed">
            GateBox <span :style="{ fontSize: '14px', color: '#999' }">v0.1.0</span>
          </template>
          <template v-else>G</template>
        </div>

        <Menu
          mode="inline"
          :selected-keys="selectedKeys"
          :open-keys="openKeys"
          :items="menuItems"
          :get-popup-container="getPopupContainer"
          @click="onMenu"
          @open-change="onOpenChange"
          :style="{ flex: 1, overflow: 'auto', borderRight: 0 }"
        />

        <!-- 底部功能区:github/文档/语言/主题(icon + hover 提示),置于退出上方 -->
        <div
          :style="{
            borderTop: '1px solid #f0f0f0',
            padding: collapsed ? '12px 0' : '12px 16px',
            display: 'flex',
          }"
        >
          <div
            :style="{
              display: 'flex',
              flex: 1,
              gap: collapsed ? '12px' : '8px',
              flexDirection: collapsed ? 'column' : 'row',
              alignItems: 'center',
              justifyContent: collapsed ? 'center' : 'space-between',
            }"
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
                :style="{ color: '#666', fontSize: '14px', lineHeight: 1, cursor: 'pointer', display: 'inline-flex' }"
              >
                <component :is="f.icon" />
              </a>
              <span
                v-else
                :style="{ color: '#999', fontSize: '14px', lineHeight: 1, cursor: 'not-allowed', display: 'inline-flex' }"
              >
                <component :is="f.icon" />
              </span>
            </Tooltip>
          </div>
        </div>

        <!-- 退出按钮:最底部,居下,18px 粗体 -->
        <div
          :style="{
            borderTop: '1px solid #f0f0f0',
            padding: collapsed ? '14px 0' : '16px 0',
            fontSize: '18px',
            color: '#333',
            cursor: 'pointer',
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'center',
            gap: '8px',
            flexShrink: 0,
            userSelect: 'none',
          }"
        >
          <LogoutOutlined :style="{ fontSize: collapsed ? '14px' : '18px' }" />
          <span v-if="!collapsed">退出</span>
        </div>
      </Layout.Sider>

      <Layout :style="{ marginLeft: collapsed ? '56px' : '220px', transition: 'margin-left 0.2s' }">
        <Layout.Header
          :style="{
            padding: '0 24px',
            height: '80px',
            lineHeight: '80px',
            background: '#fff',
            borderBottom: '1px solid #f0f0f0',
          }"
        >
          <div class="header-row">
            <div class="header-left">
              <!-- 折叠/展开切换按钮:位于标题前方,上下居中 -->
              <Tooltip :title="collapsed ? '展开菜单' : '收起菜单'" placement="bottom">
                <span
                  class="collapse-btn"
                  @click="toggleCollapse"
                >
                  <MenuUnfoldOutlined v-if="collapsed" />
                  <MenuFoldOutlined v-else />
                </span>
              </Tooltip>
              <span class="page-title">{{ title }}</span>
            </div>
            <div class="header-right">
              <div v-if="tabs.length" class="header-tabs">
                <span
                  v-for="t in tabs"
                  :key="t.key"
                  class="header-tab"
                  :class="{ active: activeTab === t.key }"
                  @click="onTab(t.key)"
                >
                  {{ t.label }}
                </span>
              </div>
              <TaskCenter class="header-task" />
            </div>
          </div>
        </Layout.Header>
        <Layout.Content :style="{ padding: '24px', background: '#f5f5f5' }">
          <router-view />
        </Layout.Content>
      </Layout>
    </Layout>
  </ConfigProvider>
</template>

<style scoped>
/* Sider 内层 children 容器也要撑满高度:否则 footer 会贴到菜单下方而不是屏幕底部 */
:deep(.ant-layout-sider-children) {
  display: flex;
  flex-direction: column;
  height: 100%;
}
/* 一级导航与子菜单标题同字号(18px)，保证「服务」与「网关」一致 */
:deep(.app-sider .ant-menu-item),
:deep(.app-sider .ant-menu-submenu-title) {
  font-size: 18px;
}
/* 二级子菜单项字号更小 */
:deep(.app-sider .ant-menu-sub.ant-menu-inline .ant-menu-item) {
  font-size: 14px;
  height: 34px;
  line-height: 34px;
}
/* 增强已选中的子菜单项：主色底 + 左侧强调条 + 加粗 */
:deep(.app-sider .ant-menu-sub .ant-menu-item-selected) {
  font-weight: 600;
  background: #e6f4ff;
  box-shadow: inset 3px 0 0 #1677ff;
}
.header-row {
  display: flex;
  align-items: stretch;
  justify-content: space-between;
  height: 100%;
}
.header-left {
  display: flex;
  align-items: center;
  gap: 12px;
}
.header-right {
  display: flex;
  align-items: stretch;
  gap: 16px;
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
  border-radius: 6px;
  font-size: 16px;
  color: #666;
  cursor: pointer;
}
.collapse-btn:hover {
  background: #f0f0f0;
  color: #333;
}
.page-title {
  font-size: 24px;
  font-weight: 700;
}
.header-tabs {
  display: flex;
  gap: 24px;
  align-items: flex-end;
  height: 100%;
}
.header-tab {
  cursor: pointer;
  display: flex;
  align-items: flex-end;
  padding: 0 4px 10px;
  font-size: 16px;
  line-height: 1;
  color: #666;
  border-bottom: 2px solid transparent;
}
.header-tab:hover {
  color: #333;
}
.header-tab.active {
  color: #1677ff;
  border-bottom-color: #1677ff;
}
</style>