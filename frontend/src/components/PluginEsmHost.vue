<script setup lang="ts">
// PluginEsmHost：L2 远程 ESM 组件宿主（ADR-039 §3）。
//
// 仅 official/verified 信任级可用；同源动态 import 插件入口模块。
// 插件模块约定：export function mount(el, ctx) / export function unmount(el)（可选）。
import { onMounted, onUnmounted, ref } from 'vue'
import { Alert } from 'ant-design-vue'
import http from '../api/http'

const props = defineProps<{ id: string; title?: string; entry?: string }>()

const el = ref<HTMLDivElement | null>(null)
const error = ref('')
let mod: { mount?: (el: HTMLElement, ctx: unknown) => void; unmount?: (el: HTMLElement) => void } | null = null

onMounted(async () => {
  const url = `/plugins/${props.id}/${props.entry || 'index.js'}`
  let token = ''
  try {
    token = (await http.get(`/plugins/${props.id}/token`)).data?.token || ''
  } catch {
    token = ''
  }
  try {
    // 同源 ESM：受信任插件（official/verified）才允许加载。
    mod = (await import(/* @vite-ignore */ url)) as typeof mod
    mod?.mount?.(el.value as HTMLElement, {
      pluginId: props.id,
      token,
      apiBase: `/api/v1/plugins/${props.id}`,
    })
  } catch (e) {
    error.value = `加载插件模块失败：${(e as Error).message}`
  }
})

onUnmounted(() => {
  try {
    if (el.value) mod?.unmount?.(el.value)
  } catch {
    /* 忽略卸载异常 */
  }
})
</script>

<template>
  <Alert v-if="error" type="error" show-icon :message="error" />
  <div v-show="!error" ref="el" class="plugin-esm-host" />
</template>

<style scoped>
.plugin-esm-host {
  width: 100%;
}
</style>
