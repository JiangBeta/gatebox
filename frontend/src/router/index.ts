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
        { key: 'cert', label: '域名管理' },
        { key: 'credential', label: 'DNS 凭证' },
      ],
    },
  },
  {
    path: '/services',
    component: () => import('../views/ServicesView.vue'),
    meta: { title: '服务' },
  },
  {
    path: '/services/mosdns',
    component: () => import('../views/services/MosdnsView.vue'),
    meta: {
      title: 'MosDNS',
      component: 'mosdns',
      tabs: [
        { key: 'status', label: '状态' },
        { key: 'basic', label: '基础设置' },
        { key: 'hosts', label: '内网解析' },
        { key: 'rules', label: '规则' },
        { key: 'geodata', label: '数据库' },
        { key: 'config', label: '配置文件' },
        { key: 'logs', label: '日志' },
      ],
    },
  },
  {
    path: '/extensions',
    component: () => import('../views/ExtensionView.vue'),
    meta: {
      title: '扩展',
      tabs: [
        { key: 'components', label: '组件' },
        { key: 'plugins', label: '插件' },
      ],
    },
  },
  {
    path: '/settings',
    component: () => import('../views/SettingsView.vue'),
    meta: {
      title: '设置',
      tabs: [
        { key: 'general', label: '常规' },
        { key: 'variables', label: '变量' },
      ],
    },
  },
]

export default createRouter({
  history: createWebHashHistory(),
  routes,
})
