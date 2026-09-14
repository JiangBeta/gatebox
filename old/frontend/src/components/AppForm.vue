<script setup lang="ts">
import { Form } from 'ant-design-vue'
import type { FormProps } from 'ant-design-vue'

interface Props {
  layout?: 'horizontal' | 'vertical' | 'inline'
  labelCol?: { span?: number; offset?: number }
  wrapperCol?: { span?: number; offset?: number }
  labelAlign?: 'left' | 'right'
  requiredMark?: boolean | 'optional'
  colon?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  layout: 'horizontal',
  labelCol: () => ({ span: 6 }),
  wrapperCol: () => ({ span: 18 }),
  labelAlign: 'right',
  requiredMark: true,
  colon: true,
})

const emit = defineEmits<{
  (e: 'finish', values: any): void
  (e: 'finishFailed', errorInfo: any): void
}>()

const [form] = Form.useForm()

function handleFinish(values: any) {
  emit('finish', values)
}

function handleFinishFailed(errorInfo: any) {
  emit('finishFailed', errorInfo)
}

function resetFields() {
  form.resetFields()
}

function setFieldsValue(values: any) {
  form.setFieldsValue(values)
}

function getFieldsValue() {
  return form.getFieldsValue()
}

defineExpose({
  form,
  resetFields,
  setFieldsValue,
  getFieldsValue,
})
</script>

<template>
  <Form
    :form="form"
    :layout="layout"
    :label-col="labelCol"
    :wrapper-col="wrapperCol"
    :label-align="labelAlign"
    :required-mark="requiredMark"
    :colon="colon"
    @finish="handleFinish"
    @finish-failed="handleFinishFailed"
  >
    <slot />
  </Form>
</template>
