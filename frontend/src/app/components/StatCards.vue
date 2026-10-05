<script setup lang="ts">
/**
 * 页级统计卡（原型 `.statcards` / `.scard`）。
 *
 * 一张卡 = 标题 + 图标 + 主数字 + 副说明。副说明允许内嵌 <b>（分色计数，
 * 例如「在线 3 · 离线 1」里在线是绿的），所以 sub 用 HTML 串而不是纯文本。
 *
 * 数字必须来自真实列表（页面自己的数据源），不从后端再拉一份汇总——
 * 两份口径迟早会对不上，对不上时用户会先怀疑数据。
 */
import { computed, type Component } from 'vue'

export interface StatCard {
  title: string
  /** 主数字。字符串也算数：页面需要显示 '3 / 12' 这种复合值。 */
  main: string | number
  /** 副说明（纯文本）。 */
  sub?: string
  /** 副说明（HTML，允许 <b style="color:...">）。与 sub 二选一，subHTML 优先。 */
  subHTML?: string
  /** 右上角图标 + 主色调。tone 用 hex，方便直接压到 8 位 alpha 做背景。 */
  icon?: Component
  tone?: string
  /** 点击跳转：{ page, tab, filter? }。filter 会被目标页当查询参数读。 */
  link?: { page: string; tab?: string; filter?: string }
}

const props = defineProps<{ cards: StatCard[] }>()
const emit = defineEmits<{ navigate: [link: NonNullable<StatCard['link']>] }>()

const items = computed(() => props.cards.filter(Boolean))
</script>

<template>
  <div v-if="items.length" class="statcards">
    <div
      v-for="(c, i) in items"
      :key="`${c.title}-${i}`"
      class="scard"
      :class="{ clickable: !!c.link }"
      @click="c.link && emit('navigate', c.link)"
    >
      <div class="srow">
        <span class="stitle">{{ c.title }}</span>
        <span
          v-if="c.icon"
          class="sico"
          :style="{ color: c.tone || '#1677ff', background: `${c.tone || '#1677ff'}1f` }"
        >
          <component :is="c.icon" />
        </span>
      </div>
      <div class="smain">{{ c.main }}</div>
      <!-- eslint-disable-next-line vue/no-v-html -- 副说明里的分色 <b> 是刻意允许的 -->
      <div v-if="c.subHTML || c.sub" class="ssub" v-html="c.subHTML || c.sub" />
    </div>
  </div>
</template>

<style scoped>
.statcards {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
  gap: 16px;
  margin-bottom: 18px;
}
.scard {
  position: relative;
  background: var(--gb-color-bg-container, #fff);
  border: 1px solid var(--gb-color-border, #e5e8ec);
  border-radius: 10px;
  padding: 12px 14px;
}
.scard.clickable {
  cursor: pointer;
  transition: border-color 0.15s ease, box-shadow 0.15s ease, transform 0.15s ease;
}
.scard.clickable:hover {
  border-color: var(--gb-color-primary, #1677ff);
  box-shadow: 0 6px 18px rgba(0, 0, 0, 0.12);
  transform: translateY(-1px);
}
.srow {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.stitle {
  font-size: 12.5px;
  color: var(--gb-color-text-secondary, #5b6470);
}
.sico {
  width: 30px;
  height: 30px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 15px;
}
.smain {
  font-size: 28px;
  font-weight: 650;
  margin: 8px 0 4px;
  letter-spacing: -0.5px;
}
.ssub {
  font-size: 12px;
  color: var(--gb-color-text-tertiary, #8b95a7);
}
</style>