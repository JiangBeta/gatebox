<script setup lang="ts">
import { ref, computed, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  ConfigProvider, Layout, Menu, Tooltip,
} from 'ant-design-vue'
import {
  DashboardOutlined, ApiOutlined, ContainerOutlined, GlobalOutlined,
  SettingOutlined, GithubOutlined, BookOutlined, TranslationOutlined,
  BulbOutlined, LogoutOutlined, MenuFoldOutlined, MenuUnfoldOutlined,
} from '@ant-design/icons-vue'
import theme from './theme'

interface TabMeta {
  key: string
  label: string
}

const route = useRoute()
const router = useRouter()

const collapsed = ref(false)

const menuItems = [
  { key: '/dashboard', label: '仪表盘', icon: () => h(DashboardOutlined) },
  // 网关:无 caddy 官方图标,用向上的代理箭头(ApiOutlined)近似
  { key: '/gateway', label: '网关', icon: () => h(ApiOutlined) },
  { key: '/docker', label: '容器', icon: () => h(ContainerOutlined) },
  { key: '/domain', label: '域名', icon: () => h(GlobalOutlined) },
  { key: '/settings', label: '设置', icon: () => h(SettingOutlined) },
]

// 底部功能区:icon + hover tooltip
const footerItems = [
  { name: 'GitHub', icon: () => h(GithubOutlined), href: 'https://github.com/JiangBeta/gatebox' },
  { name: '文档', icon: () => h(BookOutlined), href: 'https://gatebox.cn' },
  { name: '语言', icon: () => h(TranslationOutlined), disabled: true },
  { name: '主题', icon: () => h(BulbOutlined), disabled: true },
]

const activeKey = computed(() => route.path)
const title = computed(() => (route.meta.title as string) || 'GateBox')
const tabs = computed<TabMeta[]>(() => (route.meta.tabs as TabMeta[]) || [])
const activeTab = computed(() => (route.query.tab as string) || tabs.value[0]?.key || '')

function onMenu(info: { key: string }) {
  router.push(info.key)
}

function onTab(key: string) {
  router.replace({ query: { ...route.query, tab: key } })
}
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
          :inline-collapsed="collapsed"
          :selected-keys="[activeKey]"
          :items="menuItems"
          @click="onMenu"
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
                  @click="collapsed = !collapsed"
                >
                  <MenuUnfoldOutlined v-if="collapsed" />
                  <MenuFoldOutlined v-else />
                </span>
              </Tooltip>
              <span class="page-title">{{ title }}</span>
            </div>
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
/* 一级导航文字:18px */
:deep(.app-sider .ant-menu-item) {
  font-size: 18px;
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