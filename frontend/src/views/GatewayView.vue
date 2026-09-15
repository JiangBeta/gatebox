<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import RouteTab from './gateway/RouteTab.vue'
import PortsTab from './gateway/PortsTab.vue'
import FragmentTab from './gateway/FragmentTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'routes')
</script>

<template>
  <RouteTab v-if="active === 'routes'" />
  <PortsTab v-else-if="active === 'ports'" />
  <FragmentTab v-else-if="active === 'fragments'" />
</template>
