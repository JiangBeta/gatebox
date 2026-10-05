<script setup lang="ts">
import { computed, onMounted, ref, type VNode } from 'vue'
import { Button, Modal, message, Spin, Tooltip } from 'ant-design-vue'
import type { ColumnType } from 'ant-design-vue/es/table/interface'
import { ApiError } from '@/api/http'
import {
  createObject,
  deleteObject,
  getObjectRefs,
  listObjects,
  renameObject,
  updateObject,
  getPendingSync,
  syncNow,
  type ObjectRef,
  type V41Object,
} from '@/api/objects'
import { useKindSpec } from '@/app/composables/useKindSpec'
import { invalidate as invalidateObjectOptions } from '@/app/composables/useObjectOptions'
import ObjectListTable from './ObjectListTable.vue'
import ObjectEditorDrawer from './ObjectEditorDrawer.vue'
import { OP_COLOR, type OpSpec } from '@/utils/cell'
import { CheckOutlined, MinusOutlined, EditOutlined } from '@ant-design/icons-vue'

/**
 * 一个 kind = 一页（V4.1 对象面，ADR-043 §3）。
 *
 * 中间件 / 服务 / 路由三页都是它的薄封装：只多给几列、不多写逻辑。
 * 这样「保存 → 未生效 → 去抖同步 → Run → 回写状态」这条链只有一处实现，
 * 页面不可能各自漏掉状态刷新。
 */
const props = withDefaults(
  defineProps<{
    kind: string
    title: string
    description?: string
    extraColumns?: ColumnType<V41Object>[]
    /** 额外行内操作（插在「删除」之前）。页自己算，页自己负责它能落地。 */
    rowOps?: (o: V41Object) => OpSpec[]
    /** 名称列渲染（默认 displayName）。路由页要用「名称 + 引用服务副行」覆盖。 */
    nameRender?: (o: V41Object) => VNode
    /** 只读类型（catalog）不给新建/编辑入口。 */
    readonly?: boolean
  }>(),
  { description: '', extraColumns: () => [], rowOps: undefined, nameRender: undefined, readonly: false },
)

const [messageApi, contextHolder] = message.useMessage()

const objects = ref<V41Object[]>([])
const loading = ref(false)
const saving = ref(false)
const formError = ref<string | null>(null)
const pending = ref(0)
const syncEnabled = ref(true)

const kindRef = computed(() => props.kind)
const { spec, fields, fieldsForType, kinds, loading: specLoading, error: specError } =
  useKindSpec(kindRef)

// 服务/中间件的参数按 type 展开（后端 ExpandType）：选了类型才出现对应参数。
const typeField = computed(() => (props.kind === 'service' || props.kind === 'middleware' ? 'type' : undefined))

const editorOpen = ref(false)
const editing = ref<V41Object | null>(null)

// 改名用模板里的真 Modal（而不是 Modal.confirm 的 render 函数）：
// 输入框要能 v-model、能校验，render 函数里塞 h(Input) 既难读又难做类型。
const renameOpen = ref(false)
const renameValue = ref("")
const renameBusy = ref(false)

const writeOnlyFields = computed(() => spec.value?.writeOnly ?? [])

/** 单例 kind（acme / caddy-global）：至多一条，页头不显示「新增」。 */
const singleton = computed(() => Boolean(spec.value?.singleton))

/** 模型文件标签（原型 `model/route.yaml` 那种）。 */
const modelFile = computed(() => (spec.value?.source ? `model/${spec.value.source}` : ''))

/** 原型的组头计数：`N 项`，只读/单例则是 `只读` / `单例`。 */
const countTag = computed(() => {
  if (props.readonly) return '只读'
  if (singleton.value) return '单例'
  return `${objects.value.length} 项`
})

async function reload() {
  loading.value = true
  try {
    objects.value = await listObjects(props.kind)
  } catch (e) {
    messageApi.error(e instanceof Error ? e.message : String(e))
  } finally {
    loading.value = false
  }
  await refreshPending()
}

async function refreshPending() {
  try {
    const p = await getPendingSync()
    pending.value = p.pending
    syncEnabled.value = p.enabled
  } catch {
    syncEnabled.value = false
  }
}

function openCreate() {
  editing.value = null
  formError.value = null
  editorOpen.value = true
}

function openEdit(o: V41Object) {
  editing.value = o
  formError.value = null
  editorOpen.value = true
}

async function onSubmit(payload: { spec: Record<string, unknown>; writeOnly: Record<string, unknown> }) {
  saving.value = true
  formError.value = null
  try {
    if (editing.value) {
      await updateObject(props.kind, editing.value.id, payload.spec, payload.writeOnly)
      messageApi.success('已保存，同步后生效')
    } else {
      await createObject(props.kind, payload.spec, payload.writeOnly)
      messageApi.success('已创建，同步后生效')
    }
    invalidateObjectOptions()
    editorOpen.value = false
    await reload()
  } catch (e) {
    formError.value = e instanceof Error ? e.message : String(e)
  } finally {
    saving.value = false
  }
}

/** 改名：换 id + 级联改引用（普通更新改主键会被后端 409 拒）。 */
function onRename(currentKey: string) {
  renameValue.value = currentKey
  renameOpen.value = true
}

/**
 * 启停：改的是 spec.enabled 字段本身，走和抽屉保存同一条 update 链路
 * （保存 → 未生效 → 同步生效）。不给「重启」按钮：控制面没有单对象 reload 动作，
 * 摆一个点了只能弹「模拟」的实现比不摆更糟。
 */
async function onToggleEnabled(o: V41Object, next: boolean) {
  try {
    await updateObject(props.kind, o.id, { ...(o.spec ?? {}), enabled: next })
    messageApi.success(next ? `已启用 ${o.key || o.id}` : `已停用 ${o.key || o.id}，同步后生效`)
    invalidateObjectOptions()
    await reload()
  } catch (e) {
    messageApi.error(e instanceof Error ? e.message : String(e))
  }
}

/** 支持 enabled 布尔字段的 kind 才给启停按钮（组件/插件用的是 state，不是 enabled）。 */
const TOGGLE_KINDS = new Set(['route', 'service', 'middleware', 'entrypoint', 'log', 'variable'])

const rowOps = computed(() => {
  const extra = props.rowOps
  // 编辑放最前（原型 .ops 的第一个动作）：点整行也能编辑，但行内「编辑」图标
  // 让用户明确知道「这行能点开改」，而不是只能猜着点名字。
  const edit =
    !props.readonly
      ? (o: V41Object): OpSpec[] => [
          { tip: '编辑', icon: EditOutlined, color: OP_COLOR.edit, onClick: () => openEdit(o) },
        ]
      : undefined
  const toggle =
    !props.readonly && TOGGLE_KINDS.has(props.kind)
      ? (o: V41Object): OpSpec[] => {
          const on = Boolean(o.spec?.enabled)
          return [
            on
              ? {
                  tip: '停用',
                  icon: MinusOutlined,
                  color: OP_COLOR.pause,
                  onClick: () => onToggleEnabled(o, false),
                }
              : {
                  tip: '启用',
                  icon: CheckOutlined,
                  color: OP_COLOR.start,
                  onClick: () => onToggleEnabled(o, true),
                },
          ]
        }
      : undefined
  const all = [edit, toggle, extra].filter(Boolean) as ((o: V41Object) => OpSpec[])[]
  if (!all.length) return undefined
  return (o: V41Object) => all.flatMap((f) => f(o))
})

async function submitRename() {
  if (!editing.value) return
  const next = renameValue.value.trim()
  if (!next || next === editing.value.key) {
    renameOpen.value = false
    return
  }
  renameBusy.value = true
  try {
    await renameObject(props.kind, editing.value.id, next)
    messageApi.success('已改名，引用已级联更新')
    invalidateObjectOptions()
    renameOpen.value = false
    editorOpen.value = false
    await reload()
  } catch (e) {
    messageApi.error(e instanceof Error ? e.message : String(e))
  } finally {
    renameBusy.value = false
  }
}

/** 删除：被引用时后端 409，把引用方列出来让用户决定是否连带删。 */
async function onRemove(o: V41Object) {
  const name = o.key || o.id
  Modal.confirm({
    title: `删除 ${name}？`,
    content: '删除后引用它的对象会变成悬空引用，界面会标红。',
    okText: '删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await deleteObject(props.kind, o.id)
      } catch (e) {
        // 409 IN_USE：后端把引用清单放在响应体里，直接拿来展示，
        // 不用再发一次 refs 请求。
        if (e instanceof ApiError && e.code === 'IN_USE') {
          const refs = Array.isArray(e.body?.refs) ? (e.body?.refs as ObjectRef[]) : []
          void confirmCascadeDelete(o, e.message, refs)
          return
        }
        messageApi.error(e instanceof Error ? e.message : String(e))
        throw e
      }
      messageApi.success('已删除')
      invalidateObjectOptions()
      await reload()
    },
  })
}

async function confirmCascadeDelete(o: V41Object, reason: string, known: ObjectRef[] = []) {
  let refs = known
  if (!refs.length) {
    try {
      refs = (await getObjectRefs(props.kind, o.id)).inbound
    } catch {
      /* 引用清单拿不到就只提示原因，不硬拦 */
    }
  }
  const detail = refs.length
    ? `${reason}\n\n引用它的对象：\n${refs.map((r) => `- ${r.from}/${r.id} 的 ${r.field}`).join('\n')}`
    : reason
  Modal.confirm({
    title: `仍要删除 ${o.key || o.id}？`,
    content: `${detail}\n\n连带删除会同时移除这些引用。`,
    okText: '连带删除',
    okType: 'danger',
    cancelText: '取消',
    onOk: async () => {
      try {
        await deleteObject(props.kind, o.id, true)
        messageApi.success('已连带删除')
        invalidateObjectOptions()
        await reload()
      } catch (e) {
        messageApi.error(e instanceof Error ? e.message : String(e))
        throw e
      }
    },
  })
}

async function onSync() {
  try {
    const run = await syncNow()
    messageApi.success(`已触发同步（${run.id}）`)
    await reload()
  } catch (e) {
    messageApi.error(e instanceof Error ? e.message : String(e))
  }
}

/** 导出：把当前对象册落成本地 JSON（原型组头的「导出」按钮）。 */
function onExport() {
  const payload = {
    kind: props.kind,
    exportedAt: new Date().toISOString(),
    objects: objects.value,
  }
  const blob = new Blob([JSON.stringify(payload, null, 2)], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = `${props.kind}.json`
  a.click()
  URL.revokeObjectURL(url)
}

/** 单例已有对象时不再给「新增」——后端按主键唯一，再建一条只会 409。 */
const canCreate = computed(() => !props.readonly && (!singleton.value || objects.value.length === 0))

onMounted(reload)

defineExpose({ reload })
</script>

<template>
  <div
    class="kind-page"
    :class="{ readonly }"
  >
    <contextHolder />
    <header class="kind-page-head">
      <div class="kind-page-heading">
        <h2 class="kind-page-title">
          {{ title }}
        </h2>
        <span class="kind-page-count">{{ countTag }}</span>
        <code
          v-if="modelFile"
          class="kind-page-model"
        >{{ modelFile }}</code>
      </div>
      <div class="kind-page-ops">
        <span
          v-if="syncEnabled && pending > 0"
          class="kind-page-pending"
        >
          {{ pending }} 个待生效
        </span>
        <!-- 同步的对外说法是「检查更新」（原型按钮文案）：对象册是拉取型资源，
             点一下把远端模型拉回来重新比对，名字叫「立即同步」容易让人以为
             是「立刻把本地推上去」。 -->
        <Tooltip
          v-if="syncEnabled"
          title="从模型源拉取最新定义并与本地比对（不会覆盖本地未生效的修改）"
        >
          <Button @click="onSync">检查更新</Button>
        </Tooltip>
        <Button
          v-if="!readonly"
          @click="onExport"
        >
          导出
        </Button>
        <Button
          v-if="canCreate"
          type="primary"
          class="btn-add"
          @click="openCreate"
        >
          ＋ 新增
        </Button>
      </div>
    </header>
    <p
      v-if="description"
      class="kind-page-desc"
    >
      {{ description }}
    </p>

    <Spin :spinning="loading || specLoading">
      <a-alert
        v-if="specError"
        type="error"
        show-icon
        :message="specError"
        class="kind-page-alert"
      />
      <!-- 页头统计标签（原型 .lstats）：路由页放「Caddy 版本 / 已生效 / 证书告警」。
           作用域槽把 objects 交给页，保证增删后统计跟着列表一起刷新。 -->
      <slot
        name="stats"
        :objects="objects"
      />
      <ObjectListTable
        :objects="objects"
        :loading="loading"
        :extra-columns="extraColumns"
        :extra-ops="rowOps"
        :name-render="nameRender"
        :readonly="readonly"
        :empty-text="`还没有${title}`"
        @edit="openEdit"
        @remove="onRemove"
      />
    </Spin>

    <a-modal
      v-model:open="renameOpen"
      title="改主键"
      ok-text="改名"
      cancel-text="取消"
      :confirm-loading="renameBusy"
      @ok="submitRename"
    >
      <p class="rename-hint">
        改主键会换 id，并级联改写全部引用（路由引用服务、服务被中间件引用等）。
      </p>
      <a-input
        v-model:value="renameValue"
        placeholder="新主键"
      />
    </a-modal>

    <ObjectEditorDrawer
      v-model:open="editorOpen"
      :kind="kind"
      :title="title"
      :object="editing"
      :fields="fields"
      :key-field="spec?.keyField"
      :kinds="kinds"
      :type-field="typeField"
      :fields-for-type="fieldsForType"
      :write-only-fields="writeOnlyFields"
      :saving="saving"
      :error="formError"
      @submit="onSubmit"
      @rename="onRename"
    />
  </div>
</template>

<style scoped>
.kind-page {
  display: flex;
  flex-direction: column;
  gap: var(--gb-space-md);
}
.kind-page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: var(--gb-space-md);
}
/* 列表组标题（原型 .grp>.hd .nm）：14px / 600。
   页面级标题由顶栏面包屑承担，这里再放大号 h2 就是同一个名字出现两次。 */
.kind-page-title {
  margin: 0;
  font-size: var(--gb-font-base);
  font-weight: 600;
}
.kind-page-heading {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
  flex-wrap: wrap;
}
/* 原型的「N 项 / 单例 / 只读」计数标签 */
.kind-page-count {
  font-size: var(--gb-font-xs);
  color: var(--gb-color-text-secondary);
  border: 1px solid var(--gb-color-border-secondary);
  border-radius: var(--gb-radius-sm);
  padding: 0 var(--gb-space-xs);
  line-height: 18px;
}
/* 原型的模型文件标签 model/xxx.yaml */
.kind-page-model {
  font-size: var(--gb-font-xs);
  color: var(--gb-color-text-tertiary);
  background: var(--gb-color-bg-layout);
  border-radius: var(--gb-radius-sm);
  padding: 0 var(--gb-space-xs);
  line-height: 18px;
}
.kind-page-desc {
  margin: calc(-1 * var(--gb-space-xs)) 0 0;
  color: var(--gb-color-text-secondary);
  font-size: var(--gb-font-sm);
  max-width: 640px;
}
.kind-page-ops {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
}
.kind-page-pending {
  color: var(--gb-state-pending);
  font-size: var(--gb-font-sm);
}
.kind-page-alert {
  margin-bottom: var(--gb-space-sm);
}
.rename-hint {
  margin: 0 0 var(--gb-space-sm);
  color: var(--gb-color-text-secondary);
  font-size: var(--gb-font-sm);
}
</style>
