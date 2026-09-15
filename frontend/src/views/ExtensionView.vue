<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import ComponentTab from './extension/ComponentTab.vue'
import PluginTab from './extension/PluginTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'components')
</script>

<template>
  <ComponentTab v-if="active === 'components'" />
  <PluginTab v-else-if="active === 'plugins'" />
</template>
