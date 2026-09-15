<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { Drawer, Input, Button, Space, Switch, Tag, Typography, Select } from 'ant-design-vue'
import { wsURL, type LogMessage } from '../api/docker'
import type { ServiceItem } from '../api/gateway'

const props = defineProps<{ service: ServiceItem }>()
const emit = defineEmits<{ close: [] }>()

const MAX_LINES = 5000
const LEVELS = ['DEBUG', 'INFO', 'WARN', 'ERROR'] as const
type Level = (typeof LEVELS)[number]
const levelRank: Record<string, number> = { DEBUG: 0, INFO: 1, WARN: 2, ERROR: 3 }
const levelOptions = LEVELS.map((l) => ({ value: l, label: l }))

const lines = ref<string[]>([])
const keyword = ref('')
const level = ref<Level>('WARN')
const pretty = ref(true)
const follow = ref(true)
const connected = ref(false)
const errorMsg = ref('')
const truncated = ref(false)
const bodyRef = ref<HTMLElement | null>(null)

let ws: WebSocket | null = null

/** 解析一行日志的等级;非 JSON / 未知等级按 INFO 处理。 */
function lineRank(line: string): number {
  try {
    const o = JSON.parse(line) as { level?: string }
    return levelRank[String(o.level || '').toUpperCase()] ?? 1
  } catch {
    return 1
  }
}

/** 等级(≥ 选中)+ 关键字过滤。 */
const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase()
  const min = levelRank[level.value]
  return lines.value.filter((l) => lineRank(l) >= min && (!k || l.toLowerCase().includes(k)))
})

function pad2(n: number): string {
  return String(n).padStart(2, '0')
}

function fmtTime(ts?: number): string {
  if (!ts) return ''
  const d = new Date(ts * 1000)
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(
    d.getMinutes(),
  )}:${pad2(d.getSeconds())}`
}

/** 格式化:时间 · 等级 · 客户端(remote_ip) · 重要信息(msg / err / 请求行)。 */
function formatLine(raw: string): string {
  let o: any
  try {
    o = JSON.parse(raw)
  } catch {
    return raw
  }
  const parts: string[] = []
  const t = fmtTime(o.ts)
  if (t) parts.push(t)
  const lv = String(o.level || '').toUpperCase()
  if (lv) parts.push(lv.padEnd(5))
  const ip = o.request?.remote_ip
  if (ip) parts.push(ip)
  if (o.msg) parts.push(String(o.msg))
  if (o.err) parts.push('err=' + o.err)
  const req = o.request
  if (req) {
    const info = [req.method, req.uri, req.proto].filter(Boolean).join(' ')
    if (info) parts.push('· ' + info)
  }
  return parts.join('  ')
}

const displayed = computed(() => (pretty.value ? filtered.value.map(formatLine) : filtered.value))

function connect() {
  // 派生服务不落库,id 仅作路由;按 ?service=<名称> 定位其 GB_LOG_FILE。
  const params: Record<string, string> = { follow: 'true', tail: '500' }
  if (props.service.source === 'docker') params.service = props.service.name
  const url = wsURL(`/gateway/services/${encodeURIComponent(props.service.id)}/logs`, params)
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
    if (msg.data !== undefined && msg.data !== '') {
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

const bodyStyle = 'display:flex;flex-direction:column;height:100%;overflow:hidden;padding:16px'

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
    :body-style="bodyStyle"
    @close="emit('close')"
  >
    <template #title>
      <span class="dw-drawer-title">访问日志 · {{ service.name }}</span>
    </template>
    <div style="display: flex; align-items: center; justify-content: space-between; gap: 10px; margin-bottom: 10px; flex-shrink: 0">
      <Space align="center" :size="8" wrap>
        <Select v-model:value="level" size="small" style="width: 92px" :options="levelOptions" />
        <Input v-model:value="keyword" placeholder="查找日志" allow-clear style="width: 200px" size="small" />
        <Button size="small" @click="pretty = !pretty">{{ pretty ? '默认格式' : '格式化' }}</Button>
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
    <div v-if="errorMsg" style="color: #ff4d4f; font-size: 12px; margin-bottom: 8px; flex-shrink: 0">{{ errorMsg }}</div>

    <div ref="bodyRef" class="log-body">
      <div v-if="!displayed.length" class="log-empty">
        <Typography.Text type="secondary">{{ lines.length ? '没有匹配的日志行' : '暂无访问日志' }}</Typography.Text>
      </div>
      <div v-for="(l, i) in displayed" :key="i" class="log-line">{{ l }}</div>
    </div>
  </Drawer>
</template>

<style scoped>
.log-body {
  flex: 1;
  min-height: 0;
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
