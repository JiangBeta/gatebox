<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { getGraph, type GraphModel, type GraphNode, type GraphView } from '@/api/graph'
import TopologyCanvas from '@/app/components/topology/TopologyCanvas.vue'

const view = ref<GraphView>('component')
const model = ref<GraphModel | null>(null)
const loading = ref(false)
const error = ref<string | null>(null)
const selected = ref<GraphNode | null>(null)

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
function onSelect(id: string) {
  selected.value = model.value?.nodes.find((n) => n.id === id) ?? null
}
</script>

<template>
  <div class="topology">
    <div class="topology__bar">
      <a-segmented v-model:value="view" :options="viewOptions" />
      <a-button size="small" :loading="loading" @click="load">刷新</a-button>
      <a-alert v-if="error" type="error" :message="error" show-icon class="topology__alert" />
    </div>
    <div class="topology__body">
      <div class="topology__canvas">
        <TopologyCanvas v-if="model" :model="model" @select="onSelect" />
        <a-empty v-else description="暂无数据" />
      </div>
      <a-drawer
        :open="!!selected"
        placement="right"
        :width="360"
        :title="selected?.label ?? ''"
        @close="selected = null"
      >
        <template v-if="selected">
          <p>标识：{{ selectedId }}</p>
          <p>类型：{{ selected.kind }}</p>
          <p v-if="selected.tier">档位：{{ selected.tier }}</p>
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
        </template>
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
</style>
