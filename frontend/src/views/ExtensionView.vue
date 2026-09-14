<script setup lang="ts">
import { ref } from 'vue'
import { Tabs } from 'ant-design-vue'
import ComponentTab from './extension/ComponentTab.vue'
import PluginTab from './extension/PluginTab.vue'

const active = ref('components')
</script>

<template>
  <Tabs v-model:activeKey="active">
    <Tabs.TabPane key="components" tab="组件">
      <ComponentTab />
    </Tabs.TabPane>
    <Tabs.TabPane key="plugins" tab="插件">
      <PluginTab />
    </Tabs.TabPane>
  </Tabs>
</template>
