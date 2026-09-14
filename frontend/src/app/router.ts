/**
 * 应用路由。一级导航 6 入口：首页 / 网关 / 容器 / 域名 / 网络 / 设置。
 *
 * Tab 一律用 query（`?tab=`）切换，不建嵌套路由（docs/layout.md）。
 */
import { createRouter, createWebHashHistory, type RouteRecordRaw } from 'vue-router'

const routes: RouteRecordRaw[] = [
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', component: () => import('@/modules/dashboard/views/Index.vue'), meta: { title: '首页' } },
  { path: '/gateway', component: () => import('@/modules/gateway/views/Index.vue'), meta: { title: '网关' } },
  { path: '/containers', component: () => import('@/modules/containers/views/Index.vue'), meta: { title: '容器' } },
  { path: '/domain', component: () => import('@/modules/domain/views/Index.vue'), meta: { title: '域名' } },
  { path: '/network', component: () => import('@/modules/network/views/Index.vue'), meta: { title: '网络' } },
  { path: '/settings', component: () => import('@/modules/system/views/Index.vue'), meta: { title: '设置' } },
]

export default createRouter({
  history: createWebHashHistory(),
  routes,
})
