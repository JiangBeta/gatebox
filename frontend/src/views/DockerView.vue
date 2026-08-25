<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { NEmpty } from 'naive-ui'
import ContainerTab from './docker/ContainerTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'containers')
</script>

<template>
  <ContainerTab v-if="active === 'containers'" />
  <n-empty v-else description="该功能开发中" style="margin-top: 80px" />
</template>
