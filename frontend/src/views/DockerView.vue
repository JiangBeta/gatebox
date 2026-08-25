<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import { NEmpty } from 'naive-ui'
import ContainerTab from './docker/ContainerTab.vue'
import ComposeTab from './docker/ComposeTab.vue'
import ImageTab from './docker/ImageTab.vue'
import NetworkTab from './docker/NetworkTab.vue'
import VolumeTab from './docker/VolumeTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'containers')
</script>

<template>
  <ContainerTab v-if="active === 'containers'" />
  <ComposeTab v-else-if="active === 'compose'" />
  <ImageTab v-else-if="active === 'images'" />
  <NetworkTab v-else-if="active === 'networks'" />
  <VolumeTab v-else-if="active === 'volumes'" />
  <n-empty v-else description="该功能开发中" style="margin-top: 80px" />
</template>
