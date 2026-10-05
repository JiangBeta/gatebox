import { computed, ref, watch, type Ref } from 'vue'
import { antdThemeOf, type ColorScheme } from '@/design/tokens'

const STORAGE_KEY = 'gatebox:color-scheme'

function initial(): ColorScheme {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved === 'light' || saved === 'dark') return saved
  // 默认深色：8091 原型就是深色优先，tokens.css 也按深色写根变量。
  return 'dark'
}

// 单一来源：模块内共享同一个响应式值（多个组件读同一份，不各存各的）。
const scheme: Ref<ColorScheme> = ref(initial())

watch(
  scheme,
  (v) => {
    document.documentElement.dataset.theme = v
    localStorage.setItem(STORAGE_KEY, v)
  },
  { immediate: true },
)

/**
 * 亮/暗主题。切主题只改 <html data-theme>（自定义样式走 CSS 变量）+
 * ConfigProvider 的 algorithm（antd 组件走 token），页面代码零改动。
 */
export function useColorScheme() {
  return {
    scheme,
    // 必须是可写 computed，不能用 { get, set } 普通对象：
    // 对象永远是 truthy，模板里 `isDark ? 'x' : 'y'` 会恒为 x 且不随切换更新
    // （App.vue 的切换按钮文案就是这么坏的）。
    isDark: computed({
      get: () => scheme.value === 'dark',
      set: (v: boolean) => {
        scheme.value = v ? 'dark' : 'light'
      },
    }),
    antdTheme: () => antdThemeOf(scheme.value),
    toggle: () => {
      scheme.value = scheme.value === 'dark' ? 'light' : 'dark'
    },
  }
}
