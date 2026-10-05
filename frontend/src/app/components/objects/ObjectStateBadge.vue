<script setup lang="ts">
import { computed } from 'vue'
import type { ObjectState } from '@/api/objects'

/**
 * 对象的自动状态徽标（status.state，ADR-043 §4）。
 *
 * 四态与颜色是**语义**不是装饰：未生效=黄（配置已存、还没进 Caddyfile）、
 * 已生效=绿、错误=红（带原因 tooltip）、停用=灰。所以颜色来自 CSS 变量而非
 * antd 的 status 色板——暗色主题下也要跟着变。
 */
const props = defineProps<{ state?: ObjectState; error?: string; pending?: boolean }>()

/**
 * 状态取值 → 文案与色调。
 *
 * 键用**后端实际发出的中文值**（backend/internal/objects/object.go 的 State* 常量，
 * 取自各 model/*.yaml 的 status.enum）。曾按英文 pending/applied/error 映射，
 * 而后端从不发英文——于是所有徽标都掉进 idle 分支，语义颜色整个失效。
 * 英文键保留只为兼容手写/历史数据，读到时归一化到中文再查表。
 *
 * route 与 service 的枚举不同：route 五态、service 三态（运行状态），
 * 所以 service 的「未运行」按 pending（黄）处理——它同样意味着没跑起来。
 */
const TONE: Record<string, { text: string; tone: string }> = {
  已生效: { text: '已生效', tone: 'ok' },
  运行中: { text: '运行中', tone: 'ok' },
  证书告警: { text: '证书告警', tone: 'warn' },
  未生效: { text: '未生效', tone: 'warn' },
  未运行: { text: '未运行', tone: 'warn' },
  依赖缺失: { text: '依赖缺失', tone: 'err' },
  错误: { text: '错误', tone: 'err' },
  已停用: { text: '已停用', tone: 'idle' },
}

const ALIAS: Record<string, string> = {
  applied: '已生效',
  running: '运行中',
  pending: '未生效',
  notRunning: '未运行',
  certWarn: '证书告警',
  depMissing: '依赖缺失',
  error: '错误',
  disabled: '已停用',
}

const state = computed(() => props.state ?? '')
const hit = computed(() => TONE[ALIAS[state.value] ?? state.value])
const text = computed(() => hit.value?.text ?? (state.value || '—'))
const tone = computed(() => hit.value?.tone ?? 'idle')
</script>

<template>
  <span
    class="state-badge"
    :class="tone"
    :title="props.error || undefined"
  >
    <span
      v-if="tone === 'warn' || pending"
      class="state-dot"
    />
    {{ text }}
  </span>
</template>

<style scoped>
.state-badge {
  display: inline-flex;
  align-items: center;
  gap: var(--gb-space-xs);
  padding: 0 var(--gb-space-sm);
  height: 22px;
  line-height: 20px;
  border-radius: var(--gb-radius-sm);
  font-size: var(--gb-font-xs);
  white-space: nowrap;
  border: 1px solid var(--gb-color-border-secondary);
  color: var(--gb-color-text-secondary);
  background: var(--gb-color-hover);
}
.state-badge.ok {
  color: var(--gb-state-applied);
  border-color: var(--gb-color-success-bg);
  background: var(--gb-color-success-bg);
}
.state-badge.warn {
  color: var(--gb-state-pending);
  border-color: var(--gb-color-warning-bg);
  background: var(--gb-color-warning-bg);
}
.state-badge.err {
  color: var(--gb-state-error);
  border-color: var(--gb-color-error-bg);
  background: var(--gb-color-error-bg);
  cursor: help;
}
.state-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: currentColor;
  animation: state-pulse 1.2s ease-in-out infinite;
}
@keyframes state-pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.3;
  }
}
</style>
