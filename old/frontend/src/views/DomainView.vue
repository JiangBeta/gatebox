<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import OverviewTab from './domain/OverviewTab.vue'
import DomainManageTab from './domain/DomainManageTab.vue'
import CertTab from './domain/CertTab.vue'
import CredentialTab from './domain/CredentialTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'overview')
</script>

<template>
  <OverviewTab v-if="active === 'overview'" />
  <DomainManageTab v-else-if="active === 'manage'" />
  <CertTab v-else-if="active === 'cert'" />
  <CredentialTab v-else />
</template>
