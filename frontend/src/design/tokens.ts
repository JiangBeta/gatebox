/**
 * 设计 token —— 全站颜色 / 字号 / 间距 / 圆角的唯一来源。
 *
 * 规则（ADR-032 / docs/architecture.md §9.3）：`.vue` 内禁止样式字面量，
 * 一律引用本文件；由 stylelint 强制。「字号大一点」= 改 token 或换档，
 * 不是逐页讨论项。
 */
import type { ThemeConfig } from 'ant-design-vue/es/config-provider/context'

export const colors = {
  primary: '#1677ff',
  primaryHover: '#4096ff',
  primaryActive: '#0958d9',
  primaryBg: '#e6f4ff',
  success: '#52c41a',
  warning: '#faad14',
  error: '#ff4d4f',
  text: 'rgba(0,0,0,0.88)',
  textSecondary: 'rgba(0,0,0,0.45)',
  border: '#d9d9d9',
  borderSecondary: '#f0f0f0',
  bgLayout: '#f5f5f5',
  bgContainer: '#ffffff',
}

/** 字号档位（唯一来源）。 */
export const fontSize = {
  xs: '12px',
  sm: '14px',
  base: '14px',
  lg: '16px',
  xl: '20px',
  xxl: '24px',
}

/** 间距（8px 基准网格）。 */
export const spacing = {
  xs: '4px',
  sm: '8px',
  md: '16px',
  lg: '24px',
  xl: '32px',
  xxl: '48px',
}

/** 圆角档位。 */
export const radius = {
  sm: '4px',
  base: '6px',
  lg: '8px',
  xl: '12px',
}

export const fontFamily =
  "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif"

/** antd ConfigProvider 的主题配置（正确形态：token / components / algorithm）。 */
export const antdTheme: ThemeConfig = {
  token: {
    colorPrimary: colors.primary,
    colorSuccess: colors.success,
    colorWarning: colors.warning,
    colorError: colors.error,
    borderRadius: 6,
    fontFamily,
    fontSize: 14,
  },
}

export default { colors, fontSize, spacing, radius, fontFamily, antdTheme }
