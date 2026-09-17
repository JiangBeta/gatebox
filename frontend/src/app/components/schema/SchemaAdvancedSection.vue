<script setup lang="ts">
const open = defineModel<boolean>('open', { default: false })
</script>

<template>
  <a-collapse :active-key="open ? ['advanced'] : []" ghost @change="(k: unknown) => (open = Array.isArray(k) && k.length > 0)">
    <a-collapse-panel key="advanced" header="高级设置">
      <slot />
    </a-collapse-panel>
  </a-collapse>
</template>
