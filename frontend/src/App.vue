<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { NLayout, NLayoutSider, NLayoutHeader, NLayoutContent, NMenu, NMessageProvider } from 'naive-ui'

interface TabMeta {
  key: string
  label: string
}

const route = useRoute()
const router = useRouter()

const menuOptions = [
  { label: '仪表盘', key: '/dashboard' },
  { label: '网关', key: '/gateway' },
  { label: 'Docker', key: '/docker' },
  { label: '域名', key: '/domain' },
  { label: '设置', key: '/settings' },
]

const activeKey = computed(() => route.path)
const title = computed(() => (route.meta.title as string) || 'GateBox')
const tabs = computed<TabMeta[]>(() => (route.meta.tabs as TabMeta[]) || [])
const activeTab = computed(() => (route.query.tab as string) || tabs.value[0]?.key || '')

function onMenu(key: string) {
  router.push(key)
}

function onTab(key: string) {
  router.replace({ query: { ...route.query, tab: key } })
}
</script>

<template>
  <n-message-provider>
    <n-layout has-sider style="height: 100vh">
      <n-layout-sider bordered width="200" content-style="display: flex; flex-direction: column; height: 100%">
        <div style="padding: 16px; font-size: 20px; font-weight: 700">
          GateBox <span style="font-size: 14px; color: #999">v0.1.0</span>
        </div>
        <n-menu :value="activeKey" :options="menuOptions" @update:value="onMenu" style="flex: 1; font-size: 16px" />
        <div style="padding: 8px 16px; font-size: 14px; color: #999">
          <a href="https://github.com/JiangBeta/gatebox" target="_blank">GitHub</a>
          ·
          <a href="https://gatebox.cn" target="_blank">文档</a>
          ·
          <span>🌐 🌓</span>
        </div>
        <div style="padding: 12px 16px; border-top: 1px solid #eee; cursor: pointer">退出</div>
      </n-layout-sider>
      <n-layout>
        <n-layout-header bordered style="padding: 0 24px; height: 80px">
          <div class="header-row">
            <span class="page-title">{{ title }}</span>
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
        </n-layout-header>
        <n-layout-content content-style="padding: 16px">
          <router-view />
        </n-layout-content>
      </n-layout>
    </n-layout>
  </n-message-provider>
</template>

<style scoped>
.header-row {
  display: flex;
  align-items: stretch;
  justify-content: space-between;
  height: 100%;
}
.page-title {
  font-size: 24px;
  font-weight: 700;
  align-self: center;
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
  align-items: center;
  padding: 6px 4px 10px;
  font-size: 16px;
  color: #666;
  border-bottom: 2px solid transparent;
}
.header-tab:hover {
  color: #333;
}
.header-tab.active {
  color: #18a058;
  border-bottom-color: #18a058;
  font-weight: 600;
}
</style>
