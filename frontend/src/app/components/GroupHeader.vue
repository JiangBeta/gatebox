<script setup lang="ts">
/**
 * 列表组标题（原型 `.grp > .hd`）。
 *
 * 原型里每张表都套一层 panel，标题行是「名字 + 计数标签 + 右侧按钮」。我们各页之前
 * 各自随手写，右对齐按钮飘在表格上方、和表头没有任何视觉关联，扫起来像两样东西。
 * 统一到这一个组件后，标题、计数、按钮的相对位置在所有列表页一致。
 */
withDefaults(
  defineProps<{
    /** 组名（如「编排项目」）。 */
    name: string
    /** 计数标签（如 `12 项`）。传空则不渲染。 */
    tag?: string
  }>(),
  { tag: '' },
)
</script>

<template>
  <div class="grp-hd">
    <span class="grp-hd-name">{{ name }}</span>
    <span
      v-if="tag"
      class="grp-hd-tag"
    >{{ tag }}</span>
    <span class="grp-hd-sp" />
    <div class="grp-hd-ops">
      <slot />
    </div>
  </div>
</template>

<style scoped>
.grp-hd {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 0 10px;
  border-bottom: 1px solid var(--gb-color-border-secondary);
}
.grp-hd-name {
  font-weight: 600;
}
.grp-hd-tag {
  font-size: var(--gb-font-xs);
  color: var(--gb-color-text-secondary);
  background: var(--gb-color-fill-quaternary);
  border-radius: var(--gb-radius-sm);
  padding: 1px 6px;
}
.grp-hd-sp {
  flex: 1;
}
.grp-hd-ops {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
}
</style>