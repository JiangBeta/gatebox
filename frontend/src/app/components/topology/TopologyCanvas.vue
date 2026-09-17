<script setup lang="ts">
import { computed } from 'vue'
import { VueFlow, MarkerType, type Node, type Edge } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { layoutGraph } from '@/lib/graph/layout'
import type { GraphModel } from '@/api/graph'
import ComponentNode from './ComponentNode.vue'
import FunctionNode from './FunctionNode.vue'
import '@vue-flow/core/dist/style.css'
import '@vue-flow/core/dist/theme-default.css'
import '@vue-flow/controls/dist/style.css'

const props = defineProps<{
  model: GraphModel
  states?: Record<string, string>
  runSteps?: Record<string, string>
}>()
const emit = defineEmits<{ (e: 'select', id: string): void }>()

const nodes = computed<Node[]>(() => {
  const pos = layoutGraph({
    nodes: props.model.nodes.map((n) => ({ id: n.id })),
    edges: props.model.edges.map((e) => ({ from: e.from, to: e.to })),
  })
  return props.model.nodes.map((n) => ({
    id: n.id,
    type: n.kind === 'function' ? 'function' : 'component',
    position: pos[n.id] ?? { x: 0, y: 0 },
    data: { ...n, state: props.states?.[n.id], runState: props.runSteps?.[n.id] },
  }))
})

const edges = computed<Edge[]>(() =>
  props.model.edges.map((e) => {
    const active = !!props.runSteps?.[e.from] || !!props.runSteps?.[e.to]
    return {
      id: `${e.from}->${e.to}:${e.info}`,
      source: e.from,
      target: e.to,
      label: e.instances.length > 0 ? `${e.info} ×${e.instances.length}` : e.info,
      markerEnd: MarkerType.ArrowClosed,
      animated: e.cycle || active,
      style: { stroke: e.cycle ? '#ef4444' : active ? '#1677ff' : '#94a3b8' },
      labelStyle: { fontSize: '11px', fill: '#64748b' },
    }
  }),
)
</script>

<template>
  <VueFlow
    :nodes="nodes"
    :edges="edges"
    :fit-view="true"
    :min-zoom="0.2"
    :max-zoom="2"
    class="topo-canvas"
    @node-click="(p: { node: { id: string } }) => emit('select', p.node.id)"
  >
    <Background :gap="20" pattern-color="#e2e8f0" />
    <Controls :show-interactive="false" />
    <template #node-component="p">
      <ComponentNode v-bind="p" />
    </template>
    <template #node-function="p">
      <FunctionNode v-bind="p" />
    </template>
  </VueFlow>
</template>

<style scoped>
.topo-canvas {
  width: 100%;
  height: 100%;
}
</style>
