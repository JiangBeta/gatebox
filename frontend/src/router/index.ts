import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '仪表盘' } },
  { path: '/gateway', component: () => import('../views/GatewayView.vue'), meta: { title: '网关' } },
  {
    path: '/docker',
    component: () => import('../views/DockerView.vue'),
    meta: {
      title: 'Docker',
      tabs: [
        { key: 'containers', label: '容器' },
        { key: 'compose', label: '编排' },
        { key: 'images', label: '镜像' },
        { key: 'networks', label: '网络' },
        { key: 'volumes', label: '存储卷' },
      ],
    },
  },
  {
    path: '/domain',
    component: () => import('../views/DomainView.vue'),
    meta: {
      title: '域名',
      tabs: [
        { key: 'overview', label: '概览' },
        { key: 'manage', label: '域名管理' },
        { key: 'cert', label: 'SSL 证书' },
        { key: 'credential', label: 'DNS 凭证' },
      ],
    },
  },
  { path: '/settings', component: () => import('../views/SettingsView.vue'), meta: { title: '设置' } },
]

export default createRouter({
  history: createWebHashHistory(),
  routes,
})
