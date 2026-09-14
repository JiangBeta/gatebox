<script setup lang="ts">
import { ref, computed, watch, h } from 'vue'
import {
  Drawer, Tabs, Button, Table, Input, Select, Space,
  Typography, Tag, Alert, Popconfirm, InputNumber, Switch, message,
} from 'ant-design-vue'
import {
  listRegistries, createRegistry, updateRegistry, deleteRegistry,
  getDaemon, updateDaemon,
  type RegistryView, type DaemonView,
} from '../api/docker'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{ 'update:show': (v: boolean) => void }>()
const show = computed({ get: () => props.show, set: (v) => emit('update:show', v) })

const [messageApi, contextHolder] = message.useMessage()

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
    messageApi.error('读取仓库列表失败 — ' + e.message)
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
    messageApi.error('读取 daemon 配置失败 — ' + e.message)
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
    messageApi.warning('请填写名称与地址')
    return
  }
  formBusy.value = true
  try {
    if (editing.value) {
      await updateRegistry(editing.value.id, { ...form.value })
      messageApi.success('仓库已更新')
    } else {
      await createRegistry({ ...form.value })
      messageApi.success('仓库已添加')
    }
    formShow.value = false
    await loadRegistries()
  } catch (e: any) {
    messageApi.error('保存失败 — ' + e.message)
  } finally {
    formBusy.value = false
  }
}

async function doDelete(id: string) {
  try {
    await deleteRegistry(id)
    messageApi.success('仓库已删除')
    await loadRegistries()
  } catch (e: any) {
    messageApi.error('删除失败 — ' + e.message)
  }
}

const regColumns = [
  { title: '名称', key: 'name', dataIndex: 'name' },
  { title: '地址', key: 'url', dataIndex: 'url' },
  {
    title: '协议',
    key: 'scheme',
    dataIndex: 'scheme',
    width: 80,
    customRender: ({ record }: { record: RegistryView }) => h(Tag, { size: 'small' }, { default: () => record.scheme }),
  },
  { title: '用户名', key: 'username', width: 120, customRender: ({ record }: { record: RegistryView }) => record.username || '-' },
  {
    title: '密码',
    key: 'secret',
    width: 90,
    customRender: ({ record }: { record: RegistryView }) =>
      record.hasSecret
        ? h(Tag, { size: 'small', color: 'success' }, { default: () => '已设置' })
        : h(Typography.Text, { type: 'secondary' }, { default: () => '—' }),
  },
  {
    title: '操作',
    key: 'actions',
    width: 130,
    customRender: ({ record }: { record: RegistryView }) =>
      h(Space, { size: 4 }, {
        default: () => [
          h(Button, { size: 'small', onClick: () => openEdit(record) }, { default: () => '编辑' }),
          h(
            Popconfirm,
            { title: '确认删除该仓库?', onConfirm: () => doDelete(record.id) },
            {
              default: () => h(Button, { size: 'small', danger: true }, { default: () => '删除' }),
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
    messageApi.success('已保存并热重载')
    await loadDaemon()
  } catch (e: any) {
    messageApi.error('保存失败 — ' + e.message)
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
  <contextHolder />
  <Drawer
    :open="show"
    placement="right"
    :width="760"
    @close="show = false"
  >
    <template #title>
      <span class="dw-drawer-title">仓库管理</span>
    </template>
    <Tabs>
      <TabPane key="registries" tab="私有仓库">
        <div style="display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px">
          <Typography.Text type="secondary" style="font-size: 12px">
            凭证加密存储,拉取时经 X-Registry-Auth 传递,不写入 ~/.docker/config.json
          </Typography.Text>
          <Button size="small" type="primary" @click="openAdd">+ 添加仓库</Button>
        </div>

        <div v-if="formShow" style="margin-bottom: 12px; padding: 12px; border: 1px dashed #aaa; border-radius: 6px; display: flex; flex-direction: column; gap: 10px">
          <div style="display: flex; gap: 12px">
            <div style="flex: 1">
              <div class="dw-label">名称</div>
              <Input v-model:value="form.name" placeholder="我的私有仓库" />
            </div>
            <div style="flex: 2">
              <div class="dw-label">地址（不含协议）</div>
              <Input v-model:value="form.url" placeholder="registry.example.com" />
            </div>
            <div style="width: 90px">
              <div class="dw-label">协议</div>
              <Select v-model:value="form.scheme" :options="schemeOptions" />
            </div>
          </div>
          <div style="display: flex; gap: 12px">
            <div style="flex: 1">
              <div class="dw-label">用户名</div>
              <Input v-model:value="form.username" placeholder="可选" />
            </div>
            <div style="flex: 1">
              <div class="dw-label">密码{{ editing ? '（留空则不修改）' : '' }}</div>
              <Input v-model:value="form.secret" type="password" placeholder="可选" />
            </div>
          </div>
          <Space style="justify-content: flex-end">
            <Button size="small" @click="formShow = false">取消</Button>
            <Button size="small" type="primary" :loading="formBusy" @click="saveForm">保存</Button>
          </Space>
        </div>

        <Table
          :columns="regColumns"
          :data-source="registries"
          :loading="regLoading"
          :row-key="(row: RegistryView) => row.id"
          size="small"
        />
      </TabPane>

      <TabPane key="daemon" tab="daemon 配置">
        <Alert v-if="daemon && !daemon.writable" type="warning" :show-icon="true" style="margin-bottom: 14px">
          {{ daemon.readOnlyReason }}
        </Alert>

        <div v-if="daemon" style="display: flex; flex-direction: column; gap: 14px">
          <div>
            <div class="dw-section">镜像加速</div>
            <div class="dw-label">镜像加速器（一行一个）</div>
            <Input.TextArea
              v-model:value="mirrorsText"
              :auto-size="{ minRows: 2, maxRows: 4 }"
              :disabled="!daemon.writable"
              placeholder="https://mirror.example.com"
            />
            <div class="dw-desc" style="margin-top: 4px">配置 Docker 镜像加速镜像源，重启后生效。</div>
          </div>
          <div class="dw-divider" />
          <div>
            <div class="dw-section">安全</div>
            <div class="dw-label">不安全仓库（一行一个，用于 http 内网 registry）</div>
            <Input.TextArea
              v-model:value="insecureText"
              :auto-size="{ minRows: 2, maxRows: 4 }"
              :disabled="!daemon.writable"
              placeholder="192.168.1.100:5000"
            />
          </div>
          <div class="dw-divider" />
          <div>
            <div class="dw-section">运行参数</div>
            <div style="display: flex; align-items: center; gap: 24px">
              <div style="display: flex; align-items: center; gap: 8px">
                <span class="dw-label" style="margin: 0">最大并发下载数</span>
                <InputNumber
                  v-model:value="daemon.maxConcurrentDownloads"
                  :disabled="!daemon.writable"
                  :min="0"
                  style="width: 100px"
                />
              </div>
              <div style="display: flex; align-items: center; gap: 8px">
                <span class="dw-label" style="margin: 0">live-restore</span>
                <Switch :checked="daemon.liveRestore" disabled />
              </div>
            </div>
          </div>

          <div v-if="daemon.writable" style="display: flex; justify-content: flex-end">
            <Button type="primary" :loading="daemonBusy" @click="saveDaemon">保存并热重载</Button>
          </div>
          <Typography.Text v-if="daemon.writable" type="secondary" style="font-size: 12px">
            仅修改可热重载的字段,写盘后向 dockerd 发 SIGHUP,不重启进程、不影响运行中容器。
          </Typography.Text>
        </div>
      </TabPane>
    </Tabs>
  </Drawer>
</template>
