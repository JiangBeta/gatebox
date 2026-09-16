<script setup lang="ts">
// PluginHost：L1 插件页面宿主（iframe + postMessage 桥，ADR-039 §3）。
//
// 桥只承载宿主能力（身份/主题/导航/通知/尺寸），业务数据一律走同源 API：
//   /api/v1/plugins/<id>/*
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { message } from 'ant-design-vue'
import http from '../api/http'

const props = defineProps<{ id: string; title?: string }>()
const router = useRouter()

const iframe = ref<HTMLIFrameElement | null>(null)
const height = ref(600)
const src = computed(() => `/plugins/${props.id}/index.html`)
const origin = window.location.origin

function send(payload: Record<string, unknown>) {
  iframe.value?.contentWindow?.postMessage(payload, origin)
}

async function sendInit() {
  let token = ''
  try {
    // 仅 process 类插件有 sidecar token；其余返回 404，忽略。
    token = (await http.get(`/plugins/${props.id}/token`)).data?.token || ''
  } catch {
    token = ''
  }
  send({
    type: 'init',
    pluginId: props.id,
    token,
    theme: 'light',
    locale: 'zh-CN',
    apiBase: `/api/v1/plugins/${props.id}`,
  })
}

function onMessage(e: MessageEvent) {
  // 仅接受本 iframe 的消息，避免其他窗口伪造。
  if (!iframe.value || e.source !== iframe.value.contentWindow) return
  const d = (e.data || {}) as Record<string, any>
  switch (d.type) {
    case 'ready':
      sendInit()
      break
    case 'resize':
      if (typeof d.height === 'number') height.value = Math.max(400, Math.min(d.height, 4000))
      break
    case 'navigate':
      if (typeof d.path === 'string' && d.path.startsWith('/')) router.push(d.path)
      break
    case 'toast':
      if (d.message) message.info(String(d.message))
      break
    case 'setTitle':
      // 页面标题由路由 meta 决定；此处保留扩展位。
      break
  }
}

onMounted(() => window.addEventListener('message', onMessage))
onUnmounted(() => window.removeEventListener('message', onMessage))
</script>

<template>
  <div class="plugin-host">
    <iframe
      ref="iframe"
      class="plugin-frame"
      :src="src"
      :style="{ height: height + 'px' }"
      sandbox="allow-scripts allow-same-origin allow-forms allow-popups"
      @load="sendInit"
    />
  </div>
</template>

<style scoped>
.plugin-host {
  width: 100%;
}
.plugin-frame {
  width: 100%;
  border: 0;
  display: block;
}
</style>
