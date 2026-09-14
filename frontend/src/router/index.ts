import { createRouter, createWebHashHistory } from 'vue-router'

const routes = [
  { path: '/', redirect: '/dashboard' },
  { path: '/dashboard', component: () => import('../views/DashboardView.vue'), meta: { title: '仪表盘' } },
  {
    path: '/gateway',
    component: () => import('../views/GatewayView.vue'),
    meta: {
      title: '网关',
      tabs: [
        { key: 'routes', label: '代理' },
        { key: 'ports', label: '端口' },
        { key: 'fragments', label: 'Caddy 片段' },
        { key: 'variables', label: '变量' },
      ],
    },
  },
  {
    path: '/docker',
    component: () => import('../views/DockerView.vue'),
    meta: {
      title: '容器',
      tabs: [
        { key: 'containers', label: '容器' },
        { key: 'compose', label: '编排' },
        { key: 'images', label: '镜像' },
        { key: 'networks', label: '网络' },
        { key: 'volumes', label: '存储卷' },
        { key: 'variables', label: '变量' },
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
  { path: '/network', component: () => import('../views/NetworkView.vue'), meta: { title: '网络' } },
  {
    path: '/extensions',
    component: () => import('../views/ExtensionView.vue'),
    meta: { title: '扩展' },
  },
  { path: '/settings', component: () => import('../views/SettingsView.vue'), meta: { title: '设置' } },
]

export default createRouter({
  history: createWebHashHistory(),
  routes,
})
