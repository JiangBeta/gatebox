<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Card, Col, Row, Statistic, Spin } from 'ant-design-vue'
import { dockerInfo } from '@/api/docker'
import DashboardView from '../DashboardView.vue'

/**
 * 监控。
 *
 * 8091 原型这一页是空的（「该页在后续批次实现」），但把已有的仪表盘直接删掉
 * 等于砍功能，所以先原样接过来。原型里监控的四个 tab 是流量 / 健康 / 证书 / 解析，
 * 都要采集端配合（docs/v4.1/views.md §10），本轮不做空壳 tab。
 */
const loading = ref(false)
const hostOk = ref(false)
const hostVersion = ref('')

onMounted(async () => {
  loading.value = true
  try {
    const info = await dockerInfo()
    hostOk.value = Boolean(info)
    hostVersion.value = info?.ServerVersion || ''
  } catch {
    hostOk.value = false
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <div class="monitor-page">
    <Spin :spinning="loading">
      <Row
        :gutter="[16, 16]"
      >
        <Col :span="8">
          <Card size="small">
            <Statistic
              title="容器守护进程"
              :value="hostOk ? '在线' : '不可达'"
              :value-style="{ color: hostOk ? undefined : 'var(--gb-color-error)' }"
            />
            <div class="monitor-sub">
              {{ hostVersion ? `Docker ${hostVersion}` : 'Docker 未连接' }}
            </div>
          </Card>
        </Col>
      </Row>
      <DashboardView />
    </Spin>
  </div>
</template>

<style scoped>
.monitor-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-lg);
}
.monitor-sub {
  font-size: var(--gb-font-xs);
  color: var(--gb-color-text-secondary);
  margin-top: var(--gb-space-xs);
}
</style>