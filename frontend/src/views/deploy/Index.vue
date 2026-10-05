<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import type { V41Object } from '@/api/objects'
import ContainerTab from '../docker/ContainerTab.vue'
import ComposeTab from '../docker/ComposeTab.vue'
import ImageTab from '../docker/ImageTab.vue'
import NetworkTab from '../docker/NetworkTab.vue'
import VolumeTab from '../docker/VolumeTab.vue'
import ObjectKindPage from '@/app/components/objects/ObjectKindPage.vue'
import StatCards, { type StatCard } from '@/app/components/StatCards.vue'
import { useHostFilter } from '@/app/composables/useHostFilter'
import { listObjects } from '@/api/objects'
import { listContainers, type ContainerView } from '@/api/docker'
import {
  CloudServerOutlined, AppstoreOutlined, ContainerOutlined, ShareAltOutlined,
  ThunderboltOutlined, MinusOutlined,
} from '@ant-design/icons-vue'

/**
 * 容器（8091 原型：概览 / 编排 / 镜像 / 网络 / 存储卷 / 仓库凭证）。
 *
 * 前五个 tab 是 Docker 运行时面（容器 / compose / 镜像 / 网络 / 卷），
 * 「仓库凭证」是 V4.1 对象面（`registry-credential`）——私有镜像源的账号密码。
 *
 * 顶部统计卡跟随主机筛选（原型 hostMatch 过滤后计数）：换主机时数字跟着变，
 * 否则卡片和下面表格说的就不是同一件事了。
 */
const route = useRoute()
const router = useRouter()
const active = computed(() => (route.query.tab as string) || 'overview')

// 统计卡要跨四个数据源（host 对象 / deployment 对象 / 容器 / route 对象），
// 这里各自拉一份最小集合，不给后端加「汇总接口」——那会让同一口径散在两处。
const containers = ref<ContainerView[]>([])
const deployments = ref<V41Object[]>([])
const routes = ref<V41Object[]>([])
const { match: hostMatch, online, hosts, reload: reloadHosts } = useHostFilter()

async function loadCards() {
  const tasks: Promise<void>[] = [
    listContainers().then((r) => (containers.value = r)).catch(() => undefined),
    listObjects('deployment')
      .then((r) => (deployments.value = r.data || []))
      .catch(() => undefined),
    listObjects('route')
      .then((r) => (routes.value = r.data || []))
      .catch(() => undefined),
  ]
  await Promise.all(tasks)
}

const isRunning = (s?: unknown) =>
  String((s as { state?: string })?.state ?? '') === '运行中'

/** deploy-overview 卡：Agent / 编排 / 容器 / 路由（跟随主机筛选）。 */
const overviewCards = computed<StatCard[]>(() => {
  const cs = containers.value.filter((c) => hostMatch(c.host))
  const running = cs.filter((c) => c.state === 'running').length
  const ds = deployments.value.filter((d) => hostMatch(hostOf(d)))
  const depRunning = ds.filter((d) => isRunning(d.status)).length
  const rt = routes.value
  const rtOk = rt.filter((r) => isRunning(r.status)).length
  const agentsOn = online.value.length
  const total = hosts.value.length
  return [
    {
      title: 'Agent',
      main: total,
      subHTML: `在线 <b style="color:#22c55e">${agentsOn}</b> · 离线 <b style="color:#ef4444">${total - agentsOn}</b>`,
      icon: CloudServerOutlined,
      tone: '#722ed1',
      link: { page: '/settings', tab: 'host' },
    },
    {
      title: '编排',
      main: ds.length,
      subHTML: `运行中 <b style="color:#22c55e">${depRunning}</b> · 未运行 <b style="color:#8b95a7">${ds.length - depRunning}</b>`,
      icon: AppstoreOutlined,
      tone: '#13c2c2',
      link: { page: '/deploy', tab: 'compose' },
    },
    {
      title: '容器',
      main: cs.length,
      subHTML: `运行中 <b style="color:#22c55e">${running}</b> · 未运行 <b style="color:#8b95a7">${cs.length - running}</b>`,
      icon: ContainerOutlined,
      tone: '#1677ff',
      link: { page: '/deploy', tab: 'overview' },
    },
    {
      title: '路由',
      main: rt.length,
      subHTML: `已生效 <b style="color:#22c55e">${rtOk}</b> · 告警 <b style="color:#d29922">${rt.length - rtOk}</b>`,
      icon: ShareAltOutlined,
      tone: '#fa8c16',
      link: { page: '/services', tab: 'route' },
    },
  ]
})

/** compose-overview 卡：编排项目 / 运行中 / 未部署 / 派生路由。 */
const composeCards = computed<StatCard[]>(() => {
  const ds = deployments.value.filter((d) => hostMatch(hostOf(d)))
  const running = ds.filter((d) => isRunning(d.status)).length
  const undeployed = ds.filter((d) => {
    const s = (d.status as { state?: string } | undefined)?.state
    return !s || s === '未部署'
  }).length
  const fromCompose = routes.value.filter(
    (r) => String((r.status as { source?: string } | undefined)?.source ?? '') === 'docker',
  ).length
  return [
    { title: '编排项目', main: ds.length, sub: '部署单元', icon: ContainerOutlined, tone: '#1677ff', link: { page: '/deploy', tab: 'compose' } },
    { title: '运行中', main: running, sub: '', icon: ThunderboltOutlined, tone: '#22c55e', link: { page: '/deploy', tab: 'compose' } },
    { title: '未部署', main: undeployed, sub: '', icon: MinusOutlined, tone: '#8b95a7', link: { page: '/deploy', tab: 'compose' } },
    { title: '派生路由', main: fromCompose, sub: '来自编排', icon: ShareAltOutlined, tone: '#13c2c2', link: { page: '/services', tab: 'route' } },
  ]
})

/** 卡片只挂在原型指定的那两个 tab 上（deploy-overview / compose-overview）。 */
const cards = computed(() => {
  if (active.value === 'overview') return overviewCards.value
  if (active.value === 'compose') return composeCards.value
  return []
})

/** 对象落在哪台主机：deployment/route 的 spec.host，没有就是本机。 */
function hostOf(o: V41Object): string | undefined {
  const h = String(o.spec?.host ?? '')
  return h || undefined
}

function onCardNavigate(link: { page: string; tab?: string }) {
  router.push({ path: link.page, query: { tab: link.tab } })
}

onMounted(async () => {
  await Promise.all([reloadHosts(), loadCards()])
})

const registryColumns: ColumnType<V41Object>[] = [
  {
    title: '地址',
    dataIndex: 'url',
    key: 'url',
    ellipsis: true,
    customRender: ({ record }) => String(record.spec?.url ?? '') || '—',
  },
  {
    title: '协议',
    dataIndex: 'scheme',
    key: 'scheme',
    width: 100,
    customRender: ({ record }) => String(record.spec?.scheme ?? '') || '—',
  },
  {
    title: '账号',
    dataIndex: 'username',
    key: 'username',
    width: 140,
    customRender: ({ record }) => String(record.spec?.username ?? '') || '—',
  },
  {
    title: '校验',
    key: 'verified',
    width: 110,
    customRender: ({ record }) => (record.status?.verified ? '已验证' : '未验证'),
  },
]
</script>

<template>
  <div class="deploy-page">
    <StatCards :cards="cards" @navigate="onCardNavigate" />
    <ContainerTab v-if="active === 'overview'" />
    <ComposeTab v-else-if="active === 'compose'" />
    <ImageTab v-else-if="active === 'images'" />
    <NetworkTab v-else-if="active === 'networks'" />
    <VolumeTab v-else-if="active === 'volumes'" />
    <ObjectKindPage
      v-else-if="active === 'registry'"
      kind="registry-credential"
      title="仓库凭证"
      description="私有镜像仓库的账号密码。密码只写不回显；部署单元拉取镜像时按仓库地址匹配这里的凭证。"
      :extra-columns="registryColumns"
    />
  </div>
</template>

<style scoped>
.deploy-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-lg);
}
</style>