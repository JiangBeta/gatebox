import { beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick, watch } from 'vue'

// tokens 里 import 了 ant-design-vue，它在模块加载期就摸 document.getElementsByTagName。
// 本测试只关心 useColorScheme 自己的逻辑，把主题表桩掉即可。
vi.mock('@/design/tokens', () => ({
  antdThemeOf: (scheme: string) => ({ token: { colorPrimary: scheme } }),
}))

/**
 * isDark 之前是 `{get,set}` 普通对象：对象永远是 truthy，
 * 模板里的 `isDark ? '切到亮色' : '切到暗色'` 会永远显示同一句、切换后也不重渲染。
 * 修法是换成可写 computed。这里断言它是**真正的响应式布尔值**，
 * 这样模板才能自动 unwrap 并跟随切换更新。
 *
 * 本仓库 vitest 是 environment: 'node'（无 jsdom 依赖），
 * 所以这里手搓 composable 真正用到的全局：localStorage / document。
 */
function stubDOM() {
  const store = new Map<string, string>()
  const g = globalThis as Record<string, unknown>
  g.localStorage = {
    getItem: (k: string) => store.get(k) ?? null,
    setItem: (k: string, v: string) => void store.set(k, v),
    clear: () => store.clear(),
  }
  const el = { dataset: {} as Record<string, string> }
  g.document = { documentElement: el }
  return { store, el }
}

describe('useColorScheme.isDark', () => {
  beforeEach(() => {
    vi.resetModules()
  })

  it('初值 dark（原型默认深色），toggle 后 light，赋值可回写', async () => {
    const { el } = stubDOM()
    const { useColorScheme } = await import('./useColorScheme')
    const theme = useColorScheme()
    expect(theme.scheme.value).toBe('dark')
    // 必须是布尔 true，而不是一个 truthy 的对象 —— 这是旧 bug 的要害。
    expect(theme.isDark.value).toBe(true)

    theme.toggle()
    expect(theme.isDark.value).toBe(false)
    await nextTick() // watch 默认异步 flush，<html data-theme> 在这里才落
    expect(el.dataset.theme).toBe('light')

    theme.isDark.value = true
    expect(theme.scheme.value).toBe('dark')
  })

  it('跟随 scheme 变化触发监听（模板据此重渲染）', async () => {
    stubDOM()
    const { useColorScheme } = await import('./useColorScheme')
    const theme = useColorScheme()
    const seen: boolean[] = []
    watch(
      theme.isDark,
      (v) => seen.push(v),
      { immediate: true },
    )
    theme.toggle()
    await nextTick()
    theme.toggle()
    await nextTick()
    expect(seen).toEqual([true, false, true])
  })

  it('初值即落到 <html data-theme> 并持久化', async () => {
    const { store, el } = stubDOM()
    const { useColorScheme } = await import('./useColorScheme')
    const theme = useColorScheme()
    expect(theme.isDark.value).toBe(true)
    expect(el.dataset.theme).toBe('dark')
    expect(store.get('gatebox:color-scheme')).toBe('dark')
  })

  it('localStorage 里已存的偏好优先于默认值', async () => {
    const { store } = stubDOM()
    store.set('gatebox:color-scheme', 'light')
    const { useColorScheme } = await import('./useColorScheme')
    expect(useColorScheme().isDark.value).toBe(false)
  })
})
