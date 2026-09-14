<script setup lang="ts">
import { Drawer } from 'ant-design-vue'

interface Props {
  visible: boolean
  title?: string
  width?: string | number
  placement?: 'left' | 'right' | 'top' | 'bottom'
  maskClosable?: boolean
  destroyOnClose?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  title: '',
  width: '520px',
  placement: 'right',
  maskClosable: true,
  destroyOnClose: true,
})

const emit = defineEmits<{
  (e: 'update:visible', value: boolean): void
  (e: 'close'): void
}>()

function handleClose() {
  emit('update:visible', false)
  emit('close')
}
</script>

<template>
  <Drawer
    :open="visible"
    :title="title"
    :width="width"
    :placement="placement"
    :mask-closable="maskClosable"
    :destroy-on-close="destroyOnClose"
    @close="handleClose"
  >
    <slot />
    <template #footer>
      <slot name="footer" />
    </template>
  </Drawer>
</template>
