<script setup lang="ts">
import { ref, computed, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import RouteTab from './gateway/RouteTab.vue'
import PortsTab from './gateway/PortsTab.vue'
import FragmentTab from './gateway/FragmentTab.vue'
import VariableTab from './gateway/VariableTab.vue'

const route = useRoute()
const router = useRouter()
const active = computed(() => (route.query.tab as string) || 'routes')

// FragmentTab 请求打开变量添加抽屉
const varTabRef = ref<InstanceType<typeof VariableTab> | null>(null)
function openVarAdd() {
  if (active.value === 'variables') {
    varTabRef.value?.openAdd()
  } else {
    router.push({ query: { tab: 'variables', add: '1' } })
  }
}

// 从路由 ?add=1 自动打开
watch(() => route.query.add, (v) => {
  if (v === '1' && active.value === 'variables') {
    setTimeout(() => varTabRef.value?.openAdd(), 100)
    router.replace({ query: { tab: 'variables' } })
  }
}, { immediate: true })
</script>

<template>
  <RouteTab v-if="active === 'routes'" />
  <PortsTab v-else-if="active === 'ports'" />
  <FragmentTab v-else-if="active === 'fragments'" @openVarAdd="openVarAdd" />
  <VariableTab v-else-if="active === 'variables'" ref="varTabRef" />
</template>