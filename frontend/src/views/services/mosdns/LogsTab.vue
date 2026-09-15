<script setup lang="ts">
import { nextTick, onMounted, onUnmounted, ref } from 'vue'
import { Button, Card, Empty, Popconfirm, Space, Switch, Tag, message } from 'ant-design-vue'
import { ClearOutlined, ReloadOutlined } from '@ant-design/icons-vue'
import { clearLogs, getLogs } from '../../../api/mosdns'

const [messageApi, contextHolder] = message.useMessage()
const content = ref('')
const path = ref('')
const loading = ref(false)
const autoRefresh = ref(true)
const preRef = ref<HTMLElement | null>(null)
let timer: number | undefined

async function load(scroll = true) {
  try {
    const res = await getLogs()
    content.value = res.content
    path.value = res.path
    if (scroll) {
      await nextTick()
      if (preRef.value) preRef.value.scrollTop = preRef.value.scrollHeight
    }
  } catch (e) {
    messageApi.error((e as Error).message)
  }
}

async function onClear() {
  loading.value = true
  try {
    await clearLogs()
    content.value = ''
    messageApi.success('日志已清空')
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

function syncTimer() {
  if (timer) window.clearInterval(timer)
  if (autoRefresh.value) timer = window.setInterval(() => load(true), 3000)
}

onMounted(() => {
  void load()
  syncTimer()
})
onUnmounted(() => {
  if (timer) window.clearInterval(timer)
})
</script>

<template>
  <contextHolder />
  <Card title="运行日志" size="small">
    <template #extra>
      <Tag v-if="path" style="font-family: monospace">{{ path }}</Tag>
    </template>

    <Space style="margin-bottom: 12px">
      <Button :loading="loading" @click="load(true)">
        <template #icon><ReloadOutlined /></template>
        刷新
      </Button>
      <Popconfirm title="确定清空日志？" @confirm="onClear">
        <Button danger>
          <template #icon><ClearOutlined /></template>
          清空
        </Button>
      </Popconfirm>
      <span>自动刷新（3s）<Switch v-model:checked="autoRefresh" size="small" style="margin-left: 8px" @change="syncTimer" /></span>
    </Space>

    <pre
      v-if="content"
      ref="preRef"
      style="max-height: 520px; overflow: auto; margin: 0; padding: 12px; background: #1e1e1e; color: #d4d4d4; font-size: 12px; line-height: 1.6; border-radius: 6px; white-space: pre-wrap; word-break: break-all"
    >{{ content }}</pre>
    <Empty v-else description="暂无日志" />
  </Card>
</template>
