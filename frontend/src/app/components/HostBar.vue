<script setup lang="ts">
/**
 * 顶部主机条 + 离线横幅（原型 hostbar）。
 *
 * 主机是「维度」不是「页面」：这里只给筛选，不做跳转。
 * 离线横幅说清三件事——谁离线、为什么（agent 不可达）、后果（资源标未知、操作禁用）。
 *
 * 角标数从 useHostFilter 的模块级单例取（各 Tab 用 setCounts 回填），
 * 所以这里**不收 props.counts**：同一屏可能并排多个 Tab，传 props 会各说各话。
 */
import { computed } from 'vue'
import { Tooltip } from 'ant-design-vue'
import { useHostFilter } from '@/app/composables/useHostFilter'

const props = defineProps<{
  /** 'containers' | 'compose' | 'images' …，只用于离线横幅的文案。 */
  resource?: string
}>()

const {
  tabHosts, online, offline, filter, lastSeenOf,
  setFilter, countOf, reload,
} = useHostFilter()

const RESOURCE_LABEL: Record<string, string> = {
  containers: '容器',
  compose: '编排',
  images: '镜像',
  networks: '网络',
  volumes: '存储卷',
}
const resourceLabel = computed(() => RESOURCE_LABEL[props.resource ?? ''] ?? '资源')

/** 离线横幅：给结论，不只报状态。 */
const offlineText = computed(() => {
  if (!offline.value.length) return ''
  const names = offline.value.map((h) => String(h.spec?.host ?? h.key)).join('、')
  const last = lastSeenOf(offline.value[0] ? String(offline.value[0].spec?.host) : '')
  const when = last ? new Date(last).toLocaleString() : '未知'
  return `主机 ${names} 离线（agent 不可达，最后在线 ${when}）—— 其${resourceLabel.value}标「未知」，操作已禁用`
})

function pick(v: string) {
  setFilter(v)
}
</script>

<template>
  <div class="hostbar">
    <span class="hb-lb">主机</span>
    <button
      type="button"
      class="hostchip"
      :class="{ on: filter === 'all' }"
      @click="pick('all')"
    >
      全部主机<span class="hc-n">{{ tabHosts.length }}</span>
    </button>
    <button
      v-for="h in tabHosts"
      :key="h.name"
      type="button"
      class="hostchip"
      :class="{ on: filter === h.name }"
      @click="pick(h.name)"
    >
      <span class="hdot" :class="h.online ? 'on' : 'off'" />
      <Tooltip :title="h.online ? 'agent 在线' : 'agent 不可达（主机离线）'">
        <span>{{ h.name }}</span>
      </Tooltip>
      <span
        v-if="countOf(h.name) !== undefined"
        class="hc-n"
      >{{ countOf(h.name) }}</span>
    </button>
    <span class="hb-sp" />
    <span class="hb-sum">agent 在线 {{ online.length }}/{{ tabHosts.length }}</span>
    <button
      type="button"
      class="hb-refresh"
      title="刷新主机状态"
      @click="reload()"
    >
      ↻
    </button>
  </div>

  <div
    v-if="offlineText"
    class="hostwarn"
  >
    {{ offlineText }}
  </div>
</template>

<style scoped>
.hostbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  margin: 0 0 10px;
}
.hb-lb {
  margin-right: 2px;
  font-size: 12px;
  color: var(--gb-color-text-secondary);
}
.hostchip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--gb-color-border-secondary);
  border-radius: 14px;
  background: var(--gb-color-bg-container);
  color: var(--gb-color-text-secondary);
  font-size: 12px;
  cursor: pointer;
}
.hostchip:hover {
  border-color: var(--gb-color-primary);
  color: var(--gb-color-primary);
}
.hostchip.on {
  border-color: var(--gb-color-primary);
  background: var(--gb-color-primary-bg);
  color: var(--gb-color-primary);
}
.hc-n {
  color: var(--gb-color-text-tertiary);
  font-size: 11px;
}
.hb-sp {
  flex: 1;
}
.hb-sum {
  font-size: 12px;
  color: var(--gb-color-text-tertiary);
}
.hb-refresh {
  border: 0;
  background: transparent;
  color: var(--gb-color-text-tertiary);
  cursor: pointer;
  font-size: 14px;
}
.hb-refresh:hover {
  color: var(--gb-color-primary);
}

/* 在线/离线点：绿=agent 在线，红=不可达。 */
.hdot {
  display: inline-block;
  width: 7px;
  height: 7px;
  border-radius: 50%;
}
.hdot.on {
  background: var(--gb-color-success);
}
.hdot.off {
  background: var(--gb-color-error);
}

.hostwarn {
  margin-bottom: 10px;
  padding: 8px 12px;
  border: 1px solid var(--gb-color-warning-bg);
  border-radius: var(--gb-radius-base);
  background: var(--gb-color-warning-bg);
  color: var(--gb-color-warning);
  font-size: 12px;
}
</style>