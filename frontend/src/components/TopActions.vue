<script setup lang="ts">
/**
 * 顶栏动作组：任务 / 升级 / 重启（ADR-043 §6）。
 *
 * 三键的边界（都是刻意为之，不是没实现完）：
 *   - 任务：读真实 Run（/api/v1/runs + SSE），角标 = 执行中数量。
 *   - 升级：本轮不开放自升级通道，面板只说明状态。版本策略未定，
 *     没有可信版本源就不显示「有新版本」——假角标比没有角标更坏。
 *   - 重启：只提示手动重启。进程一退出 HTTP 响应就断，用户看不到结果，
 *     所以不做「点一下假装重启成功」，而是给出准确的操作方式。
 */
import { computed, onMounted, ref } from 'vue'
import { Button, Modal, Popover, Tooltip } from 'ant-design-vue'
import {
  SyncOutlined, ClockCircleOutlined, CloudUploadOutlined, RedoOutlined,
  CheckCircleOutlined, CloseCircleOutlined, ExclamationCircleOutlined,
} from '@ant-design/icons-vue'
import { useRouter } from 'vue-router'
import { getSystem, type SystemInfo } from '../api/system'
import { runDetail, runProgress, runTitle, runningRuns, useRuns } from '../app/composables/useRuns'
import type { Run } from '../shared/observe'

const router = useRouter()
const { runs } = useRuns()
const sys = ref<SystemInfo | null>(null)
onMounted(async () => {
  try {
    sys.value = await getSystem()
  } catch {
    /* 探测失败不阻塞顶栏：面板里显示「—」即可 */
  }
})

const running = computed(() => runningRuns(runs.value))
const history = computed(() => runs.value.filter((r) => r.state !== 'running'))

const taskOpen = ref(false)
const restartOpen = ref(false)

const taskTip = computed(() =>
  running.value.length ? `任务 · ${running.value.length} 个执行中` : '任务 · 空闲',
)

function fmtDuration(ms: number): string {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  return m ? `${m} 分 ${s % 60} 秒` : ''
}

function elapsed(run: Run): string {
  return fmtDuration((run.endedAt || Date.now()) - run.startedAt)
}

function fmtTime(ms?: number): string {
  if (!ms) return '—'
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`
}

function openTaskPage() {
  taskOpen.value = false
  router.push('/tasks')
}

const restartBody = computed(() => {
  const containers = sys.value?.containers
  return {
    version: sys.value?.version || '—',
    runningTasks: running.value.length,
    containers: containers === null ? 'Docker 不可达' : `${containers.total} 个`,
    estimate: sys.value?.restart?.estimate || '约 3 秒',
  }
})
</script>

<template>
  <div class="ta">
    <!-- 任务 -->
    <Popover
      v-model:open="taskOpen"
      trigger="click"
      placement="bottomRight"
      :overlay-style="{ width: '380px' }"
    >
      <template #content>
        <div class="ta-panel">
          <div class="ta-panel-head">
            <span class="ta-panel-title">任务</span>
            <Button type="link" size="small" @click="openTaskPage">全部任务</Button>
          </div>
          <template v-if="running.length">
            <div class="ta-sec">执行中</div>
            <div v-for="r in running" :key="r.id" class="ta-row running">
              <div class="ta-row-head">
                <SyncOutlined class="ta-row-icon" :spin="true" />
                <b class="ta-row-title" :title="runTitle(r)">{{ runTitle(r) }}</b>
                <span class="ta-row-sp" />
                <span class="ta-row-time">{{ elapsed(r) }}</span>
              </div>
              <div class="ta-bar"><i :style="{ width: `${runProgress(r)}%` }" /></div>
              <div v-if="runDetail(r)" class="ta-row-detail">{{ runDetail(r) }}</div>
            </div>
          </template>
          <p v-else class="ta-empty-hint">当前没有执行中的任务</p>

          <div class="ta-sec">最近完成</div>
          <div v-for="r in history.slice(0, 8)" :key="r.id" class="ta-row">
            <div class="ta-row-head">
              <CheckCircleOutlined v-if="r.state !== 'error'" class="ta-row-icon ok" />
              <CloseCircleOutlined v-else class="ta-row-icon err" />
              <b class="ta-row-title" :title="runTitle(r)">{{ runTitle(r) }}</b>
              <span class="ta-row-sp" />
              <span class="ta-row-time">{{ fmtTime(r.startedAt) }}</span>
            </div>
          </div>
          <p v-if="!history.length" class="ta-empty-hint">暂无历史任务</p>
        </div>
      </template>
      <Tooltip :title="taskTip" placement="bottom">
        <button type="button" class="ta-btn" :class="{ on: running.length > 0 }" aria-label="任务">
          <!-- 原型是 clock（静态时钟），这里用时钟图标：spinning 只给执行中的行内图标，
               顶栏按钮常驻转圈会让人以为页面卡了。 -->
          <ClockCircleOutlined />
          <span v-if="running.length" class="ta-badge">{{ running.length }}</span>
        </button>
      </Tooltip>
    </Popover>

    <!-- 升级：ADR-043 §6 决定「按钮保留但置灰 + 即将推出」。
         不做可点面板：没有可信版本源的面板只会给人「已经能升级」的错觉。 -->
    <Tooltip title="升级 · 即将推出（本版不提供自升级通道）" placement="bottom">
      <span class="ta-wrap">
        <button type="button" class="ta-btn off" aria-label="升级（即将推出）" disabled>
          <CloudUploadOutlined />
        </button>
      </span>
    </Tooltip>

    <!-- 重启（原型 .top 的 btnRestart = refresh 图标） -->
    <Tooltip title="重启 GateBox 服务" placement="bottom">
      <button type="button" class="ta-btn" aria-label="重启 GateBox 服务" @click="restartOpen = true">
        <RedoOutlined />
      </button>
    </Tooltip>

    <Modal
      v-model:open="restartOpen"
      title="重启 GateBox 服务"
      :width="460"
      ok-text="知道了"
      cancel-text="取消"
      :footer="null"
    >
      <div class="ta-restart">
        <div>
          当前版本 <b>v{{ restartBody.version }}</b>，运行中
          <b>{{ restartBody.runningTasks }}</b> 个任务、<b>{{ restartBody.containers }}</b>。
        </div>
        <div class="ta-restart-warn">
          <ExclamationCircleOutlined /> 重启期间控制面不可访问（{{ restartBody.estimate }}）。
          新版为优雅停止：先停插件 sidecar 再退出；容器由 Agent 托管，会继续运行。
        </div>
        <div class="ta-restart-cmd">
          <div>控制面不支持自重启（进程退出即响应中断），请手动执行：</div>
          <code>kill -TERM &lt;pid&gt; &amp;&amp; ./gatebox</code>
        </div>
      </div>
    </Modal>
  </div>
</template>

<style scoped>
.ta {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-right: 8px;
}

.ta-btn {
  position: relative;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 30px;
  border: 1px solid var(--gb-color-border);
  border-radius: 6px;
  background: var(--gb-color-bg-container);
  color: var(--gb-color-text-secondary);
  font-size: 15px;
  cursor: pointer;
}
.ta-btn:hover,
.ta-btn.on {
  border-color: var(--gb-color-primary);
  color: var(--gb-color-primary);
}

/* 置灰态（升级：ADR-043 §6）。hover 不变主色——置灰的元素不该有"可用"暗示。 */
.ta-btn.off,
.ta-btn.off:hover {
  border-color: var(--gb-color-border-secondary);
  background: var(--gb-color-bg-soft);
  color: var(--gb-color-text-quaternary);
  cursor: not-allowed;
}
/* disabled 的 button 不派发鼠标事件，Tooltip 必须挂到外层 span 才弹得出来。 */
.ta-wrap {
  display: inline-flex;
}

.ta-badge {
  position: absolute;
  top: -6px;
  right: -6px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  border-radius: 9px;
  background: var(--gb-color-success);
  color: #fff;
  font-size: 10px;
  font-weight: 700;
  line-height: 16px;
  text-align: center;
  box-shadow: 0 0 0 2px var(--gb-color-bg-container);
  pointer-events: none;
}

.ta-panel-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 6px;
  border-bottom: 1px solid var(--gb-color-border-secondary);
}
.ta-panel-title {
  font-size: 14px;
  font-weight: 600;
}
.ta-sec {
  padding: 10px 2px 6px;
  font-size: 12px;
  color: var(--gb-color-text-tertiary);
}
.ta-empty-hint {
  margin: 4px 0 0;
  padding: 8px 2px;
  font-size: 12px;
  color: var(--gb-color-text-tertiary);
}

.ta-row {
  border: 1px solid var(--gb-color-border-secondary);
  border-radius: 8px;
  padding: 10px 12px;
  margin-bottom: 8px;
  background: var(--gb-color-bg-soft);
}
.ta-row.running {
  background: var(--gb-color-primary-bg);
}
.ta-row-head {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 13px;
}
.ta-row-sp {
  flex: 1;
}
.ta-row-icon {
  color: var(--gb-color-primary);
  flex: 0 0 auto;
}
.ta-row-icon.ok {
  color: var(--gb-color-success);
}
.ta-row-icon.err {
  color: var(--gb-color-error);
}
.ta-row-title {
  font-weight: 500;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.ta-row-time {
  flex: 0 0 auto;
  font-size: 12px;
  color: var(--gb-color-text-tertiary);
}
.ta-bar {
  height: 4px;
  margin: 8px 0 6px;
  border-radius: 2px;
  background: var(--gb-color-border-secondary);
  overflow: hidden;
}
.ta-bar i {
  display: block;
  height: 100%;
  border-radius: 2px;
  background: var(--gb-color-primary);
  transition: width 0.4s ease;
}
.ta-row-detail {
  font-size: 12px;
  color: var(--gb-color-text-secondary);
  word-break: break-all;
}

.ta-line {
  margin: 6px 0;
  font-size: 13px;
  color: var(--gb-color-text);
}
.ta-sub {
  color: var(--gb-color-text-secondary);
}

.ta-restart {
  font-size: 13px;
  line-height: 1.8;
}
.ta-restart-warn {
  margin-top: 10px;
  color: var(--gb-color-warning);
}
.ta-restart-cmd {
  margin-top: 12px;
  font-size: 12px;
  color: var(--gb-color-text-secondary);
}
.ta-restart-cmd code {
  display: block;
  margin-top: 6px;
  padding: 8px;
  border-radius: 6px;
  background: var(--gb-color-bg-soft);
  font-family: monospace;
  word-break: break-all;
}
</style>