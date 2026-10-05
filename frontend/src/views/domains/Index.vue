<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import OverviewTab from '../domain/OverviewTab.vue'
import RootDomainTab from '../domain/RootDomainTab.vue'
import CertTab from '../domain/CertTab.vue'
import CredentialTab from '../domain/CredentialTab.vue'

/**
 * 域名（8091 原型：概览 / 域名 / 凭证）。
 *
 * 「概览」= 统计卡 + 根域名对象（原型 `kinds=[domain], cards=domain-overview`）。
 * 「域名」= 证书管理（acme.sh 签发 / 续期，走旧 `/api/domains` 面）。
 * 「凭证」= DNS 凭证（旧 `/api/credentials` 面，证书签发读的就是它；
 *   对象面 `dns-credential` 已实现，但首批迁移不重选凭证，两者暂时并存）。
 */
const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'overview')
</script>

<template>
  <div class="domains-page">
    <template v-if="active === 'overview'">
      <OverviewTab />
      <RootDomainTab />
    </template>
    <CertTab v-else-if="active === 'domains'" />
    <CredentialTab v-else-if="active === 'credentials'" />
    <OverviewTab v-else />
  </div>
</template>

<style scoped>
.domains-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-lg);
}
</style>