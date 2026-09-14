<script setup lang="ts">
import { Button } from 'ant-design-vue'

interface Props {
  type?: 'primary' | 'default' | 'dashed' | 'text' | 'link'
  danger?: boolean
  ghost?: boolean
  block?: boolean
  loading?: boolean
  disabled?: boolean
  size?: 'small' | 'middle' | 'large'
  shape?: 'default' | 'circle' | 'round'
  icon?: any
}

const props = withDefaults(defineProps<Props>(), {
  type: 'default',
  danger: false,
  ghost: false,
  block: false,
  loading: false,
  disabled: false,
  size: 'middle',
  shape: 'default',
})

const emit = defineEmits<{
  (e: 'click', event: MouseEvent): void
}>()

function handleClick(event: MouseEvent) {
  if (!props.disabled && !props.loading) {
    emit('click', event)
  }
}
</script>

<template>
  <Button
    :type="type"
    :danger="danger"
    :ghost="ghost"
    :block="block"
    :loading="loading"
    :disabled="disabled"
    :size="size"
    :shape="shape"
    @click="handleClick"
  >
    <template v-if="icon" #icon>
      <component :is="icon" />
    </template>
    <slot />
  </Button>
</template>
