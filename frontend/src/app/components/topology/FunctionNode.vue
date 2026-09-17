<script setup lang="ts">
import { Handle, Position, type NodeProps } from '@vue-flow/core'

defineProps<NodeProps>()
</script>

<template>
  <div class="topo-fn">
    <Handle type="target" :position="Position.Left" />
    <div class="topo-fn__title">{{ data.label }}</div>
    <div class="topo-fn__impl">实现：{{ (data.implementors ?? []).join(', ') }}</div>
    <Handle type="source" :position="Position.Right" />
  </div>
</template>

<style scoped>
.topo-fn {
  min-width: 160px;
  padding: 10px 12px;
  border: 1px solid rgba(22, 119, 255, 0.35);
  border-radius: 999px;
  background: #f0f7ff;
  text-align: center;
}
.topo-fn__title {
  font-weight: 600;
}
.topo-fn__impl {
  margin-top: 4px;
  font-size: 11px;
  color: rgba(0, 0, 0, 0.45);
}
</style>
