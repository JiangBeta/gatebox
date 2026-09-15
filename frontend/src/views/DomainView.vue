<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import OverviewTab from './domain/OverviewTab.vue'
import CertTab from './domain/CertTab.vue'
import CredentialTab from './domain/CredentialTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'overview')
</script>

<template>
  <OverviewTab v-if="active === 'overview'" />
  <CertTab v-else-if="active === 'cert'" />
  <CredentialTab v-else-if="active === 'credential'" />
  <OverviewTab v-else />
</template>
