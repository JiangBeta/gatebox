import { createRouter, createWebHashHistory } from 'vue-router'

/**
 * 一级导航与 8091 原型一致（docs/v4.1/views.md §3）：
 * 服务 / 域名 / 容器 / 应用商店 / 组件 / 监控 / 设置。
 *
 * Tab 用 `?tab=` 切；旧路径全部保留重定向，老书签不至于 404。
 */
const routes = [
  { path: '/', redirect: '/services' },
  { path: '/login', component: () => import('../views/LoginView.vue'), meta: { title: '登录' } },
  {
    path: '/services',
    component: () => import('../modules/config/views/Index.vue'),
    meta: {
      title: '服务',
      tabs: [
        { key: 'routes', label: '路由' },
        { key: 'services', label: '服务' },
        { key: 'middlewares', label: '中间件' },
      ],
    },
  },
  {
    path: '/domains',
    component: () => import('../views/domains/Index.vue'),
    meta: {
      title: '域名',
      tabs: [
        { key: 'overview', label: '概览' },
        { key: 'domains', label: '域名' },
        { key: 'credentials', label: '凭证' },
      ],
    },
  },
  {
    path: '/deploy',
    component: () => import('../views/deploy/Index.vue'),
    meta: {
      title: '容器',
      tabs: [
        { key: 'overview', label: '概览' },
        { key: 'compose', label: '编排' },
        { key: 'images', label: '镜像' },
        { key: 'networks', label: '网络' },
        { key: 'volumes', label: '存储卷' },
        { key: 'registry', label: '仓库凭证' },
      ],
    },
  },
  {
    path: '/store',
    component: () => import('../views/store/Index.vue'),
    meta: { title: '应用商店', tabs: [{ key: 'store', label: '应用商店' }] },
  },
  {
    path: '/components',
    component: () => import('../views/components/Index.vue'),
    meta: {
      title: '组件',
      tabs: [
        { key: 'ability', label: '能力' },
        { key: 'components', label: '组件' },
        { key: 'plugins', label: '插件' },
        { key: 'topology', label: '流程' },
      ],
    },
  },
  {
    path: '/monitor',
    component: () => import('../views/monitor/Index.vue'),
    meta: { title: '监控' },
  },
  {
    path: '/settings',
    component: () => import('../views/settings/Index.vue'),
    meta: {
      title: '设置',
      tabs: [
        { key: 'system', label: '系统' },
        { key: 'users', label: '用户' },
        { key: 'providers', label: 'DNS 供应商' },
        { key: 'hosts', label: '主机' },
        { key: 'variables', label: '变量' },
        { key: 'ports', label: '端口' },
      ],
    },
  },

  // ── 网关运行时（Caddy 直管，V4.0 旧面）────────────────────────────────────
  // V4.1 已由「服务 → 路由 / 服务 / 中间件」对象面接管；旧页面保留直管能力
  // （生效中的 Caddyfile 分组、监听端口、Caddy 片段），入口在设置 → 系统。
  {
    path: '/gateway',
    component: () => import('../views/GatewayView.vue'),
    meta: {
      title: '网关运行时',
      tabs: [
        { key: 'routes', label: '代理' },
        { key: 'ports', label: '端口' },
        { key: 'fragments', label: 'Caddy 片段' },
      ],
    },
  },
  {
    path: '/tasks',
    component: () => import('../modules/tasks/views/Index.vue'),
    meta: { title: '任务' },
  },

  // ── 旧路径重定向 ────────────────────────────────────────────────────────
  { path: '/config', redirect: { path: '/services', query: { tab: 'routes' } } },
  { path: '/domain', redirect: { path: '/domains', query: { tab: 'overview' } } },
  { path: '/docker', redirect: { path: '/deploy', query: { tab: 'overview' } } },
  { path: '/extensions', redirect: { path: '/components', query: { tab: 'components' } } },
  { path: '/dashboard', redirect: '/monitor' },
  { path: '/topology', redirect: { path: '/components', query: { tab: 'topology' } } },
  // 独立进程组件的服务页并入「组件」，聚焦参数保留。
  {
    path: '/services/component/:id',
    redirect: (to) => ({ path: '/components', query: { tab: 'components', component: to.params.id } }),
  },
]

const router = createRouter({
  history: createWebHashHistory(),
  routes,
})

// 「服务」现在是对象面（路由/服务/中间件）。旧的服务页用 `?component=` 聚焦某个
// 独立进程组件，那个组件管理已经并入「组件 → 组件」，这里带过去，别把人丢在对象面上。
router.beforeEach((to) => {
  if (to.path === '/services' && typeof to.query.component === 'string' && to.query.component) {
    return { path: '/components', query: { tab: 'components', component: to.query.component } }
  }
  return true
})

export default router