<script setup lang="ts">
import { computed, h, onMounted, ref, watch } from 'vue'
import { Badge, Button, Empty, Spin, Table, Tag, Timeline, message } from 'ant-design-vue'
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import { getRun, listRuns } from '@/api/runs'
import { useEvents } from '@/app/composables/useEvents'
import type { Run, RunStep } from '@/shared/observe'

/**
 * 任务中心（ADR-043 §6）：调和运行记录。
 *
 * 只读页：列表来自 GET /runs，点开看每步 detail/events；实时性靠全局
 * useEvents 的单条 SSE——有新 run 事件就重拉列表，不额外开 EventSource。
 */
const [messageApi, contextHolder] = message.useMessage()

const runs = ref<Run[]>([])
const loading = ref(false)
const selected = ref<Run | null>(null)
const detailLoading = ref(false)

const { activeRun } = useEvents()

const STATE_META: Record<string, { label: string; color: string }> = {
  running: { label: '进行中', color: 'processing' },
  success: { label: '成功', color: 'success' },
  degraded: { label: '降级', color: 'warning' },
  error: { label: '失败', color: 'error' },
}

function stateMeta(state: string) {
  return STATE_META[state] ?? { label: state || '未知', color: 'default' }
}

function fmtTime(ms?: number): string {
  if (!ms) return '—'
  const d = new Date(ms)
  const p = (n: number) => String(n).padStart(2, '0')
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

function fmtCost(run: Run): string {
  if (!run.endedAt) return '进行中'
  const ms = run.endedAt - run.startedAt
  return ms < 1000 ? `${ms}ms` : `${(ms / 1000).toFixed(1)}s`
}

/** 失败/降级的步数：列表上给个一眼可见的红点，不用点进去。 */
function badSteps(run: Run): number {
  return run.steps.filter((s) => s.state === 'error' || s.state === 'degraded').length
}

async function reload() {
  loading.value = true
  try {
    runs.value = await listRuns(30)
    // 选中的那条如果在列表里，顺带刷新成最新状态。
    if (selected.value) {
      const hit = runs.value.find((r) => r.id === selected.value?.id)
      if (hit) selected.value = { ...hit, steps: selected.value.steps }
    }
  } catch (e) {
    messageApi.error(e instanceof Error ? e.message : String(e))
  } finally {
    loading.value = false
  }
}

async function open(run: Run) {
  selected.value = run
  detailLoading.value = true
  try {
    // 列表里的 steps 是精简版，详情才带完整 events。
    selected.value = await getRun(run.id)
  } catch (e) {
    messageApi.error(e instanceof Error ? e.message : String(e))
  } finally {
    detailLoading.value = false
  }
}

const columns: ColumnType<Run>[] = [
  {
    title: '开始时间',
    dataIndex: 'startedAt',
    key: 'startedAt',
    width: 150,
    customRender: ({ record }) => fmtTime(record.startedAt),
  },
  { title: '触发', dataIndex: 'trigger', key: 'trigger', width: 100 },
  {
    title: '意图',
    dataIndex: 'intent',
    key: 'intent',
    ellipsis: true,
    customRender: ({ record }) => record.intent || '—',
  },
  {
    title: '状态',
    key: 'state',
    width: 110,
    customRender: ({ record }) => {
      const m = stateMeta(record.state)
      return h(Tag, { color: m.color }, () => m.label)
    },
  },
  {
    title: '异常步',
    key: 'bad',
    width: 80,
    customRender: ({ record }) => {
      const n = badSteps(record)
      return n === 0 ? '—' : h(Tag, { color: 'error' }, () => String(n))
    },
  },
  {
    title: '耗时',
    key: 'cost',
    width: 90,
    customRender: ({ record }) => fmtCost(record),
  },
  {
    title: '',
    key: 'ops',
    width: 70,
    customRender: ({ record }) =>
      h('button', { class: 'row-op', type: 'button', onClick: () => open(record) }, '详情'),
  },
]

const stepColor: Record<string, string> = {
  success: 'green',
  error: 'red',
  degraded: 'orange',
  running: 'blue',
  pending: 'gray',
  skipped: 'gray',
}

const stepItems = computed(() =>
  (selected.value?.steps ?? []).map((s: RunStep) => ({
    color: stepColor[s.state] ?? 'gray',
    children: h(
      'div',
      { class: 'run-step' },
      [
        h('div', { class: 'run-step-head' }, [
          h('span', { class: 'run-step-comp' }, `${s.component} · ${s.action}`),
          h(Tag, { color: stepColor[s.state] ?? 'default' }, () => s.state),
          h('span', { class: 'run-step-time' }, fmtCost({ startedAt: s.startedAt, endedAt: s.endedAt } as Run)),
        ]),
        s.detail ? h('pre', { class: 'run-step-detail' }, s.detail) : null,
        ...(s.events ?? []).map((e) =>
          h('pre', { class: 'run-step-event' }, String(e)),
        ),
      ],
    ),
  })),
)

/** 有 run 在跑，头顶给个提示：有新 run 时列表自动已刷新。 */
const runningCount = computed(() => runs.value.filter((r) => r.state === 'running').length)

onMounted(reload)
// 新 run 结束 / 开始 → 重拉列表（单条 SSE 已在 useEvents 里建好）。
watch(activeRun, () => reload())
</script>

<template>
  <div class="tasks-page">
    <contextHolder />
    <header class="tasks-head">
      <div>
        <h2 class="tasks-title">
          任务中心
        </h2>
        <p class="tasks-desc">
          每次调和（同步/定时/手动）都会留下一条运行记录，可逐步查看细节。
        </p>
      </div>
      <div class="tasks-ops">
        <Badge
          v-if="runningCount > 0"
          status="processing"
          :text="`${runningCount} 个进行中`"
        />
        <Button @click="reload">
          刷新
        </Button>
      </div>
    </header>

    <Spin :spinning="loading">
      <Table
        row-key="id"
        size="small"
        :columns="columns"
        :data-source="runs"
        :pagination="{ pageSize: 10, size: 'small' }"
        :custom-row="
          (record: Run) => ({
            onClick: () => open(record),
            style: { cursor: 'pointer' },
          })
        "
      >
        <template #emptyText>
          <Empty description="还没有运行记录" />
        </template>
      </Table>
    </Spin>

    <a-drawer
      :open="selected !== null"
      :title="selected ? `运行 ${selected.id}` : ''"
      :width="720"
      @close="selected = null"
    >
      <Spin :spinning="detailLoading">
        <div
          v-if="selected"
          class="run-detail"
        >
          <p class="run-detail-meta">
            <span>触发：{{ selected.trigger }}</span>
            <span>意图：{{ selected.intent || '—' }}</span>
            <span>耗时：{{ fmtCost(selected) }}</span>
          </p>
          <Timeline
            v-if="stepItems.length"
            :items="stepItems"
          />
          <Empty
            v-else
            description="这条运行没有步骤记录"
          />
        </div>
      </Spin>
    </a-drawer>
  </div>
</template>

<style scoped>
.tasks-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-md);
}
.tasks-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--gb-space-md);
}
.tasks-title {
  margin: 0;
  font-size: var(--gb-font-xl);
  font-weight: 700;
}
.tasks-desc {
  margin: var(--gb-space-xs) 0 0;
  color: var(--gb-color-text-secondary);
  font-size: var(--gb-font-sm);
  max-width: 640px;
}
.tasks-ops {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
}
.run-detail-meta {
  display: flex;
  gap: var(--gb-space-lg);
  color: var(--gb-color-text-secondary);
  font-size: var(--gb-font-sm);
  margin: 0 0 var(--gb-space-md);
}
.run-step {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-xs);
}
.run-step-head {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
}
.run-step-comp {
  font-weight: 600;
}
.run-step-time {
  color: var(--gb-color-text-secondary);
  font-size: var(--gb-font-xs);
}
.run-step-detail,
.run-step-event {
  margin: 0;
  padding: var(--gb-space-xs) var(--gb-space-sm);
  background: var(--gb-color-hover);
  border-radius: var(--gb-radius-sm);
  font-family: var(--gb-font-xs);
  font-size: var(--gb-font-xs);
  white-space: pre-wrap;
  word-break: break-all;
}
</style>