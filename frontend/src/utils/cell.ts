import { h, type VNode } from 'vue'
import { Tooltip } from 'ant-design-vue'

/**
 * 表格单元格安全文本包装。
 *
 * 排查结论(2026-09-08):rc-table 对「customRender 返回裸字符串 / 裸 <span> 根 /
 * <Typography.Text> 根或子级」的单元格,在响应式重渲染时会把数字键写入
 * CSSStyleDeclaration,抛 `Indexed property setter is not supported`,导致整页崩。
 * 已验证安全形态 = 单元格根用 div(仅含 span/文本),或组件根(Tag/Tooltip/Popover)。
 *
 * @param text 文本内容
 * @param spanStyle 可选 span 内联样式
 */
export function statCell(text: any, spanStyle?: string): any {
  return h('div', {}, h('span', { style: spanStyle }, String(text ?? '')))
}

/**
 * 操作色板（views.md §6 / 原型 .act-btn 的取值）。
 * 语义固定：编辑紫、启停绿/红、重启青、日志蓝、删除红、复制/占位灰。
 * 页面自己按语义挑色，别按位置挑——同一个红在路由页是「停止」，
 * 在对象列表是「删除」，用户看的是图标不是颜色。
 */
export const OP_COLOR = {
  edit: '#722ed1',
  stop: '#ef4444',
  pause: '#fa8c16',
  start: '#52c41a',
  restart: '#13c2c2',
  logs: '#1677ff',
  view: '#1677ff',
  upgrade: '#1677ff',
  compose: '#fa8c16',
  adopt: '#fa8c16',
  console: '#722ed1',
  deploy: '#52c41a',
  copy: '#8b95a7',
  delete: '#ef4444',
  disabled: '#8b95a7',
} as const

export interface OpSpec {
  tip: string
  icon: any
  color?: string
  disabled?: boolean
  /** disabled 时额外说明原因，直接显示在 tooltip 里。 */
  disabledReason?: string
  onClick?: () => void
}

/**
 * 操作列图标按钮（原型 .act-btn）：无背景无边框、纯图标 + tooltip。
 * 用 span 包一层：disabled 的 <button> 不派发 hover 事件，tooltip 会失效。
 */
export function opButton(op: OpSpec): VNode {
  const btn = h(
    'button',
    {
      class: 'row-ops-btn',
      type: 'button',
      disabled: Boolean(op.disabled),
      style: { color: op.disabled ? OP_COLOR.disabled : (op.color ?? 'currentColor') },
      onClick: (e: MouseEvent) => {
        // 行本身可点（点整行进编辑），操作按钮不能连带触发行点击。
        e.stopPropagation()
        if (!op.disabled) op.onClick?.()
      },
    },
    [h(op.icon)],
  )
  const title = op.disabled && op.disabledReason ? `${op.tip} · ${op.disabledReason}` : op.tip
  if (op.disabled) {
    return h(Tooltip, { title }, { default: () => h('span', { class: 'row-ops-wrap' }, [btn]) })
  }
  return h(Tooltip, { title }, { default: () => btn })
}

/** 操作列容器：一行图标按钮，宽度按内容，别让表格最后一列空一大片。 */
export function opCell(...ops: (OpSpec | null | undefined | false)[]): VNode {
  return h('div', { class: 'row-ops' }, ops.filter(Boolean).map((o) => opButton(o as OpSpec)))
}