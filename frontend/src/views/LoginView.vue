<script setup lang="ts">
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import { Card, Form, FormItem, Input, Button, message } from 'ant-design-vue'
import { login } from '../api/auth'

const router = useRouter()
const [messageApi, contextHolder] = message.useMessage()
const password = ref('')
const busy = ref(false)

async function submit() {
  if (!password.value) {
    messageApi.warning('请输入口令')
    return
  }
  busy.value = true
  try {
    await login(password.value)
    messageApi.success('已登录')
    router.replace('/dashboard')
  } catch (e) {
    messageApi.error((e as Error).message)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <contextHolder />
  <div class="login-wrap">
    <Card title="GateBox 登录" :style="{ width: '360px' }">
      <Form layout="vertical" @finish="submit">
        <FormItem label="管理员口令">
          <Input.Password v-model:value="password" placeholder="请输入口令" @press-enter="submit" />
        </FormItem>
        <Button type="primary" block :loading="busy" @click="submit">登录</Button>
      </Form>
    </Card>
  </div>
</template>

<style scoped>
.login-wrap {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: #f5f5f5;
}
</style>
