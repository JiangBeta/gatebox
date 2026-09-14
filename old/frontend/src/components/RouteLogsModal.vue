<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { Drawer, Input, Button, Space, Switch, Tag, Typography } from 'ant-design-vue'
import { wsURL, type LogMessage } from '../api/docker'
import type { ServiceItem } from '../api/gateway'

const props = defineProps<{ service: ServiceItem }>()
const emit = defineEmits<{ close: [] }>()

const MAX_LINES = 5000

const lines = ref<string[]>([])
const keyword = ref('')
const follow = ref(true)
const connected = ref(false)
const errorMsg = ref('')
const truncated = ref(false)
const bodyRef = ref<HTMLElement | null>(null)

let ws: WebSocket | null = null

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  if (!k) return lines.value
  return lines.value.filter((l) => l.toLowerCase().includes(k))
})

function connect() {
  const url = wsURL(`/gateway/services/${props.service.id}/logs`, { follow: 'true', tail: '500' })
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
      lines.value.push(msg.data)
      if (lines.value.length > MAX_LINES) {
        lines.value = lines.value.slice(-MAX_LINES)
        truncated.value = true
      }
      if (follow.value) scrollToBottom()
    }
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
  const blob = new Blob([lines.value.join('\n')], { type: 'text/plain;charset=utf-8' })
  const a = document.createElement('a')
  a.href = URL.createObjectURL(blob)
  a.download = `${props.service.name}-access.log`
  a.click()
  URL.revokeObjectURL(a.href)
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
  <Drawer
    :open="true"
    placement="right"
    :width="800"
    @close="emit('close')"
  >
    <template #title>
      <span class="dw-drawer-title">访问日志 · {{ service.name }}</span>
    </template>
    <div style="display: flex; align-items: center; justify-content: space-between; margin-bottom: 10px">
      <Space align="center">
        <Input v-model:value="keyword" placeholder="查找日志" allow-clear style="width: 280px" size="small" />
        <span style="font-size: 13px; color: #666">
          {{ filtered.length }} / {{ lines.length }} 行
          <span v-if="truncated">（已截断至最近 {{ MAX_LINES }} 行）</span>
        </span>
        <Space align="center" :size="6">
          <span style="font-size: 13px; color: #666">自动滚动</span>
          <Switch v-model:checked="follow" size="small" />
        </Space>
        <Button size="small" @click="download">下载</Button>
      </Space>
      <Tag :color="connected ? 'success' : 'default'" size="small">
        {{ connected ? '已连接' : '已断开' }}
      </Tag>
    </div>

    <div ref="bodyRef" class="log-body">
      <div v-if="!filtered.length" class="log-empty">
        <Typography.Text type="secondary">{{ lines.length ? '没有匹配的日志行' : '暂无访问日志' }}</Typography.Text>
      </div>
      <div v-for="(l, i) in filtered" :key="i" class="log-line">{{ l }}</div>
    </div>
  </Drawer>
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
.log-empty {
  display: flex;
  justify-content: center;
  padding: 40px 0;
}
</style>
