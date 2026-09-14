<script setup lang="ts">
import { computed } from 'vue'
import { Modal } from 'ant-design-vue'

interface Props {
  visible: boolean
  title?: string
  width?: string | number
  confirmLoading?: boolean
  maskClosable?: boolean
  destroyOnClose?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  title: '',
  width: '520px',
  confirmLoading: false,
  maskClosable: true,
  destroyOnClose: true,
})

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'confirm'): void
  (e: 'cancel'): void
}>()

const modalWidth = computed(() => {
  if (typeof props.width === 'number') {
    return `${props.width}px`
  }
  return props.width
})

function handleCancel() {
  emit('update:visible', false)
  emit('cancel')
}

function handleOk() {
  emit('confirm')
}
</script>

<template>
  <Modal
    :open="visible"
    :title="title"
    :width="modalWidth"
    :confirm-loading="confirmLoading"
    :mask-closable="maskClosable"
    :destroy-on-close="destroyOnClose"
    @cancel="handleCancel"
    @ok="handleOk"
  >
    <slot />
    <template #footer>
      <slot name="footer">
        <a-button @click="handleCancel">取消</a-button>
        <a-button type="primary" :loading="confirmLoading" @click="handleOk">
          确认
        </a-button>
      </slot>
    </template>
  </Modal>
</template>
