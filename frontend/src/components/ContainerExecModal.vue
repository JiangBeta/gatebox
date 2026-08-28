<script setup lang="ts">
import { ref, onMounted, onUnmounted, computed, shallowRef } from 'vue'
import { NDrawer, NTag, NAlert, NText, NSpin, NButton } from 'naive-ui'
import { wsURL, probeShell, type ContainerView } from '../api/docker'
// 样式随本组件所在的懒加载 chunk 走,不进首屏;xterm 的 JS 仍按需动态导入
import '@xterm/xterm/css/xterm.css'

const props = defineProps<{ container: ContainerView }>()
const emit = defineEmits<{ close: [] }>()

const termHost = ref<HTMLElement | null>(null)
const connected = ref(false)
const preparing = ref(true)
const errorMsg = ref('')
const shellPath = ref('')

// xterm 实例不需要响应式代理(深度代理会拖慢终端渲染)
const term = shallowRef<any>(null)
const fitAddon = shallowRef<any>(null)
let ws: WebSocket | null = null
let resizeObserver: ResizeObserver | null = null

const title = computed(() => `控制台 · ${props.container.name}`)

function sendResize() {
  if (!term.value || ws?.readyState !== WebSocket.OPEN) return
  ws.send(JSON.stringify({ type: 'resize', cols: term.value.cols, rows: term.value.rows }))
}

async function boot() {
  // 先探测 shell:distroless / scratch 镜像没有 shell,
  // 此时要明确告知而不是丢一个空白终端(docs §5.4)
  try {
    const probe = await probeShell(props.container.id)
    if (!probe.available) {
      errorMsg.value = '该镜像不含可用的 shell（常见于 distroless / scratch 构建），无法打开控制台。'
      preparing.value = false
      return
    }
    shellPath.value = probe.shell
  } catch (e: any) {
    errorMsg.value = `探测 shell 失败：${e.message}`
    preparing.value = false
    return
  }

  // 动态导入:xterm 约 250KB,只在真正打开控制台时才加载
  const [{ Terminal }, { FitAddon }] = await Promise.all([
    import('@xterm/xterm'),
    import('@xterm/addon-fit'),
  ])

  preparing.value = false
  await new Promise((r) => setTimeout(r, 0)) // 等待 DOM 挂载

  const t = new Terminal({
    fontSize: 13,
    fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
    cursorBlink: true,
    theme: { background: '#1e1e1e', foreground: '#d4d4d4' },
  })
  const fit = new FitAddon()
  t.loadAddon(fit)
  if (!termHost.value) return
  t.open(termHost.value)
  fit.fit()
  term.value = t
  fitAddon.value = fit

  const url = wsURL(`/docker/containers/${props.container.id}/exec`, {
    shell: shellPath.value,
    cols: String(t.cols),
    rows: String(t.rows),
  })
  ws = new WebSocket(url)

  ws.onopen = () => {
    connected.value = true
    t.focus()
    sendResize()
  }
  ws.onmessage = (ev) => t.write(ev.data)
  ws.onclose = () => {
    connected.value = false
    t.write('\r\n\x1b[33m[会话已结束]\x1b[0m\r\n')
  }
  ws.onerror = () => {
    if (!errorMsg.value) errorMsg.value = '控制台连接失败'
  }

  t.onData((data: string) => {
    if (ws?.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'stdin', data }))
    }
  })

  resizeObserver = new ResizeObserver(() => {
    try {
      fit.fit()
      sendResize()
    } catch {
      /* 容器隐藏时 fit 会抛错,忽略 */
    }
  })
  if (termHost.value) resizeObserver.observe(termHost.value)
}

onMounted(boot)

onUnmounted(() => {
  resizeObserver?.disconnect()
  ws?.close()
  ws = null
  term.value?.dispose()
  term.value = null
})
</script>

<template>
  <n-drawer
    :show="true"
    placement="right"
    width="min(900px, 100vw)"
    @update:show="(v: boolean) => !v && emit('close')"
  >
    <div style="display: flex; flex-direction: column; height: 100%">
      <div style="padding: 14px 24px; border-bottom: 1px solid #eee; font-size: 16px; font-weight: 600; display: flex; align-items: center; justify-content: space-between; flex-shrink: 0">
        <span>{{ title }}</span>
        <div style="display: flex; align-items: center; gap: 8px">
          <n-tag :type="connected ? 'success' : 'default'" size="small" :bordered="false">
            {{ connected ? shellPath || '已连接' : '未连接' }}
          </n-tag>
          <n-button quaternary circle size="small" @click="emit('close')">✕</n-button>
        </div>
      </div>
      <div style="flex: 1; overflow: auto; padding: 16px 24px">

    <n-alert v-if="errorMsg" type="warning" :show-icon="true">
      {{ errorMsg }}
    </n-alert>

    <template v-else>
      <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 8px">
        控制台以容器内的默认用户运行，权限等同于在宿主机上执行 docker exec；
        30 分钟无输入会自动断开。
      </n-text>
      <div v-if="preparing" class="term-loading">
        <n-spin size="small" />
        <n-text depth="3" style="margin-left: 10px">正在探测 shell…</n-text>
      </div>
      <div v-show="!preparing" ref="termHost" class="term-host" />
    </template>
      </div>
    </div>
  </n-drawer>
</template>

<style scoped>
.term-host {
  height: 60vh;
  background: #1e1e1e;
  border-radius: 4px;
  padding: 8px;
}
.term-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 60vh;
  background: #fafafa;
  border-radius: 4px;
}
</style>
