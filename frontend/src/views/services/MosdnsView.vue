<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import StatusTab from './mosdns/StatusTab.vue'
import BasicTab from './mosdns/BasicTab.vue'
import HostsTab from './mosdns/HostsTab.vue'
import ConfigTab from './mosdns/ConfigTab.vue'
import LogsTab from './mosdns/LogsTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'status')
</script>

<template>
  <StatusTab v-if="active === 'status'" />
  <BasicTab v-else-if="active === 'basic'" />
  <HostsTab v-else-if="active === 'hosts'" />
  <ConfigTab v-else-if="active === 'config'" />
  <LogsTab v-else-if="active === 'logs'" />
  <StatusTab v-else />
</template>
