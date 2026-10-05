// 组件/插件 ID → 相关工具页面路由。
//
// 组件页「查看」、插件页「查看」与侧边栏共用，避免多处维护漂移。
export const TOOL_ROUTE: Record<string, string> = {
  caddy: '/gateway?tab=routes',
  acme: '/domains?tab=domains',
  docker: '/deploy?tab=overview',
  // 插件页路由由 manifest 的 ui.nav 动态注册，不再硬编码（ADR-039 §3）。
}

// toolRoute 返回组件/插件对应的工具页路由；无映射时回退组件页。
export function toolRoute(id: string): string {
  return TOOL_ROUTE[id] || '/components?tab=components'
}
