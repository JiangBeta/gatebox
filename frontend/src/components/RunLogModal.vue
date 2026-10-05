<script setup lang="ts">
/**
 * 日志详情弹层：地图视图 / 文本视图（原型 logModal）。
 *
 * 两个视图，一个数据源：
 *   - 地图视图：触发卡 → 调用线 → 步骤卡。步骤来自 **序列模板对账**
 *     （model/log.yaml `sequences`，按 marker 扫原始日志），所以每一步都有
 *     实际时间/耗时，而不是把日志按行数平均切。
 *   - 文本视图：插件原始输出，按日志格式高亮（时间戳 / 级别 / 关键字 / PEM 边界）。
 *
 * 没有原始输出时（evidence 为空）：地图视图只给触发卡 + 明确说明，
 * 文本视图显示「未采集」。绝不编步骤——假步骤比没步骤危险。
 */
import { computed, ref, watch } from 'vue'
import { Modal, Button, Tooltip, Empty } from 'ant-design-vue'
import {
  CheckCircleOutlined, CloseCircleOutlined, MinusCircleOutlined,
  LoadingOutlined, CopyOutlined, PlusOutlined,
} from '@ant-design/icons-vue'
import { getKindSpec, type Sequence } from '@/api/objects'
import { parseLog, pickSequence } from '../lib/schema/logParse'

const props = defineProps<{
  open: boolean
  /** 链/事务标识：地图视图边上的连线标签 + 文本视图头部。 */
  chain?: string
  /** 触发方式：boot | manual | schedule（log.yaml trigger 枚举）。 */
  trigger?: string
  /** 动作：ensure | renew | delete，用于挑序列模板。 */
  action?: string
  state?: string
  startedAt?: string
  endedAt?: string
  /** 插件原始输出（acme.sh 日志全文）。 */
  evidence?: string
  title?: string
}>()

const emit = defineEmits<{ close: [] }>()

const view = ref<'map' | 'text'>('map')
const sequences = ref<Sequence[] | undefined>()
const zoom = ref(1)
const pan = ref({ x: 12, y: 12 })

const TRIGGER_LABEL: Record<string, string> = {
  boot: '第一次启动',
  manual: '手工触发',
  schedule: '自动更新',
}
const TRIGGER_ROLE: Record<string, string> = {
  boot: '系统',
  manual: '用户',
  schedule: '定时器',
}

/** 序列模板是全局的（log.yaml），取一次就够；打开弹层时确保已加载。 */
watch(
  () => props.open,
  async (v) => {
    if (!v || sequences.value) return
    try {
      const spec = await getKindSpec('log')
      sequences.value = spec.sequences
    } catch {
      /* 拿不到序列模板就退化成「只有触发卡」，不报错阻断查看 */
      sequences.value = undefined
    }
  },
  { immediate: true },
)

const seq = computed(() => pickSequence(sequences.value, props.action))
const steps = computed(() => parseLog(props.evidence || '', seq.value?.steps))

/**
 * triggerText 触发来源文案。
 *
 * 记录里没写触发来源（比如证书日志只存了 action）就显示「—」，
 * 再退到 action —— 但绝不猜「手工触发」：日志没记的东西不能编。
 */
const triggerText = computed(() => TRIGGER_LABEL[props.trigger || ''] || props.trigger || '')

const stateText = computed(() => {
  const s = props.state || ''
  return s === 'success' ? '成功' : s === 'failed' || s === 'fail' ? '失败' : s === 'running' ? '进行中' : s || '未知'
})

const stateColor = computed(() => {
  const s = props.state || ''
  if (s === 'success') return '#52c41a'
  if (s === 'failed' || s === 'fail') return '#ff4d4f'
  if (s === 'running') return '#1677ff'
  return '#8b95a7'
})

/** 总耗时：优先用起止时间差（真实），没有就用步骤累加。 */
const totalMs = computed(() => {
  if (props.startedAt && props.endedAt) {
    const d = new Date(props.endedAt).getTime() - new Date(props.startedAt).getTime()
    if (!isNaN(d) && d >= 0) return d
  }
  return steps.value.reduce((a, s) => a + s.ms, 0)
})

function fmtMs(ms: number): string {
  if (!ms) return '0ms'
  return ms >= 1000 ? `${(ms / 1000).toFixed(1)}s` : `${ms}ms`
}

function fmtTime(s?: string): string {
  if (!s) return '—'
  const d = new Date(s)
  if (isNaN(d.getTime())) return s
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function stepIcon(state: string) {
  if (state === 'success') return CheckCircleOutlined
  if (state === 'failed') return CloseCircleOutlined
  if (state === 'running') return LoadingOutlined
  return MinusCircleOutlined
}

function stepColor(state: string): string {
  if (state === 'success') return '#52c41a'
  if (state === 'failed') return '#ff4d4f'
  if (state === 'running') return '#1677ff'
  return '#8b95a7'
}

const lines = computed(() => (props.evidence || '').split('\n'))

/** esc 转义（文本视图靠 v-html，必须自己转义，否则日志里的 <b> 会变成真标签）。 */
function esc(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

/** logHL 按日志格式高亮：PEM 边界 / 行首时间戳 / 级别关键字。 */
function logHL(line: string): string {
  const raw = esc(line)
  if (/^-----(BEGIN|END)/.test(line)) return `<span class="lv lv-cert">${raw}</span>`
  let h = raw.replace(/^(\[[^\]]+\])/, '<span class="lt">$1</span>')
  h = h.replace(
    /(successful|successfully|Success|success|Cert success)/g,
    '<span class="lv lv-info">$1</span>',
  )
  h = h.replace(/\b(error|Error|failed|Failed|fail)\b/g, '<span class="lv lv-error">$1</span>')
  h = h.replace(/\b(Pending|Sleeping|wait)\b/g, '<span class="lv lv-warn">$1</span>')
  return h
}

function zoomBy(f: number) {
  zoom.value = Math.max(0.4, Math.min(2.5, zoom.value * f))
}

function fit() {
  zoom.value = 1
  pan.value = { x: 12, y: 12 }
}

/**
 * 地图拖拽平移（原型是自由拖动的画布）。
 *
 * 用 pointer 事件而不是 mouse：触屏/触控板也能拖。
 * 拖动时把 scale 换算掉（屏幕位移 / zoom 才是画布位移），
 * 否则 120% 时画布会跟着鼠标跑偏。
 */
const dragging = ref(false)
let dragFrom = { x: 0, y: 0, px: 0, py: 0 }

function onPanDown(e: PointerEvent) {
  dragging.value = true
  dragFrom = { x: e.clientX, y: e.clientY, px: pan.value.x, py: pan.value.y }
  ;(e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId)
}

function onPanMove(e: PointerEvent) {
  if (!dragging.value) return
  pan.value = {
    x: dragFrom.px + (e.clientX - dragFrom.x) / zoom.value,
    y: dragFrom.py + (e.clientY - dragFrom.y) / zoom.value,
  }
}

function onPanUp(e: PointerEvent) {
  dragging.value = false
  ;(e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId)
}

async function copyLog() {
  try {
    await navigator.clipboard.writeText(props.evidence || '')
  } catch {
    /* 非安全上下文（http 局域网）下 clipboard 不可用，忽略 */
  }
}
</script>

<template>
  <Modal
    :open="open"
    :title="title || `日志 · ${chain || ''}`"
    :width="900"
    :footer="null"
    @cancel="emit('close')"
  >
    <div class="lm">
      <div class="lm-tabs">
        <Button size="small" :type="view === 'map' ? 'primary' : 'default'" @click="view = 'map'">
          地图视图
        </Button>
        <Button size="small" :type="view === 'text' ? 'primary' : 'default'" @click="view = 'text'">
          文本视图
        </Button>
        <span class="lm-sp" />
        <span class="lm-meta">
          {{ triggerText || action || '—' }} · 耗时 {{ fmtMs(totalMs) }} ·
          <b :style="{ color: stateColor }">{{ stateText }}</b>
        </span>
      </div>

      <!-- 地图视图 -->
      <div v-if="view === 'map'" class="lm-canvas">
        <div class="lm-stats">
          <span class="lm-tag">1 个节点</span>
          <span class="lm-tag">序列 {{ steps.length }} 步</span>
          <span class="lm-tag" :style="{ color: stateColor }">{{ stateText }}</span>
          <span class="lm-tag">{{ fmtMs(totalMs) }}</span>
        </div>

        <div
          class="lm-world"
          :class="{ 'lm-dragging': dragging }"
          :style="{ transform: `translate(${pan.x}px, ${pan.y}px) scale(${zoom})` }"
          @pointerdown="onPanDown"
          @pointermove="onPanMove"
          @pointerup="onPanUp"
          @pointercancel="onPanUp"
        >
          <svg class="lm-edges" width="1200" height="240" viewBox="0 0 1200 240">
            <defs>
              <marker id="lm-arrow" markerWidth="10" markerHeight="10" refX="8" refY="3.2" orient="auto">
                <path d="M0,0 L6.5,3.2 L0,6.4 z" fill="#5b6470" />
              </marker>
            </defs>
            <path d="M320 62 H436" stroke="#5b6470" stroke-width="1.4" fill="none" marker-end="url(#lm-arrow)" />
          </svg>

          <div class="lm-node lm-trigger">
            <div class="lm-nh">
              <span class="lm-dot" style="background: #e7ac2a" />
              触发任务
              <span class="lm-sp" />
              <span class="lm-tag">入口</span>
            </div>
            <div class="lm-kv"><span>动作</span><b>{{ action || '—' }}</b></div>
            <div class="lm-kv"><span>角色</span><b>{{ TRIGGER_ROLE[trigger || ''] || '—' }}</b></div>
            <div class="lm-kv"><span>时间</span><b>{{ fmtTime(startedAt) }}</b></div>
          </div>

          <div class="lm-edge-label">{{ seq?.id || 'sequence' }}</div>

          <div class="lm-node lm-steps">
            <div class="lm-nh">
              <span class="lm-dot" style="background: #1677ff" />
              {{ chain || '任务' }}
              <span class="lm-sp" />
              <span class="lm-tag">
                {{ seq ? `序列 ${steps.length} 条` : '无序列模板' }} ·
                <span :style="{ color: stateColor }">{{ stateText }}</span> · {{ fmtMs(totalMs) }}
              </span>
            </div>
            <div v-if="!evidence" class="lm-note">未采集到原始输出，无法对账步骤。</div>
            <div v-else-if="!seq" class="lm-note">未知序列模板，按日志格式展示原文。</div>
            <div v-else class="lm-steps-list">
              <div v-for="s in steps" :key="s.no" class="lm-step">
                <span class="lm-sn">#{{ s.no }}</span>
                <span class="lm-ss">
                  <Tooltip :title="s.marker">
                    <span class="lm-tip">{{ s.step }}</span>
                  </Tooltip>
                </span>
                <span class="lm-sm">{{ s.at }}</span>
                <component :is="stepIcon(s.state)" class="lm-st" :style="{ color: stepColor(s.state) }" />
                <span class="lm-sd">{{ fmtMs(s.ms) }}</span>
                <span v-if="s.state === 'skipped'" class="lm-skip">跳过</span>
              </div>
            </div>
          </div>
        </div>

        <div class="lm-legend">
          <div class="lm-row"><span class="lm-dot" style="background: #e7ac2a" />触发</div>
          <div class="lm-row"><span class="lm-dot" style="background: #1677ff" />sequence</div>
          <div class="lm-row"><span class="lm-line" />调用</div>
        </div>

        <div class="lm-ctrl">
          <Button size="small" @click="zoomBy(1.2)"><PlusOutlined /></Button>
          <Button size="small" @click="zoomBy(0.8)">－</Button>
          <Button size="small" @click="fit">适配</Button>
          <span class="lm-z">{{ Math.round(zoom * 100) }}%</span>
        </div>
      </div>

      <!-- 文本视图 -->
      <div v-else class="lm-text">
        <div class="lm-text-head">
          {{ chain || '—' }} · {{ triggerText || action || '—' }} · 耗时
          {{ fmtMs(totalMs) }} ·
          <b :style="{ color: stateColor }">{{ stateText }}</b>
          <Button size="small" @click="copyLog" :disabled="!evidence">
            <template #icon><CopyOutlined /></template>
            复制
          </Button>
        </div>
        <Empty v-if="!evidence" description="未采集到原始输出" style="padding: 32px 0" />
        <div v-else class="lm-code">
          <div class="lm-gutter">
            <div v-for="(_, i) in lines" :key="i">{{ i + 1 }}</div>
          </div>
          <pre class="lm-pre"><span
            v-for="(l, i) in lines"
            :key="i"
            class="lm-line"
            v-html="logHL(l)"
          ></span></pre>
        </div>
      </div>
    </div>
  </Modal>
</template>

<style scoped>
.lm-tabs {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 10px;
}
.lm-sp {
  flex: 1;
}
.lm-meta {
  font-size: 12px;
  color: #8b95a7;
}

.lm-canvas {
  position: relative;
  height: 420px;
  border: 1px solid #e5e8ec;
  border-radius: 8px;
  background: #fbfcfd;
  overflow: auto;
}
.lm-stats {
  display: flex;
  gap: 6px;
  padding: 10px 12px;
  border-bottom: 1px solid #eef0f3;
  background: #fff;
  position: sticky;
  top: 0;
  z-index: 2;
}
.lm-tag {
  padding: 1px 8px;
  border-radius: 10px;
  background: #f0f2f5;
  font-size: 12px;
  color: #5b6470;
}

.lm-world {
  position: relative;
  width: 1020px;
  transform-origin: 0 0;
  padding: 16px;
  cursor: grab;
  touch-action: none;
}
.lm-world.lm-dragging {
  cursor: grabbing;
}
.lm-edges {
  position: absolute;
  left: 0;
  top: 16px;
  pointer-events: none;
}
.lm-node {
  position: absolute;
  top: 16px;
  border: 1px solid #e5e8ec;
  border-radius: 8px;
  background: #fff;
  padding: 10px 12px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.04);
}
.lm-trigger {
  left: 16px;
  width: 320px;
}
.lm-steps {
  left: 456px;
  width: 548px;
}
.lm-nh {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 13px;
  font-weight: 600;
  margin-bottom: 8px;
}
.lm-dot {
  display: inline-block;
  width: 8px;
  height: 8px;
  border-radius: 50%;
}
.lm-kv {
  display: flex;
  justify-content: space-between;
  font-size: 12px;
  color: #5b6470;
  padding: 2px 0;
}
.lm-edge-label {
  position: absolute;
  left: 394px;
  top: 62px;
  font-size: 12px;
  color: #8b95a7;
}
.lm-note {
  font-size: 12px;
  color: #8b95a7;
  padding: 12px 4px;
}
.lm-steps-list {
  max-height: 260px;
  overflow: auto;
}
.lm-step {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 3px 0;
  border-bottom: 1px dashed #eef0f3;
  font-size: 12px;
}
.lm-sn {
  color: #8b95a7;
  width: 26px;
  flex: 0 0 auto;
}
.lm-ss {
  flex: 1;
  min-width: 0;
}
.lm-tip {
  border-bottom: 1px dotted #8b95a7;
  cursor: default;
}
.lm-sm {
  color: #5b6470;
  width: 62px;
  flex: 0 0 auto;
  font-family: monospace;
}
.lm-sd {
  color: #5b6470;
  width: 52px;
  flex: 0 0 auto;
  font-family: monospace;
}
.lm-skip {
  color: #8b95a7;
  flex: 0 0 auto;
}

.lm-legend {
  position: absolute;
  left: 12px;
  bottom: 10px;
  display: flex;
  gap: 14px;
  font-size: 12px;
  color: #5b6470;
  background: rgba(255, 255, 255, 0.9);
  padding: 4px 8px;
  border-radius: 6px;
}
.lm-row {
  display: flex;
  align-items: center;
  gap: 5px;
}
.lm-line {
  display: inline-block;
  width: 18px;
  height: 1px;
  background: #5b6470;
}
.lm-ctrl {
  position: absolute;
  right: 12px;
  bottom: 10px;
  display: flex;
  align-items: center;
  gap: 6px;
  background: #fff;
  border: 1px solid #e5e8ec;
  border-radius: 6px;
  padding: 4px 8px;
}
.lm-z {
  font-size: 12px;
  color: #8b95a7;
}

.lm-text-head {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  color: #5b6470;
  padding: 6px 8px;
  border: 1px solid #e5e8ec;
  border-radius: 6px 6px 0 0;
  background: #fbfcfd;
}
.lm-code {
  display: flex;
  max-height: 380px;
  overflow: auto;
  border: 1px solid #e5e8ec;
  border-top: none;
  border-radius: 0 0 6px 6px;
  background: #1e1e1e;
}
.lm-gutter {
  flex: 0 0 auto;
  padding: 8px 6px 8px 10px;
  text-align: right;
  color: #5b6470;
  font-family: monospace;
  font-size: 11px;
  line-height: 1.6;
  user-select: none;
  background: #171717;
}
.lm-pre {
  margin: 0;
  padding: 8px 10px;
  font-family: monospace;
  font-size: 11px;
  line-height: 1.6;
  color: #d4d4d4;
  white-space: pre-wrap;
  word-break: break-all;
}
.lm-line {
  display: block;
}
.lt {
  color: #808080;
}
.lv-info {
  color: #4ec9b0;
}
.lv-error {
  color: #f48771;
}
.lv-warn {
  color: #dcdcaa;
}
.lv-cert {
  color: #569cd6;
}
</style>