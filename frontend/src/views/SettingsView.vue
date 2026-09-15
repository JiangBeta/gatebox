<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { Card, Alert, message } from 'ant-design-vue'
import { getGatewaySettings } from '../api/settings'
import VariableTab from './settings/VariableTab.vue'

const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'general')

const [messageApi, contextHolder] = message.useMessage()

const acmeBin = ref('')
const confFile = ref('')
const loading = ref(false)

onMounted(async () => {
  loading.value = true
  try {
    const s = await getGatewaySettings()
    acmeBin.value = s.acmeBin || ''
    confFile.value = s.confFile
  } catch (e: any) {
    messageApi.error('读取设置失败 — ' + e.message)
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <contextHolder />
  <VariableTab v-if="active === 'variables'" />
  <div v-else style="max-width: 640px; padding: 16px 24px">
    <Card size="small" title="网关端口" :loading="loading">
      <Alert
        type="info"
        show-icon
        message="端口由「网关 → 端口」管理（HTTP/HTTPS 可对应多个监听端口）"
        description="此处仅展示配置文件位置。"
      />
      <div style="font-family: monospace; font-size: 12px; margin-top: 8px; color: #555">{{ confFile || '-' }}</div>
      <div style="font-size: 12px; color: #888; margin-top: 4px">登录网关→端口 修改后需重启 GateBox 生效。</div>
    </Card>

    <Card size="small" title="证书签发（acme.sh DNS-01）" :style="{ marginTop: '12px' }">
      <Alert
        type="warning"
        show-icon
        message="需在 DNS 供应商侧具备域名解析权限的 API 凭证"
        description="证书由 acme.sh 按受管域名的 DNS 凭证签发（ADR-013）。当前 acme.sh 路径："
      />
      <div style="font-family: monospace; font-size: 12px; margin: 8px 0; color: #555">{{ acmeBin || '（未配置，将使用 PATH 中的 acme.sh）' }}</div>
    </Card>
  </div>
</template>
