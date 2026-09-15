<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import {
  Alert, Button, Card, Form, FormItem, Input, InputNumber, Select, Space, Switch, message,
} from 'ant-design-vue'
import { ReloadOutlined, RestOutlined, SaveOutlined } from '@ant-design/icons-vue'
import { getSettings, saveSettings, type MosdnsSettings } from '../../../api/mosdns'
import { restartComponent } from '../../../api/components'

const [messageApi, contextHolder] = message.useMessage()
const loading = ref(false)
const saving = ref(false)

const form = reactive<MosdnsSettings>({
  listen: ':5335',
  logLevel: 'info',
  localDns: [],
  remoteDns: [],
  cache: true,
  cacheSize: 10240,
})

const logLevelOptions = [
  { label: 'Debug', value: 'debug' },
  { label: 'Info', value: 'info' },
  { label: 'Warn', value: 'warn' },
  { label: 'Error', value: 'error' },
]

async function load() {
  loading.value = true
  try {
    Object.assign(form, await getSettings())
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    loading.value = false
  }
}

async function save(restart: boolean) {
  saving.value = true
  try {
    const saved = await saveSettings({ ...form })
    Object.assign(form, saved)
    if (restart) {
      await restartComponent('mosdns')
      messageApi.success('已保存并重启 mosdns')
    } else {
      messageApi.success('已保存（重启后生效）')
    }
    window.dispatchEvent(new Event('gatebox:components-changed'))
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    saving.value = false
  }
}

onMounted(load)
</script>

<template>
  <contextHolder />
  <Card title="基础设置" size="small">
    <Alert
      type="info"
      show-icon
      message="保存将按上方表单重新生成 config.yaml，会覆盖「配置文件」页的手工改动"
      style="margin-bottom: 16px"
    />
    <Form layout="vertical" :disabled="loading" style="max-width: 720px">
      <FormItem label="DNS 监听地址" help="格式 host:port 或 :port，例如 :5335 监听所有网卡的 UDP/TCP 5335">
        <Input v-model:value="form.listen" placeholder=":5335" />
      </FormItem>
      <FormItem label="日志级别">
        <Select v-model:value="form.logLevel" :options="logLevelOptions" style="width: 200px" />
      </FormItem>
      <FormItem label="本地 / 国内上游 DNS" help="回车添加；支持 8.8.8.8、tls://、https://、quic:// 等格式">
        <Select
          v-model:value="form.localDns"
          mode="tags"
          placeholder="如 223.5.5.5、119.29.29.29"
          :token-separators="[',', ' ']"
        />
      </FormItem>
      <FormItem label="远程 / 国外上游 DNS" help="当本地上游无响应时启用（fallback 备用）">
        <Select
          v-model:value="form.remoteDns"
          mode="tags"
          placeholder="如 tls://8.8.8.8"
          :token-separators="[',', ' ']"
        />
      </FormItem>
      <FormItem label="DNS 缓存">
        <Space>
          <Switch v-model:checked="form.cache" />
          <InputNumber
            v-if="form.cache"
            v-model:value="form.cacheSize"
            :min="1"
            :step="1024"
            addon-after="条"
          />
        </Space>
      </FormItem>
    </Form>

    <Space>
      <Button type="primary" :loading="saving" @click="save(true)">
        <template #icon><SaveOutlined /></template>
        保存并重启
      </Button>
      <Button :loading="saving" @click="save(false)">
        <template #icon><SaveOutlined /></template>
        仅保存
      </Button>
      <Button :loading="loading" @click="load">
        <template #icon><ReloadOutlined /></template>
        重新加载
      </Button>
      <Button @click="Object.assign(form, { listen: ':5335', logLevel: 'info', localDns: ['223.5.5.5', '119.29.29.29'], remoteDns: ['tls://8.8.8.8', 'tls://1.1.1.1'], cache: true, cacheSize: 10240 })">
        <template #icon><RestOutlined /></template>
        恢复默认
      </Button>
    </Space>
  </Card>
</template>
