<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { Alert, Card, Empty, Table, Tag, Typography } from 'ant-design-vue'
import { getGatewaySettings } from '@/api/settings'
import { listProviders, type DNSProviderSpec } from '@/api/credentials'
import ObjectKindPage from '@/app/components/objects/ObjectKindPage.vue'
import VariableTab from './VariableTab.vue'
import UserTab from './UserTab.vue'

/**
 * 设置（V4.1 对象面 + 保留旧面）。
 *
 * Tab 与 8091 原型一致：系统 / 用户 / DNS 供应商 / 主机 / 变量 / 端口。
 * 能对象化的一律走 ObjectKindPage（`host` / `acme` / `caddy-global` / `entrypoint`），
 * 只有「变量」还留在旧 API（`/settings/variables`）——它带系统变量表和迁移冲突处理，
 * 对象面那三个字段替代不了，先原样留着。
 */
const route = useRoute()
const active = computed(() => (route.query.tab as string) || 'system')

const settings = ref<{ acmeBin?: string; confFile: string }>({ confFile: '' })
onMounted(async () => {
  try {
    settings.value = await getGatewaySettings()
  } catch {
    /* 设置读不到不阻塞页面：下面几张卡都有兜底文案 */
  }
})

// ── DNS 供应商（索引，不是对象实例）──────────────────────────────────────────
// `dns-provider` 是 typespec 的索引 kind（model/dns-provider/*.yaml 定义模型），
// 没有实例字段也不可 CRUD；真正带凭证的是 `dns-credential`（域名 → 凭证）。
// 这里展示内置供应商目录与当前 acme.sh hook 安装情况，「新增（从远程库导入）」属后续批次。
const providers = ref<DNSProviderSpec[]>([])
const providerLoading = ref(false)
async function loadProviders() {
  if (active.value !== 'providers' || providers.value.length) return
  providerLoading.value = true
  try {
    providers.value = await listProviders()
  } catch {
    providers.value = []
  } finally {
    providerLoading.value = false
  }
}
onMounted(loadProviders)

const providerColumns = [
  { title: '供应商', dataIndex: 'label', key: 'label' },
  { title: 'ID', dataIndex: 'id', key: 'id', width: 140 },
  { title: 'acme.sh hook', dataIndex: 'acmeHook', key: 'acmeHook', width: 160 },
  {
    title: '凭证字段',
    key: 'fields',
    render: (r: DNSProviderSpec) =>
      r.fields?.map((f) => (f.label ?? f.name) + (f.secret ? '（密钥）' : '')).join(' · ') || '—',
  },
  {
    title: '凭证数',
    key: 'count',
    width: 90,
    render: (r: DNSProviderSpec) => String(r.fields?.length ?? 0),
  },
]
</script>

<template>
  <div class="settings-page">
    <template v-if="active === 'system'">
      <ObjectKindPage
        kind="acme"
        title="ACME / 证书设置"
        description="acme.sh 全局参数（CA、邮箱、密钥类型）。单例对象，保存后由同步链路生效。"
      />
      <ObjectKindPage
        kind="caddy-global"
        title="Caddy 全局"
        description="Caddy 全局参数（日志级别、admin 地址、保留天数）。单例对象。"
      />
      <Card
        size="small"
        title="Caddy 运行时（直管）"
      >
        <Typography.Paragraph type="secondary">
          V4.1 的路由 / 服务 / 中间件在「服务」页对象化管理；这里保留旧的面，
          用于直接看生效中的 Caddyfile 分组、监听端口与片段。
        </Typography.Paragraph>
        <Alert
          type="info"
          show-icon
          :message="`配置文件：${settings.confFile || '未知'}`"
          :description="`acme.sh 路径：${settings.acmeBin || '（未配置，使用 PATH 中的 acme.sh）'}`"
        />
        <div class="settings-links">
          <RouterLink to="/gateway?tab=routes">代理（生效配置）</RouterLink>
          <RouterLink to="/gateway?tab=ports">端口</RouterLink>
          <RouterLink to="/gateway?tab=fragments">Caddy 片段</RouterLink>
        </div>
      </Card>
    </template>

    <UserTab v-else-if="active === 'users'" />
    <ObjectKindPage
      v-else-if="active === 'hosts'"
      kind="host"
      title="主机"
      description="多主机管理。本机 edge 由系统自动创建且全局唯一；手动新增只能是 worker。"
    />

    <Card
      v-else-if="active === 'providers'"
      size="small"
      title="DNS 供应商"
      :loading="providerLoading"
    >
      <Alert
        type="info"
        show-icon
        message="供应商是模型定义，不是对象"
        description="「勾选显示」与「从远程库新增」属后续批次。现在到「域名 → 凭证」添加凭证，凭证里选供应商。"
        class="settings-alert"
      />
      <Table
        :columns="providerColumns"
        :data-source="providers"
        :pagination="false"
        size="small"
        row-key="id"
        :scroll="{ x: 'max-content' }"
      >
        <template #emptyText>
          <Empty description="读不到供应商目录（acme.sh 未安装？）" />
        </template>
      </Table>
    </Card>

    <VariableTab v-else-if="active === 'variables'" />

    <ObjectKindPage
      v-else-if="active === 'ports'"
      kind="entrypoint"
      title="入口点"
      description="「协议 + 端口」的监听入口，每条路由都要选一个。tcp/udp 依赖 caddy-l4，未安装时置灰。"
      :extra-columns="[
        { title: '协议', dataIndex: 'protocol', key: 'protocol', width: 130 },
        {
          title: '端口',
          key: 'ports',
          width: 170,
          customRender: ({ record }) => {
            const p = record.spec?.ports
            return Array.isArray(p) && p.length ? p.join('、') : '—'
          },
        },
        { title: '网络', dataIndex: 'network', key: 'network', width: 110 },
      ]"
    />

    <Card
      v-else
      size="small"
    >
      <Tag>未知 tab</Tag>
      <Empty description="该 tab 尚未实现" />
    </Card>
  </div>
</template>

<style scoped>
.settings-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-lg);
}
.settings-alert {
  margin-bottom: var(--gb-space-sm);
}
.settings-links {
  display: flex;
  gap: var(--gb-space-lg);
  margin-top: var(--gb-space-sm);
}
</style>