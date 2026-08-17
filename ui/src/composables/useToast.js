import { ref } from 'vue'
import { formatErrorForDisplay } from '../utils/error.js'

// Toast 状态
const message = ref('')
const type = ref('info')
const suggestion = ref('')
const retryable = ref(false)
const showRetry = ref(true)
const onRetryCallback = ref(null)

// 显示 Toast
function showToast(options = {}) {
  message.value = options.message || options.title || ''
  type.value = options.type || 'info'
  suggestion.value = options.suggestion || ''
  retryable.value = options.retryable || false
  showRetry.value = options.showRetry !== false
  onRetryCallback.value = options.onRetry || null
}

// 快捷方法
const toast = {
  success: (msg, opts = {}) => showToast({ message: msg, type: 'success', ...opts }),
  error: (msg, opts = {}) => showToast({ message: msg, type: 'error', ...opts }),
  warning: (msg, opts = {}) => showToast({ message: msg, type: 'warning', ...opts }),
  info: (msg, opts = {}) => showToast({ message: msg, type: 'info', ...opts }),

  // 处理 AppError
  fromError(error, onRetry = null) {
    const display = formatErrorForDisplay(error)
    showToast({
      message: display.title,
      type: display.category === 'network' || display.category === 'server' ? 'error' : 'error',
      suggestion: display.suggestion,
      retryable: display.retryable,
      onRetry
    })
  },

  // 处理 API 错误响应
  fromResponse(error, onRetry = null) {
    const display = formatErrorForDisplay(error)
    showToast({
      message: display.title,
      type: 'error',
      suggestion: display.suggestion,
      retryable: display.retryable,
      onRetry
    })
  },

  // 通用 show
  show: showToast
}

export function useToast() {
  return {
    // 状态
    message,
    type,
    suggestion,
    retryable,
    showRetry,

    // 方法
    show: showToast,
    success: toast.success,
    error: toast.error,
    warning: toast.warning,
    info: toast.info,
    fromError: toast.fromError,
    fromResponse: toast.fromResponse
  }
}

export default useToast