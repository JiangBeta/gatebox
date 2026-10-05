/**
 * 设计 token —— 全站颜色 / 字号 / 间距 / 圆角 / 阴影的**唯一来源**。
 *
 * 规则（ADR-032 / docs/architecture.md §9.3）：`.vue` 内禁止样式字面量，
 * 一律引用本文件（TS 侧）或 `tokens.css` 的 CSS 变量（模板侧）；由 stylelint 强制。
 * 「字号大一点」= 改 token 或换档，不是逐页讨论项。
 *
 * 本文件之前有两个来源（`src/theme/*` 与本文件），App.vue 还把错误形态的对象
 * 传给了 ConfigProvider，导致 antd 主题配置整体失效。现已合并到此处。
 */
import { theme as antdThemeAlgo } from 'ant-design-vue'
import type { ThemeConfig } from 'ant-design-vue/es/config-provider/context'

/** 品牌色板（不随主题变化的部分，数值同 8091 原型亮色主题）。 */
export const palette = {
  primary: '#1677ff',
  primaryHover: '#4096ff',
  primaryActive: '#0958d9',
  primaryBg: '#e6f4ff',
  success: '#22c55e',
  warning: '#f59e0b',
  error: '#ef4444',
  info: '#1677ff',
}

/**
 * 语义色（亮色）。数值与 8091 原型的 `[data-theme=light]` 一一对应；
 * 暗色值见 tokens.css 的 [data-theme='dark']，同样抄原型。
 */
export const colors = {
  ...palette,
  text: '#1f2329',
  textSecondary: '#6b7280',
  textTertiary: 'rgba(0,0,0,0.25)',
  border: '#dfe3e8',
  borderSecondary: '#e3e6eb',
  bgLayout: '#f5f6f8',
  bgContainer: '#ffffff',
  bgElevated: '#ffffff',
  headerBg: '#fafbfc',
  softBg: '#f6f8fa',
  siderBg: '#001529',
  siderText: '#c9d1d9',
  hover: '#f7f9ff',
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

/** 阴影档位。 */
export const shadow = {
  sm: '0 1px 2px 0 rgba(0, 0, 0, 0.03), 0 1px 6px -1px rgba(0, 0, 0, 0.02)',
  base: '0 6px 16px 0 rgba(0, 0, 0, 0.08), 0 3px 6px -4px rgba(0, 0, 0, 0.12)',
}

/** 动效档位。 */
export const motion = {
  fast: '0.1s',
  base: '0.2s',
  slow: '0.3s',
  ease: 'cubic-bezier(0.645, 0.045, 0.355, 1)',
}

export const fontFamily =
  "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, 'Noto Sans', sans-serif"

/** 排版数值。 */
export const typography = {
  fontSize,
  fontFamily,
  lineHeight: { tight: '1.4', base: '1.5715', loose: '1.8' },
}

/** 组件间距。侧栏宽度与 8091 原型一致（190 / 64）。 */
export const layout = {
  formItem: '24px',
  card: '16px',
  siderWidth: '190px',
  siderCollapsedWidth: '64px',
  headerHeight: '56px',
}

// antd-vue 的 ThemeConfig.components 只声明了通用 ComponentToken，逐组件专属 token
// （Menu.itemSelectedBg 等）不在类型里，故此处集中一次转换，避免每处 as any。
type Components = NonNullable<ThemeConfig['components']>
const asComponents = (c: Record<string, Record<string, unknown>>) => c as unknown as Components

/** 亮色主题（antd ConfigProvider 的正确形态：token / components / algorithm）。 */
export const antdTheme: ThemeConfig = {
  token: {
    colorPrimary: palette.primary,
    colorSuccess: palette.success,
    colorWarning: palette.warning,
    colorError: palette.error,
    colorInfo: palette.info,
    colorBgLayout: colors.bgLayout,
    colorBgContainer: colors.bgContainer,
    colorText: colors.text,
    colorTextSecondary: colors.textSecondary,
    colorBorder: colors.border,
    colorBorderSecondary: colors.borderSecondary,
    borderRadius: 6,
    fontFamily,
    fontSize: 14,
    motionDurationFast: motion.fast,
    motionDurationMid: motion.base,
    motionDurationSlow: motion.slow,
    motionEaseInOut: motion.ease,
  },
  components: asComponents({
    Layout: { headerBg: colors.bgContainer, siderBg: colors.siderBg, bodyBg: colors.bgLayout },
    Menu: {
      // 侧栏是深底（原型亮色也是 #001529），菜单文字走 siderText 而不是默认黑。
      itemBg: 'transparent',
      subMenuItemBg: 'transparent',
      itemColor: colors.siderText,
      itemSelectedBg: palette.primaryBg,
      itemSelectedColor: palette.primary,
      itemHoverBg: 'rgba(255,255,255,0.08)',
      itemBorderRadius: radius.base,
    },
    Table: { headerBg: colors.headerBg, rowHoverBg: colors.hover },
    Card: { borderRadiusLG: radius.lg },
  }),
}

/** 暗色语义色：抄 8091 原型的 `:root,[data-theme=dark]`。 */
export const darkColors = {
  text: '#e6e9ef',
  textSecondary: '#98a2b3',
  textTertiary: 'rgba(230,233,239,0.45)',
  border: '#2a3342',
  borderSecondary: '#2a3342',
  bgLayout: '#12161d',
  bgContainer: '#1b212c',
  bgElevated: '#202836',
  headerBg: '#171d27',
  softBg: '#202836',
  siderBg: '#0b0f16',
  siderText: '#c9d1d9',
  hover: '#1d2635',
}
const DARK = darkColors

/** 暗色主题：同一套 token + antd 官方暗色算法。 */
export const antdThemeDark: ThemeConfig = {
  ...antdTheme,
  algorithm: antdThemeAlgo.darkAlgorithm,
  token: {
    ...antdTheme.token,
    colorPrimary: '#4096ff',
    colorSuccess: '#3fb950',
    colorWarning: '#d29922',
    colorError: '#f85149',
    colorBgLayout: DARK.bgLayout,
    colorBgContainer: DARK.bgContainer,
    colorBgElevated: DARK.bgElevated,
    colorText: DARK.text,
    colorTextSecondary: DARK.textSecondary,
    colorTextTertiary: DARK.textTertiary,
    colorBorder: DARK.border,
    colorBorderSecondary: DARK.borderSecondary,
  },
  components: asComponents({
    Layout: { headerBg: DARK.headerBg, siderBg: DARK.siderBg, bodyBg: DARK.bgLayout },
    Menu: {
      itemBg: 'transparent',
      subMenuItemBg: 'transparent',
      itemSelectedBg: 'rgba(64,150,255,0.16)',
      itemSelectedColor: '#4096ff',
      itemHoverBg: DARK.hover,
      itemBorderRadius: radius.base,
    },
    Table: { headerBg: DARK.headerBg, rowHoverBg: DARK.hover },
    Card: { borderRadiusLG: radius.lg },
  }),
}

export type ColorScheme = 'light' | 'dark'

/** 按配色方案取 antd 主题。 */
export function antdThemeOf(scheme: ColorScheme): ThemeConfig {
  return scheme === 'dark' ? antdThemeDark : antdTheme
}

export default {
  palette,
  colors,
  darkColors,
  fontSize,
  spacing,
  radius,
  shadow,
  motion,
  fontFamily,
  typography,
  layout,
  antdTheme,
  antdThemeDark,
  antdThemeOf,
}
