<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getGraph, type GraphModel, type GraphNode, type GraphView } from '@/api/graph'
import { getComponentActivity, listRuns, triggerReconcile } from '@/api/observe'
import type { ComponentActivity, Run } from '@/shared/observe'
import TopologyCanvas from '@/app/components/topology/TopologyCanvas.vue'
import { useEvents } from '@/app/composables/useEvents'

const view = ref<GraphView>('component')
const model = ref<GraphModel | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const selected = ref<GraphNode | null>(null)
const activity = ref<ComponentActivity | null>(null)

const { states, runSteps } = useEvents()

const runs = ref<Run[]>([])
const runsOpen = ref(false)
const reconciling = ref(false)

async function load() {
  loading.value = true
  error.value = null
  try {
    model.value = await getGraph(view.value)
  } catch (e) {
    error.value = e instanceof Error ? e.message : String(e)
  } finally {
    loading.value = false
  }
}

onMounted(load)
watch(view, load)

const viewOptions = [
  { label: '组件视角', value: 'component' },
  { label: '功能视角', value: 'function' },
]

const selectedId = computed(() => selected.value?.id ?? '')
async function onSelect(id: string) {
  selected.value = model.value?.nodes.find((n) => n.id === id) ?? null
  activity.value = null
  if (selected.value?.kind === 'component') {
    try {
      activity.value = await getComponentActivity(id)
    } catch {
      activity.value = null
    }
  }
}

async function openRuns() {
  runsOpen.value = true
  try {
    runs.value = await listRuns(30)
  } catch {
    runs.value = []
  }
}

async function reconcile() {
  reconciling.value = true
  try {
    await triggerReconcile()
    await load()
    if (runsOpen.value) await openRuns()
  } finally {
    reconciling.value = false
  }
}

const stateColor = (s?: string) =>
  s === 'running' ? 'success' : s === 'error' ? 'error' : s === 'degraded' ? 'warning' : 'default'
const stepColor = (s: string) =>
  s === 'success' ? 'success' : s === 'error' ? 'error' : s === 'degraded' ? 'warning' : 'default'
</script>

<template>
  <div class="topology">
    <div class="topology__bar">
      <a-segmented v-model:value="view" :options="viewOptions" />
      <a-button size="small" :loading="loading" @click="load">刷新</a-button>
      <a-button size="small" @click="openRuns">运行记录</a-button>
      <a-button size="small" type="primary" :loading="reconciling" @click="reconcile">立即调和</a-button>
      <a-alert v-if="error" type="error" :message="error" show-icon class="topology__alert" />
    </div>
    <div class="topology__body">
      <div class="topology__canvas">
        <TopologyCanvas
          v-if="model"
          :model="model"
          :states="states"
          :run-steps="runSteps"
          @select="onSelect"
        />
        <a-empty v-else description="暂无数据" />
      </div>
      <a-drawer
        :open="!!selected"
        placement="right"
        :width="380"
        :title="selected?.label ?? ''"
        @close="selected = null"
      >
        <template v-if="selected">
          <p>标识：{{ selectedId }}</p>
          <p>类型：{{ selected.kind }}</p>
          <p v-if="selected.tier">档位：{{ selected.tier }}</p>
          <p v-if="states[selectedId]">
            状态：<a-tag :color="stateColor(states[selectedId])" :bordered="false">{{ states[selectedId] }}</a-tag>
          </p>
          <div v-if="selected.functions?.length">
            <p>功能：</p>
            <a-tag v-for="f in selected.functions" :key="f">{{ f }}</a-tag>
          </div>
          <div v-if="selected.implementors?.length">
            <p>实现组件：{{ selected.implementors.join(', ') }}</p>
          </div>
          <div v-if="selected.consumes?.length">
            <p>消费信息：</p>
            <a-tag v-for="c in selected.consumes" :key="c.info">{{ c.info }} ({{ c.count }})</a-tag>
          </div>
          <div v-if="selected.produces?.length">
            <p>产出信息：</p>
            <a-tag v-for="p in selected.produces" :key="p.info">{{ p.info }} ({{ p.count }})</a-tag>
          </div>
          <a-divider v-if="selected.kind === 'component'">当前活动</a-divider>
          <p v-if="activity">任务：{{ activity.task || 'idle' }} · 步骤：{{ activity.step || '-' }}</p>
        </template>
      </a-drawer>
      <a-drawer :open="runsOpen" placement="right" :width="520" title="运行记录" @close="runsOpen = false">
        <a-empty v-if="runs.length === 0" description="暂无运行记录" />
        <a-collapse v-else ghost>
          <a-collapse-panel v-for="r in runs" :key="r.id">
            <template #header>
              <a-tag :color="stepColor(r.state)" :bordered="false">{{ r.state }}</a-tag>
              {{ r.trigger }} · {{ r.intent || 'full' }}
            </template>
            <p class="topology__run-id">{{ r.id }}</p>
            <a-timeline>
              <a-timeline-item v-for="(s, i) in r.steps" :key="i" :color="stepColor(s.state)">
                <b>{{ s.component }}</b> · {{ s.action }} · {{ s.state }}
                <div v-if="s.detail" class="topology__detail">{{ s.detail }}</div>
              </a-timeline-item>
            </a-timeline>
          </a-collapse-panel>
        </a-collapse>
      </a-drawer>
    </div>
  </div>
</template>

<style scoped>
.topology {
  display: flex;
  flex-direction: column;
  height: 100%;
}
.topology__bar {
  display: flex;
  align-items: center;
  gap: var(--gb-space-md);
  padding: var(--gb-space-md);
}
.topology__alert {
  flex: 1;
}
.topology__body {
  flex: 1;
  min-height: 0;
  display: flex;
}
.topology__canvas {
  flex: 1;
  min-width: 0;
}
.topology__run-id {
  color: var(--gb-color-text-secondary, var(--ant-color-text-secondary, inherit));
  font-size: var(--gb-font-xs);
  word-break: break-all;
}
.topology__detail {
  color: var(--gb-color-text-secondary, var(--ant-color-text-secondary, inherit));
  font-size: var(--gb-font-xs);
}
</style>
