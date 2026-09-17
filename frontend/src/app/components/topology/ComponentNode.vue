<script setup lang="ts">
import { Handle, Position, type NodeProps } from '@vue-flow/core'

defineProps<NodeProps>()
</script>

<template>
  <div class="topo-node">
    <Handle type="target" :position="Position.Left" />
    <div class="topo-node__head">
      <span class="topo-node__dot" />
      <span class="topo-node__title">{{ data.label }}</span>
      <a-tag v-if="data.tier" :bordered="false">{{ data.tier }}</a-tag>
    </div>
    <div v-if="data.functions?.length" class="topo-node__tags">
      <a-tag v-for="f in data.functions" :key="f" color="blue" :bordered="false">{{ f }}</a-tag>
    </div>
    <div class="topo-node__meta">
      <span v-for="c in data.consumes ?? []" :key="c.info">↓{{ c.info }} {{ c.count }}</span>
      <span v-for="p in data.produces ?? []" :key="p.info">↑{{ p.info }} {{ p.count }}</span>
    </div>
    <Handle type="source" :position="Position.Right" />
  </div>
</template>

<style scoped>
.topo-node {
  min-width: 200px;
  padding: 10px 12px;
  border: 1px solid rgba(0, 0, 0, 0.12);
  border-radius: 8px;
  background: #fff;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
}
.topo-node__head {
  display: flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
}
.topo-node__dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #1677ff;
}
.topo-node__tags {
  margin-top: 6px;
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}
.topo-node__meta {
  margin-top: 6px;
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  font-size: 11px;
  color: rgba(0, 0, 0, 0.45);
}
</style>
