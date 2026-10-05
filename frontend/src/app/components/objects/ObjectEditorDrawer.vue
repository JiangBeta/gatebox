<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Drawer, Alert, Button, Space, Tooltip } from 'ant-design-vue'
import {
  SwapOutlined, VerticalAlignBottomOutlined,
} from '@ant-design/icons-vue'
import type { ConfigField, FieldOption } from '@/lib/schema'
import {
  OBJECT_PRESENT,
  applyYamlToFields,
  createFormValues,
  serializeFormValues,
  stringifyObjectYaml,
  yamlToFormValues,
} from '@/lib/schema'
import SchemaForm from '@/app/components/schema/SchemaForm.vue'
import CodeEditor from '@/components/CodeEditor.vue'
import { useObjectOptions } from '@/app/composables/useObjectOptions'
import type { SpecField, V41Object } from '@/api/objects'

/**
 * 对象编辑器抽屉：左表单 / 右 YAML 的双面编辑器（V4.1 全部 kind 共用）。
 *
 * 两面表达**同一个对象**（ADR-043 §5「双面同源」），唯一真相是后端返回的 spec：
 * 打开时 spec → 表单值 + YAML 文本；改动任一面后另一面跟着更新；
 * 保存时以 spec 为准序列化给后端。所以 YAML 面不是只读查看窗，可以直接改。
 *
 * 三件容易被忽略但必须做对的事：
 * 1. **主键单独处理**。改名要走后端 Rename（换 id + 级联改引用），普通 PUT 改主键
 *    会被 409 拒。所以有值的主键字段锁成只读，另给「改名」入口。
 * 2. **只写字段（password）不进 spec**，也不回显；编辑时留空 = 不改。
 *    因此 YAML 面里没有它——YAML 面必须能无损表达要提交的东西。
 * 3. **confirm 字段要二次确认**（model/user.yaml 的 confirm: true）。
 *
 * 布局可用「切换布局」在左右 / 上下之间换（原型 dr-cols.col）：
 * 窄屏或表单特别长时上下排更舒服。
 */
const props = defineProps<{
  open: boolean
  kind: string
  /** 中文标题（如「路由」）；缺省退回 kind。原型抽屉标题是「新增 · 路由」。 */
  title?: string
  /** null = 新建。 */
  object: V41Object | null
  fields: ConfigField[]
  writeOnlyFields?: SpecField[]
  /** 主键字段（后端 /specs/{kind} 的 keyField）。给了就锁只读 + 改名走 Rename。 */
  keyField?: string
  /** 后端已实现的 kind 清单：用于识别「引用目标类型还不存在」的字段。 */
  kinds?: string[]
  /** 选类型的字段键（service.type / middleware.type）。给了就按所选类型重取字段。 */
  typeField?: string
  /** 按类型取字段集；不给就固定用 fields。 */
  fieldsForType?: (type?: string) => Promise<ConfigField[]>
  loading?: boolean
  saving?: boolean
  error?: string | null
}>()

const kindTitle = computed(() => props.title || props.kind)

const emit = defineEmits<{
  (e: 'update:open', v: boolean): void
  (e: 'submit', payload: { spec: Record<string, unknown>; writeOnly: Record<string, unknown> }): void
  (e: 'rename', newKey: string): void
}>()

/** 实际渲染的字段集：选了新类型就换成该类型的字段集（类型参数按需出现）。 */
const resolved = ref<ConfigField[]>(props.fields)
watch(
  () => props.fields,
  (v) => {
    resolved.value = v
  },
)

/** 锁成只读的主键字段名（编辑态才有值；新建态必须能填，所以为空）。 */
const lockedKey = ref<string>('')
const editing = ref<Record<string, unknown>>({})
const errors = ref<string[]>([])
// 只写字段按字段键存：不要用一个共享的 password 变量 ——
// 第二个 writeOnly 字段出现时会和密码共用同一个值，把值写错地方。
const writeOnlyValues = ref<Record<string, string>>({})
const confirmValues = ref<Record<string, string>>({})

/** YAML 文本：与 editing 双向同步（两侧同源）。 */
const yamlText = ref('')
/** 用户正在 YAML 面编辑中：此时不要用表单侧的改动覆盖他手敲的内容。 */
const yamlDirty = ref(false)
/** YAML 语法错误：保存被挡住，并提示回到表单面。 */
const yamlError = ref<string | null>(null)
/** 布局方向：左右（默认，原型 dr-cols）↔ 上下（原型 dr-cols.col）。 */
const stacked = ref(false)

const isNew = computed(() => props.object === null)

// 主键字段锁成只读：改名走 Rename 接口。
const editableFields = computed(() =>
  lockedKey.value ? usableFields.value.filter((f) => f.key !== lockedKey.value) : usableFields.value,
)

// 类型变化 → 重取字段（middleware.basic_auth.users / service.file_server.root 这类
// 参数字段只在对应类型下出现）。
const activeType = computed(() =>
  props.typeField ? String(editing.value[props.typeField] ?? '') : '',
)
watch(
  activeType,
  async (t) => {
    if (!props.fieldsForType || !t) return
    try {
      resolved.value = await props.fieldsForType(t)
    } catch {
      // 取不到就退回基础字段：宁可少显示几个参数，也不要让整个抽屉打不开。
    }
  },
  { immediate: true },
)

const referenceTypes = computed(() => {
  const out: string[] = []
  for (const f of editableFields.value) {
    for (const t of f.reference?.types ?? []) if (!out.includes(t)) out.push(t)
  }
  return out
})
const { options } = useObjectOptions(referenceTypes)

// 引用字段指向的 kind 不存在时（例如 domain.credentialId → dns-credential，
// 该 kind 首批还没做），不能给一个永远空的下拉：那看着像 bug，
// 用户会以为「凭证丢了」。剔掉字段 + 在抽屉里说明原因才是真的。
const missingRefKinds = computed(() => {
  // kinds 为空 = 目录还没回来（或没传），此时**不做**过滤：
  // 把所有引用字段都当成「目标不存在」会让表单闪一下就少一片。
  const known = props.kinds ?? []
  if (!known.length) return []
  const miss = new Set<string>()
  for (const f of resolved.value) {
    for (const t of f.reference?.types ?? []) {
      if (!known.includes(t)) miss.add(t)
    }
  }
  return [...miss]
})
const usableFields = computed(() =>
  resolved.value.filter((f) => !(f.reference?.types ?? []).some((t) => missingRefKinds.value.includes(t))),
)

const writeOnlyList = computed(() => props.writeOnlyFields ?? [])
const needConfirm = (f: SpecField) => Boolean(f.confirm)

/** specToYaml：把当前表单值序列化成 YAML（只写字段不在其中，理由见文件头）。 */
function specToYaml(): string {
  const spec = serializeFormValues(usableFields.value, editing.value)
  if (!isNew.value && lockedKey.value) {
    const kv = props.object?.spec?.[lockedKey.value]
    if (kv !== undefined) spec[lockedKey.value] = kv
  }
  return stringifyObjectYaml(spec)
}

watch(
  () => [props.open, props.object, props.fields] as const,
  () => {
    if (!props.open) return
    errors.value = []
    writeOnlyValues.value = {}
    confirmValues.value = {}
    yamlError.value = null
    yamlDirty.value = false
    editing.value = createFormValues(usableFields.value, props.object?.spec ?? {})
    // 主键字段：接口给了 keyField 就用它；没给才按「哪个字段的值等于 key」反推。
    // 新建时不给 keyField —— 主键字段必须能填。
    if (props.keyField) {
      lockedKey.value = props.keyField
    } else if (props.object) {
      const hit = props.fields.find((f) => String(props.object?.spec?.[f.key] ?? '') === props.object?.key)
      lockedKey.value = hit?.key ?? props.fields[0]?.key ?? ''
    } else {
      lockedKey.value = ''
    }
    yamlText.value = specToYaml()
  },
  { immediate: true, deep: true },
)

// 表单侧改动 → 同步 YAML（除非用户正在 YAML 面敲字）。
watch(
  editing,
  () => {
    if (yamlDirty.value) return
    yamlText.value = specToYaml()
    yamlError.value = null
  },
  { deep: true },
)

/** onYamlInput 处理 YAML 面编辑：能解析就回填表单，不能解析就只记错。 */
function onYamlInput(v: string) {
  yamlText.value = v
  yamlDirty.value = true
  const got = yamlToFormValues(usableFields.value, v)
  if (!got.ok) {
    yamlError.value = got.error ?? 'YAML 语法错误'
    return
  }
  yamlError.value = null
  editing.value = got.value ?? {}
}

function onValidity(v: string[]) {
  errors.value = v
}

function submit() {
  // 只写字段的校验：新建必填 + 最小长度 + 二次确认（confirm: true）。
  const woErr: string[] = []
  for (const wf of writeOnlyList.value) {
    const v = writeOnlyValues.value[wf.key] ?? ''
    if (!v) {
      if (isNew.value && wf.required) woErr.push(`${wf.label || wf.key} 必填`)
      continue
    }
    const min = wf.minLength ?? 0
    if (min > 0 && v.length < min) woErr.push(`${wf.label || wf.key} 至少 ${min} 位`)
    if (needConfirm(wf) && (confirmValues.value[wf.key] ?? '') !== v) {
      woErr.push(`${wf.label || wf.key} 两次输入不一致`)
    }
  }
  if (woErr.length) {
    errors.value = woErr
    return
  }
  if (errors.value.length) return

  let spec: Record<string, unknown>
  if (yamlDirty.value) {
    // 用户在 YAML 面改过：以 YAML 为准（它能表达表单表达不了的结构）。
    // 但仍要补只读的主键字段——它在表单里被锁住了，YAML 面却能删。
    const got = applyYamlToFields(usableFields.value, yamlText.value)
    if (!got.ok) {
      yamlError.value = got.error
      errors.value = ['YAML 语法错误，请修正后再保存']
      return
    }
    spec = got.spec
  } else {
    spec = serializeFormValues(usableFields.value, editing.value)
  }
  if (!isNew.value && lockedKey.value) {
    const kv = props.object?.spec?.[lockedKey.value]
    if (kv !== undefined) spec[lockedKey.value] = kv
  }
  // 表单内部标记（object 的 presence）不进 spec：它是 UI 状态，不是对象数据。
  delete spec[OBJECT_PRESENT]

  const writeOnly: Record<string, unknown> = {}
  for (const wf of writeOnlyList.value) {
    const v = writeOnlyValues.value[wf.key]
    if (v) writeOnly[wf.key] = v
  }
  emit('submit', { spec, writeOnly })
}

const layoutTip = computed(() => (stacked.value ? '切换为左右布局' : '切换为上下布局'))
</script>

<template>
  <Drawer
    :open="open"
    :width="1040"
    :title="`${isNew ? '新建' : '编辑'} ${kindTitle}`"
    :destroy-on-close="true"
    class="object-editor"
    @update:open="(v: boolean) => emit('update:open', v)"
  >
    <template #title>
      <div class="editor-title">
        <span>{{ isNew ? '新增 · ' : '编辑 · ' }}{{ kindTitle }}</span>
        <Tooltip :title="layoutTip" placement="bottom">
          <Button
            size="small"
            type="text"
            :aria-label="layoutTip"
            @click="stacked = !stacked"
          >
            <!-- 上下布局 = 图标朝上（可切到左右）；左右布局 = 图标换向。
             两个条件必须互斥：之前写成 v-if / v-else-if 同一条件，第二个分支永远不渲染。 -->
            <VerticalAlignBottomOutlined v-if="stacked" />
            <SwapOutlined v-else />
          </Button>
        </Tooltip>
      </div>
    </template>

    <div
      class="editor-cols"
      :class="{ col: stacked }"
    >
      <!-- 左：表单（由对象 schema 生成） -->
      <div class="editor-left">
        <Alert
          v-if="error"
          type="error"
          show-icon
          class="editor-alert"
          :message="error"
        />

        <Alert
          v-if="missingRefKinds.length"
          type="info"
          show-icon
          class="editor-alert"
          :message="`${missingRefKinds.join('、')} 这类对象本版本还不支持，相关字段已隐藏`"
          description="迁移过的对象里可能已存了这类引用的旧值（迁移报告会列出），此处不做猜测清理。"
        />

        <a-form layout="vertical">
          <a-form-item
            v-if="!isNew"
            label="主键"
            extra="改主键会换 id 并级联改写全部引用"
          >
            <span class="editor-key">{{ object?.key }}</span>
            <Button
              size="small"
              class="editor-key-btn"
              @click="emit('rename', object?.key ?? '')"
            >
              改名
            </Button>
          </a-form-item>

          <SchemaForm
            :fields="editableFields"
            :model-value="editing"
            :reference-options="options as Record<string, FieldOption[]>"
            @update:model-value="(v: Record<string, unknown>) => (editing = v)"
            @validity="onValidity"
          />

          <!-- 只写字段：按字段键独立存值，不回显；编辑时留空 = 不改 -->
          <template
            v-for="wf in writeOnlyList"
            :key="wf.key"
          >
            <a-form-item
              :label="wf.label || wf.key"
              :required="isNew && wf.required"
              :extra="wf.description || (isNew ? '' : '留空表示不修改；只写字段不出现在右侧 YAML')"
            >
              <a-input-password
                v-model:value="writeOnlyValues[wf.key]"
                :placeholder="
                  isNew && wf.required
                    ? `必填${wf.minLength ? `，至少 ${wf.minLength} 位` : ''}`
                    : '留空表示不修改'
                "
                autocomplete="new-password"
              />
            </a-form-item>
            <a-form-item
              v-if="needConfirm(wf) && writeOnlyValues[wf.key]"
              :label="`确认${wf.label || wf.key}`"
              required
            >
              <a-input-password
                v-model:value="confirmValues[wf.key]"
                placeholder="再输入一次"
                autocomplete="new-password"
              />
            </a-form-item>
          </template>
        </a-form>
      </div>

      <div class="editor-splitter" />

      <!-- 右：YAML（同一对象的等价文本，可直接编辑） -->
      <div class="editor-right">
        <p class="editor-hint">
          YAML（与左侧表单是同一个对象，两面同步；也可直接在这里改）
        </p>
        <Alert
          v-if="yamlError"
          type="error"
          show-icon
          class="editor-alert"
          :message="yamlError"
          description="已暂停把 YAML 回填到表单：语法错误时照搬会丢掉你敲的内容。修正后自动继续。"
        />
        <CodeEditor
          :model-value="yamlText"
          language="yaml"
          height="100%"
          @update:model-value="onYamlInput"
        />
      </div>
    </div>

    <template #footer>
      <div class="editor-footer">
        <span class="editor-errors">
          <template
            v-for="e in errors"
            :key="e"
          >{{ e }}；</template>
        </span>
        <Space>
          <Button @click="emit('update:open', false)">
            取消
          </Button>
          <Button
            type="primary"
            :loading="saving"
            :disabled="errors.length > 0 || !!yamlError"
            @click="submit"
          >
            保存
          </Button>
        </Space>
      </div>
    </template>
  </Drawer>
</template>

<style scoped>
.editor-title {
  display: flex;
  align-items: center;
  gap: var(--gb-space-sm);
}
.editor-cols {
  display: flex;
  flex-direction: row;
  height: 100%;
  min-height: 0;
}
.editor-cols.col {
  flex-direction: column;
}
.editor-left {
  flex: 0 0 46%;
  padding-right: var(--gb-space-lg);
  overflow: auto;
  border-right: 1px solid var(--gb-color-border-secondary);
}
.editor-cols.col .editor-left {
  flex: 0 0 40%;
  border-right: 0;
  border-bottom: 1px solid var(--gb-color-border-secondary);
  padding-right: 0;
  padding-bottom: var(--gb-space-md);
}
.editor-splitter {
  flex: 0 0 6px;
  background: var(--gb-color-border-secondary);
  cursor: col-resize;
}
.editor-cols.col .editor-splitter {
  cursor: row-resize;
}
.editor-right {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
  padding-left: var(--gb-space-lg);
}
.editor-cols.col .editor-right {
  padding-left: 0;
  padding-top: var(--gb-space-md);
}
.editor-hint {
  margin: 0 0 var(--gb-space-sm);
  font-size: var(--gb-font-xs);
  color: var(--gb-color-text-tertiary);
}
.editor-alert {
  margin-bottom: var(--gb-space-md);
}
.editor-key {
  font-family: monospace;
  background: var(--gb-color-hover);
  padding: 2px var(--gb-space-sm);
  border-radius: var(--gb-radius-sm);
}
.editor-key-btn {
  margin-left: var(--gb-space-sm);
}
.editor-footer {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: var(--gb-space-sm);
}
.editor-errors {
  color: var(--gb-color-error);
  font-size: var(--gb-font-xs);
}
</style>

<style>
/* 抽屉本体需要让内容区撑满高度，双栏才能各自滚动。非 scoped。 */
.object-editor .ant-drawer-body {
  display: flex;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.object-editor .ant-drawer-content-wrapper {
  max-width: 96vw;
}
</style>