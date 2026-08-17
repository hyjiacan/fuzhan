<template>
  <Toast
    :message="message"
    :type="type"
    :suggestion="suggestion"
    :retryable="retryable"
    :show-retry="showRetry"
    @close="onClose"
    @retry="onRetry"
  />
</template>

<script setup>
import Toast from './Toast.vue'
import { useToast } from '../composables/useToast.js'

const { message, type, suggestion, retryable, showRetry } = useToast()

const emit = defineEmits(['close', 'retry'])

const onClose = () => emit('close')
const onRetry = () => emit('retry')
</script>