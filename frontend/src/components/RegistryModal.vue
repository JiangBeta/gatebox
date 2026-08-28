<script setup lang="ts">
import { ref, computed, watch, h } from 'vue'
import {
  NDrawer, NTabs, NTabPane, NButton, NDataTable, NInput, NSelect, NSpace,
  NText, NTag, NAlert, NPopconfirm, NInputNumber, NSwitch, useMessage,
} from 'naive-ui'
import {
  listRegistries, createRegistry, updateRegistry, deleteRegistry,
  getDaemon, updateDaemon,
  type RegistryView, type DaemonView,
} from '../api/docker'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': (v: boolean) => void }>()
const show = computed({ get: () => props.show, set: (v) => emit('update:show', v) })

const message = useMessage()

// --- 私有仓库 ---

const registries = ref<RegistryView[]>([])
const regLoading = ref(false)
const formShow = ref(false)
const editing = ref<RegistryView | null>(null)
const formBusy = ref(false)
const form = ref({ name: '', url: '', scheme: 'https', username: '', secret: '' })

const schemeOptions = [
  { label: 'https', value: 'https' },
  { label: 'http', value: 'http' },
]

// --- daemon 配置 ---

const daemon = ref<DaemonView | null>(null)
const daemonBusy = ref(false)
const mirrorsText = ref('')
const insecureText = ref('')

async function loadRegistries() {
  regLoading.value = true
  try {
    registries.value = await listRegistries()
  } catch (e: any) {
    message.error('读取仓库列表失败 — ' + e.message)
  } finally {
    regLoading.value = false
  }
}

async function loadDaemon() {
  try {
    daemon.value = await getDaemon()
    mirrorsText.value = (daemon.value.mirrors || []).join('\n')
    insecureText.value = (daemon.value.insecureRegistries || []).join('\n')
  } catch (e: any) {
    message.error('读取 daemon 配置失败 — ' + e.message)
  }
}

function openAdd() {
  editing.value = null
  form.value = { name: '', url: '', scheme: 'https', username: '', secret: '' }
  formShow.value = true
}

function openEdit(r: RegistryView) {
  editing.value = r
  form.value = { name: r.name, url: r.url, scheme: r.scheme, username: r.username, secret: '' }
  formShow.value = true
}

async function saveForm() {
  if (!form.value.name.trim() || !form.value.url.trim()) {
    message.warning('请填写名称与地址')
    return
  }
  formBusy.value = true
  try {
    if (editing.value) {
      await updateRegistry(editing.value.id, { ...form.value })
      message.success('仓库已更新')
    } else {
      await createRegistry({ ...form.value })
      message.success('仓库已添加')
    }
    formShow.value = false
    await loadRegistries()
  } catch (e: any) {
    message.error('保存失败 — ' + e.message)
  } finally {
    formBusy.value = false
  }
}

async function doDelete(id: string) {
  try {
    await deleteRegistry(id)
    message.success('仓库已删除')
    await loadRegistries()
  } catch (e: any) {
    message.error('删除失败 — ' + e.message)
  }
}

const regColumns = [
  { title: '名称', key: 'name' },
  { title: '地址', key: 'url' },
  {
    title: '协议',
    key: 'scheme',
    width: 80,
    render: (row: RegistryView) => h(NTag, { size: 'small', bordered: false }, { default: () => row.scheme }),
  },
  { title: '用户名', key: 'username', width: 120, render: (row: RegistryView) => row.username || '-' },
  {
    title: '密码',
    key: 'secret',
    width: 90,
    render: (row: RegistryView) =>
      row.hasSecret
        ? h(NTag, { size: 'small', type: 'success', bordered: false }, { default: () => '已设置' })
        : h(NText, { depth: 3 }, { default: () => '—' }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 130,
    render: (row: RegistryView) =>
      h(NSpace, { size: 4 }, {
        default: () => [
          h(NButton, { size: 'tiny', onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(
            NPopconfirm,
            { onPositiveClick: () => doDelete(row.id) },
            {
              trigger: () => h(NButton, { size: 'tiny', type: 'error', ghost: true }, { default: () => '删除' }),
              default: () => '确认删除该仓库?',
            },
          ),
        ],
      }),
  },
]

async function saveDaemon() {
  const mirrors = mirrorsText.value.split('\n').map((s) => s.trim()).filter(Boolean)
  const insecure = insecureText.value.split('\n').map((s) => s.trim()).filter(Boolean)
  daemonBusy.value = true
  try {
    await updateDaemon({
      mirrors,
      insecureRegistries: insecure,
      maxConcurrentDownloads: daemon.value?.maxConcurrentDownloads || undefined,
    })
    message.success('已保存并热重载')
    await loadDaemon()
  } catch (e: any) {
    message.error('保存失败 — ' + e.message)
  } finally {
    daemonBusy.value = false
  }
}

watch(show, (v) => {
  if (v) {
    loadRegistries()
    loadDaemon()
  }
})
</script>

<template>
  <n-drawer
    v-model:show="show"
    placement="right"
    width="min(760px, 100vw)"
  >
    <div style="display: flex; flex-direction: column; height: 100%">
      <div style="padding: 14px 24px; border-bottom: 1px solid #eee; font-size: 16px; font-weight: 600; display: flex; align-items: center; justify-content: space-between; flex-shrink: 0">
        <span>仓库管理</span>
        <n-button quaternary circle size="small" @click="show = false">✕</n-button>
      </div>
      <div style="flex: 1; overflow: auto; padding: 16px 24px">
        <n-tabs type="line" default-value="registries">
      <n-tab-pane name="registries" tab="私有仓库">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px">
          <n-text depth="3" style="font-size: 12px">
            凭证加密存储,拉取时经 X-Registry-Auth 传递,不写入 ~/.docker/config.json
          </n-text>
          <n-button size="small" type="primary" @click="openAdd">+ 添加仓库</n-button>
        </div>

        <div v-if="formShow" style="margin-bottom: 12px; padding: 12px; border: 1px dashed #aaa; border-radius: 6px; display: flex; flex-direction: column; gap: 10px">
          <div style="display: flex; gap: 12px">
            <div style="flex: 1">
              <n-text depth="3" style="font-size: 12px">名称</n-text>
              <n-input v-model:value="form.name" placeholder="我的私有仓库" />
            </div>
            <div style="flex: 2">
              <n-text depth="3" style="font-size: 12px">地址（不含协议）</n-text>
              <n-input v-model:value="form.url" placeholder="registry.example.com" />
            </div>
            <div style="width: 90px">
              <n-text depth="3" style="font-size: 12px">协议</n-text>
              <n-select v-model:value="form.scheme" :options="schemeOptions" />
            </div>
          </div>
          <div style="display: flex; gap: 12px">
            <div style="flex: 1">
              <n-text depth="3" style="font-size: 12px">用户名</n-text>
              <n-input v-model:value="form.username" placeholder="可选" />
            </div>
            <div style="flex: 1">
              <n-text depth="3" style="font-size: 12px">
                密码{{ editing ? '（留空则不修改）' : '' }}
              </n-text>
              <n-input v-model:value="form.secret" type="password" show-password-on="click" placeholder="可选" />
            </div>
          </div>
          <n-space justify="end">
            <n-button size="small" type="primary" :loading="formBusy" @click="saveForm">保存</n-button>
            <n-button size="small" @click="formShow = false">取消</n-button>
          </n-space>
        </div>

        <n-data-table
          :columns="regColumns"
          :data="registries"
          :loading="regLoading"
          :row-key="(row: RegistryView) => row.id"
          size="small"
        />
      </n-tab-pane>

      <n-tab-pane name="daemon" tab="daemon 配置">
        <n-alert v-if="daemon && !daemon.writable" type="warning" :show-icon="true" style="margin-bottom: 14px">
          {{ daemon.readOnlyReason }}
        </n-alert>

        <div v-if="daemon" style="display: flex; flex-direction: column; gap: 14px">
          <div>
            <n-text depth="3" style="font-size: 12px">镜像加速器（一行一个）</n-text>
            <n-input
              v-model:value="mirrorsText"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 4 }"
              :disabled="!daemon.writable"
              placeholder="https://mirror.example.com"
            />
          </div>
          <div>
            <n-text depth="3" style="font-size: 12px">不安全仓库（一行一个，用于 http 内网 registry）</n-text>
            <n-input
              v-model:value="insecureText"
              type="textarea"
              :autosize="{ minRows: 2, maxRows: 4 }"
              :disabled="!daemon.writable"
              placeholder="192.168.1.100:5000"
            />
          </div>
          <div style="display: flex; align-items: center; gap: 24px">
            <div style="display: flex; align-items: center; gap: 8px">
              <span>最大并发下载数</span>
              <n-input-number
                v-model:value="daemon.maxConcurrentDownloads"
                :disabled="!daemon.writable"
                :min="0"
                style="width: 100px"
              />
            </div>
            <div style="display: flex; align-items: center; gap: 8px">
              <span>live-restore</span>
              <n-switch :value="daemon.liveRestore" disabled />
            </div>
          </div>

          <div v-if="daemon.writable" style="display: flex; justify-content: flex-end">
            <n-button type="primary" size="small" :loading="daemonBusy" @click="saveDaemon">保存并热重载</n-button>
          </div>
          <n-text v-if="daemon.writable" depth="3" style="font-size: 12px">
            仅修改可热重载的字段,写盘后向 dockerd 发 SIGHUP,不重启进程、不影响运行中容器。
          </n-text>
        </div>
      </n-tab-pane>
    </n-tabs>
      </div>
    </div>
  </n-drawer>
</template>
