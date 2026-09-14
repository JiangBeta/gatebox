<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { Row, Col, Card, Statistic, List, Tag, Empty } from 'ant-design-vue'
import { listComponents } from '../api/components'
import { listPlugins } from '../api/plugins'
import { overview } from '../api/domains'
import { dockerInfo, listContainers } from '../api/docker'

const loading = ref(false)
const compTotal = ref(0)
const compUpdates = ref(0)
const pluginTotal = ref(0)
const pluginEnabled = ref(0)
const domainCount = ref(0)
const credCount = ref(0)
const certExpiring = ref(0)
const certExpired = ref(0)
const dockerOk = ref(false)
const containerRunning = ref(0)
const containerTotal = ref(0)
const updateList = ref<{ ID: string; Name: string; current: string; latest: string }[]>([])

onMounted(load)

async function load() {
  loading.value = true
  try {
    const [comps, plugs, dom] = await Promise.all([listComponents(), listPlugins(), overview()])
    compTotal.value = comps.length
    updateList.value = comps.filter((c) => c.updateAvailable)
    compUpdates.value = updateList.value.length
    pluginTotal.value = plugs.length
    pluginEnabled.value = plugs.filter((p) => p.state === 'enabled').length
    domainCount.value = dom.domainCount
    credCount.value = dom.credentialCount
    certExpiring.value = dom.certExpiringSoon
    certExpired.value = dom.certExpired
  } catch {
    /* 部分接口异常不阻断仪表盘 */
  } finally {
    loading.value = false
  }
  try {
    await dockerInfo()
    const cs = await listContainers()
    dockerOk.value = true
    containerTotal.value = cs.length
    containerRunning.value = cs.filter((c) => c.state === 'running').length
  } catch {
    dockerOk.value = false
  }
}
</script>

<template>
  <div>
    <Row :gutter="[16, 16]">
      <Col :xs="24" :sm="12" :md="6">
        <Card :loading="loading">
          <Statistic title="组件" :value="compTotal" suffix="个" />
          <div v-if="compUpdates" style="margin-top: 6px">
            <Tag color="red">{{ compUpdates }} 个有更新</Tag>
          </div>
        </Card>
      </Col>
      <Col :xs="24" :sm="12" :md="6">
        <Card :loading="loading">
          <Statistic title="插件" :value="pluginTotal" suffix="个" />
          <div style="margin-top: 6px">
            <Tag color="green">{{ pluginEnabled }} 启用</Tag>
          </div>
        </Card>
      </Col>
      <Col :xs="24" :sm="12" :md="6">
        <Card :loading="loading">
          <Statistic title="域名 / 凭证" :value="domainCount" />
          <div style="margin-top: 6px; color: #888; font-size: 12px">凭证 {{ credCount }} 条</div>
        </Card>
      </Col>
      <Col :xs="24" :sm="12" :md="6">
        <Card :loading="loading">
          <Statistic
            title="证书"
            :value="certExpired + certExpiring"
            suffix="待处理"
          />
          <div style="margin-top: 6px; font-size: 12px">
            <Tag v-if="certExpired" color="red">已过期 {{ certExpired }}</Tag>
            <Tag v-if="certExpiring" color="orange">即将过期 {{ certExpiring }}</Tag>
            <span v-if="!certExpired && !certExpiring" style="color: #888">全部正常</span>
          </div>
        </Card>
      </Col>
    </Row>

    <Row :gutter="[16, 16]" style="margin-top: 16px">
      <Col :xs="24" :md="12">
        <Card title="容器" :loading="loading">
          <template v-if="dockerOk">
            <Statistic title="运行中 / 总数" :value="containerRunning" :suffix="`/ ${containerTotal}`" />
          </template>
          <template v-else>
            <Empty description="Docker 不可用" />
          </template>
        </Card>
      </Col>
      <Col :xs="24" :md="12">
        <Card title="组件更新" :loading="loading">
          <List v-if="updateList.length" :data-source="updateList" size="small">
            <template #renderItem="{ item }">
              <List.Item>
                <span>{{ item.Name }}</span>
                <span style="color: #888">{{ item.current || '—' }} → {{ item.latest }}</span>
              </List.Item>
            </template>
          </List>
          <Empty v-else description="暂无更新" />
        </Card>
      </Col>
    </Row>
  </div>
</template>
