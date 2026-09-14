<script setup lang="ts">
import { ref, computed, h } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Layout, Menu, ConfigProvider } from 'ant-design-vue'
import {
  DashboardOutlined, ApiOutlined, ContainerOutlined, GlobalOutlined,
  ClusterOutlined, SettingOutlined,
} from '@ant-design/icons-vue'
import { antdTheme, colors, spacing, fontSize } from '@/design/tokens'

const route = useRoute()
const router = useRouter()
const collapsed = ref(false)

const menuItems = [
  { key: '/dashboard', label: '首页', icon: () => h(DashboardOutlined) },
  { key: '/gateway', label: '网关', icon: () => h(ApiOutlined) },
  { key: '/containers', label: '容器', icon: () => h(ContainerOutlined) },
  { key: '/domain', label: '域名', icon: () => h(GlobalOutlined) },
  { key: '/network', label: '网络', icon: () => h(ClusterOutlined) },
  { key: '/settings', label: '设置', icon: () => h(SettingOutlined) },
]

const activeKey = computed(() => route.path)
const title = computed(() => (route.meta.title as string) || 'GateBox')
</script>

<template>
  <ConfigProvider :theme="antdTheme">
    <Layout :style="{ minHeight: '100vh' }">
      <Layout.Sider
        :width="200"
        :collapsed-width="56"
        :collapsed="collapsed"
        :style="{
          background: colors.bgContainer,
          borderRight: `1px solid ${colors.borderSecondary}`,
        }"
      >
        <div
          :style="{
            padding: spacing.md,
            fontSize: fontSize.xl,
            fontWeight: 700,
            whiteSpace: 'nowrap',
            overflow: 'hidden',
          }"
        >
          <template v-if="!collapsed">
            GateBox
          </template>
          <template v-else>
            G
          </template>
        </div>
        <Menu
          mode="inline"
          :selected-keys="[activeKey]"
          :items="menuItems"
          :style="{ borderInlineEnd: 0 }"
          @click="(info: { key: string }) => router.push(info.key)"
        />
      </Layout.Sider>

      <Layout>
        <Layout.Header
          :style="{
            background: colors.bgContainer,
            borderBottom: `1px solid ${colors.borderSecondary}`,
            padding: `0 ${spacing.lg}`,
          }"
        >
          <span :style="{ fontSize: fontSize.xxl, fontWeight: 700 }">{{ title }}</span>
        </Layout.Header>
        <Layout.Content :style="{ padding: spacing.lg, background: colors.bgLayout }">
          <router-view />
        </Layout.Content>
      </Layout>
    </Layout>
  </ConfigProvider>
</template>
