<script setup lang="ts">
import { ref, watch, onMounted, onUnmounted } from 'vue'
import { Popover, Badge, Tag, Button, Empty } from 'ant-design-vue'
import {
  SyncOutlined, LoadingOutlined, CheckCircleOutlined, CloseCircleOutlined, ClearOutlined,
} from '@ant-design/icons-vue'
import { useTaskStore, type TaskItem } from '../stores/tasks'

const { tasks, runningCount, clearFinishedTasks, removeTask } = useTaskStore()
const open = ref(false)

// 运行中任务的「已执行时间」需秒级刷新；没有运行中任务时停表省电。
const now = ref(Date.now())
let timer: ReturnType<typeof setInterval> | undefined
function syncTimer() {
  if (runningCount.value > 0 && !timer) {
    timer = setInterval(() => { now.value = Date.now() }, 1000)
  } else if (runningCount.value === 0 && timer) {
    clearInterval(timer)
    timer = undefined
  }
}
watch(runningCount, syncTimer)
onMounted(syncTimer)
onUnmounted(() => { if (timer) clearInterval(timer) })

function pad(n: number) { return String(n).padStart(2, '0') }
function fmtTime(ts?: number) {
  if (!ts) return '-'
  const d = new Date(ts)
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}
function fmtDuration(ms: number) {
  const s = Math.max(0, Math.floor(ms / 1000))
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  const r = s % 60
  return r ? `${m} 分 ${r} 秒` : `${m} 分`
}
function timeText(t: TaskItem) {
  if (t.status === 'running') return `已执行 ${fmtDuration(now.value - t.startedAt)}`
  return `${t.status === 'error' ? '失败' : '完成'}于 ${fmtTime(t.finishedAt)}`
}
</script>

<template>
  <Popover v-model:open="open" trigger="click" placement="bottomRight" :overlay-style="{ width: '360px' }">
    <template #content>
      <div class="tc">
        <div class="tc-head">
          <span class="tc-title">任务</span>
          <Button v-if="tasks.length" type="link" size="small" @click="clearFinishedTasks">
            <ClearOutlined /> 清除已完成
          </Button>
        </div>
        <div v-if="!tasks.length" class="tc-empty">
          <Empty :image="Empty.PRESENTED_IMAGE_SIMPLE" description="暂无任务" />
        </div>
        <div v-else class="tc-list">
          <div v-for="t in tasks" :key="t.id" class="tc-item">
            <div class="tc-item-main">
              <span class="tc-name" :title="t.name">{{ t.name }}</span>
              <Tag v-if="t.status === 'running'" color="processing">
                <LoadingOutlined /> 运行中
              </Tag>
              <Tag v-else-if="t.status === 'done'" color="success">
                <CheckCircleOutlined /> 已完成
              </Tag>
              <Tag v-else color="error">
                <CloseCircleOutlined /> 失败
              </Tag>
            </div>
            <div class="tc-item-sub">{{ timeText(t) }}</div>
            <div v-if="t.error" class="tc-error">{{ t.error }}</div>
            <span v-if="t.status !== 'running'" class="tc-remove" title="移除" @click="removeTask(t.id)">✕</span>
          </div>
        </div>
      </div>
    </template>
    <span class="tc-trigger" :class="{ active: open }">
      <Badge :count="runningCount" :offset="[2, -2]" size="small">
        <SyncOutlined :spin="runningCount > 0" />
      </Badge>
    </span>
  </Popover>
</template>

<style scoped>
.tc-trigger {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  border-radius: 6px;
  font-size: 16px;
  color: #666;
  cursor: pointer;
}
.tc-trigger:hover,
.tc-trigger.active {
  background: #f0f0f0;
  color: #333;
}
.tc-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 6px;
  border-bottom: 1px solid #f0f0f0;
}
.tc-title {
  font-size: 14px;
  font-weight: 600;
}
.tc-empty {
  padding: 12px 0;
}
.tc-list {
  max-height: 360px;
  overflow: auto;
  margin: 0 -4px;
}
.tc-item {
  position: relative;
  padding: 8px 4px;
  border-bottom: 1px solid #f5f5f5;
}
.tc-item:last-child {
  border-bottom: 0;
}
.tc-item-main {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
}
.tc-name {
  flex: 1;
  min-width: 0;
  font-size: 13px;
  color: #333;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tc-item-sub {
  margin-top: 2px;
  font-size: 12px;
  color: #999;
}
.tc-error {
  margin-top: 2px;
  font-size: 12px;
  color: #ff4d4f;
  word-break: break-all;
}
.tc-remove {
  position: absolute;
  right: 2px;
  bottom: 8px;
  padding: 0 4px;
  font-size: 11px;
  color: #bbb;
  cursor: pointer;
}
.tc-remove:hover {
  color: #666;
}
:deep(.ant-tag) {
  margin-inline-end: 0;
}
</style>
