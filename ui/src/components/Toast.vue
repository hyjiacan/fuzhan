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
  left: 50%;
  transform: translateX(-50%);
  z-index: @zindex-notification;
  display: flex;
  align-items: flex-start;
  gap: 10px;
  padding: 10px 14px;
  background: var(--el-bg-color-overlay);
  border-radius: var(--el-border-radius-base);
  box-shadow: @shadow-lg;
  min-width: 280px;
  max-width: 480px;
  font-size: var(--el-font-size-base);
  line-height: var(--el-font-line-height-primary);
  color: var(--el-text-color-primary);
  transition: transform @transition-bounce, box-shadow @transition-fast;
}

.toast-container:hover {
  transform: translateX(-50%) translateY(-2px);
  box-shadow: @card-hover-shadow;
}

.toast-icon {
  font-size: @font-size-lg;
  line-height: var(--el-font-line-height-primary);
  flex-shrink: 0;
  margin-top: 1px;
  transition: transform @transition-bounce;
}

.toast-info .toast-icon { color: var(--el-color-info); }
.toast-success .toast-icon { color: var(--el-color-success); }
.toast-warning .toast-icon { color: var(--el-color-warning); }
.toast-error .toast-icon { color: var(--el-color-danger); }

.toast-content { flex: 1; }

.toast-message {
  font-size: var(--el-font-size-base);
  color: var(--el-text-color-primary);
  line-height: var(--el-font-line-height-primary);
  word-break: break-word;
}

.toast-suggestion {
  font-size: var(--el-font-size-small);
  color: var(--el-text-color-secondary);
  margin-top: 4px;
}

.toast-action {
  flex-shrink: 0;
  margin-top: -2px;
  padding: 3px 12px;
  background: var(--el-color-primary);
  border: none;
  border-radius: var(--el-border-radius-base);
  cursor: pointer;
  font-size: var(--el-font-size-base);
  color: #fff;
  line-height: 1.4;
  align-self: flex-start;
  transition: background @transition-fast, transform @transition-fast;

  &:hover {
    background: var(--el-color-primary-dark-2);
    transform: translateY(-1px);
  }
}

.toast-close {
  flex-shrink: 0;
  align-self: flex-start;
  background: none;
  border: none;
  font-size: @font-size-lg;
  color: var(--el-text-color-secondary);
  cursor: pointer;
  padding: 0;
  line-height: var(--el-font-line-height-primary);
  transition: color @transition-fast, transform @transition-fast;

  &:hover {
    color: var(--el-text-color-primary);
    transform: scale(1.2);
  }
}

.toast-enter-active { animation: toastSlideIn @transition-bounce; }
.toast-leave-active { animation: toastSlideOut @transition-normal forwards; }

// 容器居中依赖 translateX(-50%)，带动画的 keyframes 必须保留该变换，
// 因此仅对 opacity/translateY 做位移动画，避免覆盖居中定位
@keyframes toastSlideIn {
  from { opacity: 0; transform: translateX(-50%) translateY(-16px); }
  to { opacity: 1; transform: translateX(-50%) translateY(0) scale(1); }
}

@keyframes toastSlideOut {
  from { opacity: 1; transform: translateX(-50%) translateY(0) scale(1); }
  to { opacity: 0; transform: translateX(-50%) translateY(-12px) scale(0.96); }
}
</style>