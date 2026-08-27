<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { NDrawer, NInput, NButton, NSpace, NSwitch, NTag, NAlert, NText } from 'naive-ui'
import { wsURL, type ContainerView, type LogMessage } from '../api/docker'

const props = defineProps<{ container: ContainerView }>()
const emit = defineEmits<{ close: [] }>()

interface LogLine {
  seq: number
  stream: string
  text: string
}

/** 上限保护:长时间 follow 的容器可能刷出海量日志,不设限会吃满浏览器内存 */
const MAX_LINES = 5000

const lines = ref<LogLine[]>([])
const keyword = ref('')
const follow = ref(true)
const connected = ref(false)
const errorMsg = ref('')
const truncated = ref(false)
const bodyRef = ref<HTMLElement | null>(null)

let ws: WebSocket | null = null
let seq = 0
let pending = ''

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  if (!k) return lines.value
  return lines.value.filter((l) => l.text.toLowerCase().includes(k))
})

const title = computed(() => `日志 · ${props.container.name}`)

/** 把流式片段按换行切成行:一帧可能含多行,也可能只是半行 */
function feed(stream: string, chunk: string) {
  pending += chunk
  const parts = pending.split('\n')
  pending = parts.pop() ?? ''
  for (const p of parts) {
    lines.value.push({ seq: seq++, stream, text: p })
  }
  if (lines.value.length > MAX_LINES) {
    lines.value = lines.value.slice(-MAX_LINES)
    truncated.value = true
  }
}

function connect() {
  const url = wsURL(`/docker/containers/${props.container.id}/logs`, {
    follow: 'true',
    tail: '500',
  })
  ws = new WebSocket(url)

  ws.onopen = () => {
    connected.value = true
    errorMsg.value = ''
  }
  ws.onmessage = (ev) => {
    let msg: LogMessage
    try {
      msg = JSON.parse(ev.data)
    } catch {
      return
    }
    if (msg.error) {
      errorMsg.value = msg.error
      return
    }
    if (msg.data) {
      feed(msg.stream || 'stdout', msg.data)
      if (follow.value) scrollToBottom()
    }
  }
  ws.onerror = () => {
    if (!errorMsg.value) errorMsg.value = '日志连接中断'
  }
  ws.onclose = () => {
    connected.value = false
  }
}

function scrollToBottom() {
  nextTick(() => {
    const el = bodyRef.value
    if (el) el.scrollTop = el.scrollHeight
  })
}

function download() {
  const content = lines.value.map((l) => l.text).join('\n')
  const blob = new Blob([content], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `${props.container.name}-logs.txt`
  a.click()
  URL.revokeObjectURL(a.href)
}

function clear() {
  lines.value = []
  truncated.value = false
}

watch(follow, (v) => {
  if (v) scrollToBottom()
})

onMounted(connect)
onUnmounted(() => {
  ws?.close()
  ws = null
})
</script>

<template>
  <n-drawer
    :show="true"
    placement="right"
    width="min(800px, 100vw)"
    @update:show="(v: boolean) => !v && emit('close')"
  >
    <template #header>
      <div style="display: flex; align-items: center; justify-content: space-between; gap: 8px">
        <span style="font-size: 15px; font-weight: 600">{{ title }}</span>
        <div style="display: flex; align-items: center; gap: 8px">
          <n-tag :type="connected ? 'success' : 'default'" size="small" :bordered="false">
            {{ connected ? '已连接' : '已断开' }}
          </n-tag>
          <n-button quaternary circle size="small" @click="emit('close')">✕</n-button>
        </div>
      </div>
    </template>

    <n-alert v-if="errorMsg" type="warning" style="margin-bottom: 10px" :show-icon="true">
      {{ errorMsg }}
    </n-alert>

    <n-space align="center" style="margin-bottom: 10px">
      <n-input
        v-model:value="keyword"
        placeholder="查找日志内容"
        clearable
        style="width: 280px"
        size="small"
      />
      <span style="font-size: 13px; color: #666">
        {{ filtered.length }} / {{ lines.length }} 行
        <span v-if="truncated">（已截断至最近 {{ MAX_LINES }} 行）</span>
      </span>
      <n-space align="center" :size="6">
        <span style="font-size: 13px; color: #666">自动滚动</span>
        <n-switch v-model:value="follow" size="small" />
      </n-space>
      <n-button size="small" @click="clear">清空</n-button>
      <n-button size="small" @click="download">下载</n-button>
    </n-space>

    <div ref="bodyRef" class="log-body">
      <div v-if="!filtered.length" class="log-empty">
        <n-text depth="3">
          {{ lines.length ? '没有匹配的日志行' : '暂无日志输出' }}
        </n-text>
      </div>
      <div
        v-for="l in filtered"
        :key="l.seq"
        class="log-line"
        :class="{ stderr: l.stream === 'stderr' }"
      >{{ l.text }}</div>
    </div>
  </n-drawer>
</template>

<style scoped>
.log-body {
  height: 60vh;
  overflow: auto;
  background: #1e1e1e;
  color: #d4d4d4;
  border-radius: 4px;
  padding: 10px 12px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12.5px;
  line-height: 1.55;
}
.log-line {
  white-space: pre-wrap;
  word-break: break-all;
}
.log-line.stderr {
  color: #f48771;
}
.log-empty {
  display: flex;
  justify-content: center;
  padding: 40px 0;
}
</style>
