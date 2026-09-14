import { h } from 'vue'

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