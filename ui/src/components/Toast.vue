<template>
  <Teleport to="body">
    <Transition name="toast">
      <div v-if="visible" :class="['toast-container', `toast-${type}`]">
        <div class="toast-icon">
          <span v-if="type === 'success'">✓</span>
          <span v-else-if="type === 'error'">✕</span>
          <span v-else-if="type === 'warning'">⚠</span>
          <span v-else-if="type === 'info'">ℹ</span>
        </div>
        <div class="toast-content">
          <div class="toast-message">{{ message }}</div>
          <div v-if="suggestion" class="toast-suggestion">{{ suggestion }}</div>
        </div>
        <button v-if="retryable && showRetry" class="toast-action" @click="handleRetry">
          重试
        </button>
        <button class="toast-close" @click="close">×</button>
      </div>
    </Transition>
  </Teleport>
</template>

<script setup>
import { ref, watch } from 'vue'

const props = defineProps({
  message: {
    type: String,
    default: ''
  },
  type: {
    type: String,
    default: 'info', // info, success, warning, error
    validator: (v) => ['info', 'success', 'warning', 'error'].includes(v)
  },
  duration: {
    type: Number,
    default: 4000 // 0 = 不自动关闭
  },
  suggestion: {
    type: String,
    default: ''
  },
  retryable: {
    type: Boolean,
    default: false
  },
  showRetry: {
    type: Boolean,
    default: true
  }
})

const emit = defineEmits(['close', 'retry'])

const visible = ref(false)
let timer = null

const close = () => {
  visible.value = false
  emit('close')
  clearTimer()
}

const handleRetry = () => {
  emit('retry')
  close()
}

const clearTimer = () => {
  if (timer) {
    clearTimeout(timer)
    timer = null
  }
}

const show = () => {
  visible.value = true
  clearTimer()
  if (props.duration > 0) {
    timer = setTimeout(close, props.duration)
  }
}

watch(() => props.message, (newVal) => {
  if (newVal) {
    show()
  }
}, { immediate: true })

defineExpose({ show, close })
</script>

<style lang="less">
@import '@/styles/variables.less';

.toast-container {
  position: fixed;
  top: 20px;
  right: 20px;
  z-index: @zindex-notification;
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 16px;
  background: @bg-color;
  border-radius: @border-radius-lg;
  box-shadow: @shadow-lg;
  min-width: 320px;
  max-width: 420px;
  transition: transform @transition-bounce, box-shadow @transition-fast;
}

.toast-container:hover {
  transform: translateX(-4px);
  box-shadow: @card-hover-shadow;
}

.toast-info { border-left: 4px solid @info-color; }
.toast-success { border-left: 4px solid @success-color; }
.toast-warning { border-left: 4px solid @warning-color; }
.toast-error { border-left: 4px solid @error-color; }

.toast-icon {
  font-size: 20px;
  line-height: 1;
  transition: transform @transition-bounce;
}

.toast-container:hover .toast-icon {
  transform: scale(1.2);
}

.toast-info .toast-icon { color: @info-color; }
.toast-success .toast-icon { color: @success-color; }
.toast-warning .toast-icon { color: @warning-color; }
.toast-error .toast-icon { color: @error-color; }

.toast-content { flex: 1; }

.toast-message {
  font-size: @font-size-base;
  color: @text-color;
  line-height: 1.5;
}

.toast-suggestion {
  font-size: @font-size-xs;
  color: @text-color-placeholder;
  margin-top: 4px;
}

.toast-action {
  padding: 4px 12px;
  background: @info-color;
  color: #fff;
  border: none;
  border-radius: @border-radius-sm;
  cursor: pointer;
  font-size: @font-size-xs;
  transition: background @transition-fast, transform @transition-fast;
}

.toast-action:hover {
  background: @info-color-hover;
  transform: translateY(-1px);
}

.toast-close {
  background: none;
  border: none;
  font-size: @font-size-lg;
  color: @text-color-placeholder;
  cursor: pointer;
  padding: 0;
  line-height: 1;
  transition: color @transition-fast, transform @transition-fast;
}

.toast-close:hover {
  color: @text-color-secondary;
  transform: scale(1.2);
}

.toast-enter-active { animation: toastSlideIn @transition-bounce; }
.toast-leave-active { animation: toastSlideOut @transition-normal forwards; }

@keyframes toastSlideIn {
  from { opacity: 0; transform: translateX(100%) scale(0.8); }
  to { opacity: 1; transform: translateX(0) scale(1); }
}

@keyframes toastSlideOut {
  from { opacity: 1; transform: translateX(0) scale(1); }
  to { opacity: 0; transform: translateX(100%) scale(0.8); }
}
</style>