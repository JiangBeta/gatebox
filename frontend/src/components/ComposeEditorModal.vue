<script setup lang="ts">
import { ref, watch, nextTick, onBeforeUnmount, computed } from 'vue'
import {
  NModal, NInput, NButton, NSpace, NAlert, NText, NTag, useMessage,
} from 'naive-ui'
import {
  getCompose, createCompose, saveCompose, validateCompose, wsURL,
  type DeployProgress,
} from '../api/docker'

const props = defineProps<{ show: boolean; project: string | null; readOnly?: boolean }>()
const emit = defineEmits<{ 'update:show': (v: boolean) => void; saved: () => void }>()

const message = useMessage()

const projectName = ref('')
const displayName = ref('')
const yamlContent = ref('')
const loading = ref(false)
const saving = ref(false)
const validating = ref(false)
const validateError = ref('')

// 部署状态
const deploying = ref(false)
const deployLines = ref<DeployProgress[]>([])
const deployError = ref('')
const deployDone = ref(false)
let deploySocket: WebSocket | null = null

const isNew = computed(() => !props.project)
const editable = computed(() => !props.readOnly)

const editorEl = ref<HTMLElement | null>(null)
let editorView: any = null
let cmModule: any = null

function defaultTemplate(): string {
  return `services:
  web:
    image: nginx:alpine
    container_name: my-web
    restart: unless-stopped
    ports:
      - "8080:80"
`
}

async function loadCodeMirror() {
  if (cmModule) return cmModule
  const [cm, langYaml, state] = await Promise.all([
    import('codemirror'),
    import('@codemirror/lang-yaml'),
    import('@codemirror/state'),
  ])
  cmModule = { cm, langYaml, state }
  return cmModule
}

async function ensureEditor() {
  if (editorView || !editorEl.value) return
  const { cm, langYaml, state } = await loadCodeMirror()
  editorView = new cm.EditorView({
    doc: yamlContent.value,
    extensions: [
      cm.basicSetup,
      langYaml.yaml(),
      state.EditorState.tabSize.of(2),
      cm.EditorView.editable.of(editable.value),
      cm.EditorView.lineWrapping,
    ],
    parent: editorEl.value,
  })
}

function destroyEditor() {
  editorView?.destroy()
  editorView = null
}

function currentYAML(): string {
  return editorView ? editorView.state.doc.toString() : yamlContent.value
}

watch(() => props.show, async (v) => {
  if (v) {
    loading.value = true
    validateError.value = ''
    deployLines.value = []
    deployError.value = ''
    deployDone.value = false
    projectName.value = props.project || ''
    displayName.value = ''
    try {
      if (props.project) {
        const d = await getCompose(props.project)
        displayName.value = d.displayName
        yamlContent.value = d.yaml
      } else {
        yamlContent.value = defaultTemplate()
      }
    } catch (e: any) {
      message.error('读取配置失败 — ' + e.message)
    } finally {
      loading.value = false
    }
    await nextTick()
    await ensureEditor()
  } else {
    closeDeploy()
    destroyEditor()
  }
})

async function doValidate() {
  const project = projectName.value.trim() || props.project || ''
  if (!project) {
    message.warning('请填写 projectName')
    return
  }
  validating.value = true
  validateError.value = ''
  try {
    await validateCompose({ project, yaml: currentYAML() })
    message.success('校验通过')
  } catch (e: any) {
    validateError.value = e.message
    message.error('校验失败')
  } finally {
    validating.value = false
  }
}

/** 保存:新建走 create,编辑走 save。返回实际 projectName,失败返回 null。 */
async function doSave(): Promise<string | null> {
  const yaml = currentYAML()
  if (!yaml.trim()) {
    message.warning('YAML 不能为空')
    return null
  }
  saving.value = true
  try {
    if (isNew.value) {
      const p = projectName.value.trim()
      if (!p) {
        message.warning('请填写 projectName')
        return null
      }
      await createCompose(p, { displayName: displayName.value, yaml })
      projectName.value = p
    } else {
      await saveCompose(props.project!, { displayName: displayName.value, yaml })
    }
    return projectName.value
  } catch (e: any) {
    validateError.value = e.message
    message.error('保存失败 — ' + e.message)
    return null
  } finally {
    saving.value = false
  }
}

async function doSaveAndDeploy() {
  const project = await doSave()
  if (project) startDeploy(project)
}

function startDeploy(project: string) {
  deploying.value = true
  deployDone.value = false
  deployError.value = ''
  deployLines.value = []

  deploySocket = new WebSocket(wsURL(`/docker/compose/${project}/deploy`))
  deploySocket.onmessage = (ev) => {
    const p: DeployProgress = JSON.parse(ev.data)
    if (p.error) {
      deployError.value = p.error
      deploying.value = false
      return
    }
    deployLines.value.push(p)
    if (p.done) {
      deployDone.value = true
      deploying.value = false
      message.success('部署完成')
      emit('saved')
    }
  }
  deploySocket.onerror = () => {
    deployError.value = '部署连接失败'
    deploying.value = false
  }
}

function closeDeploy() {
  if (deploySocket) {
    deploySocket.onmessage = null
    deploySocket.close()
    deploySocket = null
  }
  deploying.value = false
}

onBeforeUnmount(() => {
  closeDeploy()
  destroyEditor()
})
</script>

<template>
  <n-modal
    :show="show"
    preset="card"
    :title="isNew ? '创建应用' : `编辑 ${displayName || projectName}`"
    style="width: 820px"
    :mask-closable="!deploying"
    @update:show="(v) => emit('update:show', v)"
  >
    <div style="display: flex; flex-direction: column; gap: 12px">
      <div style="display: flex; gap: 12px">
        <div :style="isNew ? 'flex: 1' : 'flex: 1'">
          <n-text depth="3" style="font-size: 12px">projectName（{{ isNew ? '创建后不可改' : '只读' }}）</n-text>
          <n-input v-model:value="projectName" :disabled="!isNew" placeholder="my-app" />
        </div>
        <div style="flex: 1">
          <n-text depth="3" style="font-size: 12px">展示名</n-text>
          <n-input v-model:value="displayName" :disabled="readOnly" placeholder="家庭影院" />
        </div>
      </div>

      <n-alert v-if="validateError" type="error" :show-icon="true" style="margin-bottom: 0">
        {{ validateError }}
      </n-alert>

      <n-alert v-if="readOnly" type="info" :show-icon="true">
        外部编排只读。编辑需在源文件中进行或先「接管」。
      </n-alert>

      <div
        ref="editorEl"
        style="height: 420px; border: 1px solid #e0e0e0; border-radius: 4px; overflow: auto"
      />

      <div v-if="deploying || deployDone || deployError" style="max-height: 180px; overflow: auto; font-size: 12px; font-family: monospace; background: #f7f7f7; border-radius: 4px; padding: 8px">
        <div v-for="(l, i) in deployLines" :key="i" style="line-height: 1.6">
          <n-tag size="tiny" :type="l.status === 'Done' ? 'success' : l.status === 'Error' ? 'error' : 'default'" :bordered="false" style="margin-right: 6px">
            {{ l.status }}
          </n-tag>
          <span>{{ l.id }}</span>
          <span style="color: #888; margin-left: 6px">{{ l.text }}</span>
        </div>
        <n-text v-if="deployError" type="error" style="font-size: 12px">{{ deployError }}</n-text>
        <n-text v-if="deployDone" type="success" style="font-size: 12px">✓ 部署完成</n-text>
      </div>
    </div>

    <template #footer>
      <n-space justify="end">
        <template v-if="editable">
          <n-button size="small" :loading="validating" @click="doValidate">校验</n-button>
          <n-button size="small" :loading="saving" @click="doSave">保存</n-button>
          <n-button size="small" type="primary" :loading="saving || deploying" :disabled="deploying" @click="doSaveAndDeploy">
            保存并部署
          </n-button>
        </template>
        <n-button size="small" @click="emit('update:show', false)">关闭</n-button>
      </n-space>
    </template>
  </n-modal>
</template>
