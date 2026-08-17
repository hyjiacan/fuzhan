// 错误码定义
export const ErrorCode = {
  // 客户端错误 (1xxx)
  NETWORK_ERROR: 1001,
  TIMEOUT: 1002,
  PARSE_ERROR: 1003,
  VALIDATION_ERROR: 1004,

  // 认证错误 (2xxx)
  UNAUTHORIZED: 2001,
  TOKEN_EXPIRED: 2002,
  TOKEN_INVALID: 2003,
  PERMISSION_DENIED: 2004,

  // 业务错误 (3xxx)
  NOT_FOUND: 3001,
  FILE_TOO_LARGE: 3002,
  QUOTA_EXCEEDED: 3003,
  FILE_TYPE_NOT_ALLOWED: 3004,
  DUPLICATE_NAME: 3005,
  CONFLICT: 3006,
  RATE_LIMITED: 3007,

  // 服务器错误 (5xxx)
  INTERNAL_ERROR: 5001,
  SERVICE_UNAVAILABLE: 5002,
  DATABASE_ERROR: 5003,

  // 未知错误
  UNKNOWN: 9999
}

// 错误分类
export const ErrorCategory = {
  NETWORK: 'network',
  AUTH: 'auth',
  BUSINESS: 'business',
  SERVER: 'server',
  UNKNOWN: 'unknown'
}

// 错误消息配置
const errorMessages = {
  [ErrorCode.NETWORK_ERROR]: {
    category: ErrorCategory.NETWORK,
    message: '网络连接失败',
    suggestion: '请检查网络连接后重试',
    retryable: true
  },
  [ErrorCode.TIMEOUT]: {
    category: ErrorCategory.NETWORK,
    message: '请求超时',
    suggestion: '服务器响应较慢，请稍后重试',
    retryable: true
  },
  [ErrorCode.PARSE_ERROR]: {
    category: ErrorCategory.UNKNOWN,
    message: '数据解析失败',
    suggestion: '请联系管理员',
    retryable: false
  },
  [ErrorCode.VALIDATION_ERROR]: {
    category: ErrorCategory.BUSINESS,
    message: '数据验证失败',
    suggestion: '请检查输入内容',
    retryable: false
  },
  [ErrorCode.UNAUTHORIZED]: {
    category: ErrorCategory.AUTH,
    message: '未登录',
    suggestion: '请先登录',
    retryable: false
  },
  [ErrorCode.TOKEN_EXPIRED]: {
    category: ErrorCategory.AUTH,
    message: '登录已过期',
    suggestion: '请重新登录',
    retryable: false
  },
  [ErrorCode.TOKEN_INVALID]: {
    category: ErrorCategory.AUTH,
    message: '登录信息无效',
    suggestion: '请重新登录',
    retryable: false
  },
  [ErrorCode.PERMISSION_DENIED]: {
    category: ErrorCategory.AUTH,
    message: '权限不足',
    suggestion: '您没有权限执行此操作',
    retryable: false
  },
  [ErrorCode.NOT_FOUND]: {
    category: ErrorCategory.BUSINESS,
    message: '资源不存在',
    suggestion: '该文件或目录可能已被删除',
    retryable: false
  },
  [ErrorCode.FILE_TOO_LARGE]: {
    category: ErrorCategory.BUSINESS,
    message: '文件过大',
    suggestion: '请选择更小的文件或压缩后再上传',
    retryable: false
  },
  [ErrorCode.QUOTA_EXCEEDED]: {
    category: ErrorCategory.BUSINESS,
    message: '存储空间不足',
    suggestion: '请清理部分文件或联系管理员扩容',
    retryable: false
  },
  [ErrorCode.FILE_TYPE_NOT_ALLOWED]: {
    category: ErrorCategory.BUSINESS,
    message: '文件类型不支持',
    suggestion: '请选择允许的文件类型',
    retryable: false
  },
  [ErrorCode.DUPLICATE_NAME]: {
    category: ErrorCategory.BUSINESS,
    message: '文件名已存在',
    suggestion: '请使用不同的文件名',
    retryable: false
  },
  [ErrorCode.INTERNAL_ERROR]: {
    category: ErrorCategory.SERVER,
    message: '服务器内部错误',
    suggestion: '请稍后重试',
    retryable: true
  },
  [ErrorCode.SERVICE_UNAVAILABLE]: {
    category: ErrorCategory.SERVER,
    message: '服务暂时不可用',
    suggestion: '请稍后重试',
    retryable: true
  },
  [ErrorCode.DATABASE_ERROR]: {
    category: ErrorCategory.SERVER,
    message: '数据库错误',
    suggestion: '请稍后重试',
    retryable: true
  },
  [ErrorCode.UNKNOWN]: {
    category: ErrorCategory.UNKNOWN,
    message: '发生未知错误',
    suggestion: '请联系管理员',
    retryable: true
  },
  [ErrorCode.CONFLICT]: {
    category: ErrorCategory.BUSINESS,
    message: '资源冲突',
    suggestion: '请检查操作是否与其他操作冲突，稍后重试',
    retryable: true
  },
  [ErrorCode.RATE_LIMITED]: {
    category: ErrorCategory.BUSINESS,
    message: '操作过于频繁',
    suggestion: '请稍后再试',
    retryable: true
  }
}

// 错误码到 HTTP 状态码的映射
const httpStatusToErrorCode = {
  400: ErrorCode.VALIDATION_ERROR,
  401: ErrorCode.UNAUTHORIZED,
  403: ErrorCode.PERMISSION_DENIED,
  404: ErrorCode.NOT_FOUND,
  409: ErrorCode.CONFLICT,
  413: ErrorCode.FILE_TOO_LARGE,
  429: ErrorCode.RATE_LIMITED,
  500: ErrorCode.INTERNAL_ERROR,
  502: ErrorCode.SERVICE_UNAVAILABLE,
  503: ErrorCode.SERVICE_UNAVAILABLE,
  504: ErrorCode.TIMEOUT
}

// 错误对象类
export class AppError extends Error {
  constructor(code, message, details = {}) {
    super(message)
    this.code = code
    this.details = details
    const config = errorMessages[code] || errorMessages[ErrorCode.UNKNOWN]
    this.category = config.category
    this.suggestion = config.suggestion
    this.retryable = config.retryable
    this.timestamp = Date.now()
  }
}

// 从错误响应创建 AppError
export function createErrorFromResponse(response) {
  // 尝试从响应体中解析错误信息
  let code = ErrorCode.UNKNOWN
  let message = '发生错误'
  let details = {}

  if (response?.data) {
    const data = response.data
    if (data.code) {
      code = data.code
    }
    if (data.message) {
      message = data.message
    }
    if (data.errors) {
      details.errors = data.errors
    }
  }

  // 如果没有 code，尝试从 HTTP 状态码推断
  if (!response?.data?.code && response?.status) {
    code = httpStatusToErrorCode[response.status] || ErrorCode.UNKNOWN
    if (!response.data?.message) {
      message = errorMessages[code]?.message || '发生错误'
    }
  }

  return new AppError(code, message, details)
}

// 从网络错误创建 AppError
export function createNetworkError(error) {
  if (error.code === 'ECONNABORTED' || error.message?.includes('timeout')) {
    return new AppError(ErrorCode.TIMEOUT, '请求超时', { originalError: error.message })
  }
  return new AppError(ErrorCode.NETWORK_ERROR, '网络连接失败', { originalError: error.message })
}

// 格式化错误用于显示
export function formatErrorForDisplay(error) {
  if (error instanceof AppError) {
    return {
      title: error.message,
      suggestion: error.suggestion,
      category: error.category,
      retryable: error.retryable,
      code: error.code
    }
  }

  // 处理普通 Error 对象
  if (error instanceof Error) {
    return {
      title: error.message || '发生错误',
      suggestion: '请联系管理员',
      category: ErrorCategory.UNKNOWN,
      retryable: true,
      code: ErrorCode.UNKNOWN
    }
  }

  // 处理原始响应
  if (error?.response) {
    const appError = createErrorFromResponse(error.response)
    return formatErrorForDisplay(appError)
  }

  return {
    title: '发生未知错误',
    suggestion: '请联系管理员',
    category: ErrorCategory.UNKNOWN,
    retryable: true,
    code: ErrorCode.UNKNOWN
  }
}

// HTTP 状态码对应的默认友好消息
const statusFriendlyMessages = {
  400: '请求参数错误',
  401: '请先登录',
  403: '没有访问权限',
  404: '请求的资源不存在',
  409: '资源冲突，请检查后重试',
  413: '文件大小超过限制',
  422: '提交数据验证失败',
  429: '操作过于频繁，请稍后重试',
  500: '服务器内部错误，请稍后重试',
  502: '服务暂时不可用，请稍后重试',
  503: '服务暂时不可用，请稍后重试',
  504: '请求超时，请稍后重试'
}

// Axios 技术消息正则模式
const axiosErrorPattern = /^Request failed with status code (\d{3})$/i

/**
 * 将错误对象转换为用户友好的消息文本
 * - 如果是 AppError，直接返回其消息（已由后端或映射保证友好）
 * - 如果是 Axios 技术消息（如 "Request failed with status code 409"），替换为友好消息
 * - 其他情况返回默认消息
 *
 * @param {Error|AppError} error - 捕获的错误对象
 * @param {string} defaultMessage - 默认消息
 * @returns {string} 用户友好消息
 */
export function formatErrorMessage(error, defaultMessage = '操作失败，请稍后重试') {
  // AppError 的消息已经过 createErrorFromResponse 或后端保证是友好的
  if (error instanceof AppError) {
    return error.message || defaultMessage
  }

  const rawMessage = error?.message || String(error ?? '')

  // 检测 Axios 技术消息 "Request failed with status code XXX"
  const axiosMatch = rawMessage.match(axiosErrorPattern)
  if (axiosMatch) {
    const statusCode = parseInt(axiosMatch[1])
    if (statusFriendlyMessages[statusCode]) {
      return statusFriendlyMessages[statusCode]
    }
    return `服务器返回错误（${statusCode}），请稍后重试`
  }

  // Network Error
  if (rawMessage.includes('Network Error') || rawMessage.includes('ERR_CONNECTION')) {
    return '网络连接失败，请检查网络后重试'
  }

  // Timeout
  if (rawMessage.includes('timeout') || rawMessage.includes('Timeout')) {
    return '请求超时，请稍后重试'
  }

  // 其他技术消息特征（栈追踪、文件路径等）→ 使用默认消息
  const technicalPatterns = [
    /at https?:\/\//,
    /\/node_modules\//,
    /\.js:\d+:\d+/,
    /^TypeError:/,
    /^SyntaxError:/,
    /^ReferenceError:/,
    /^RangeError:/
  ]
  const isTechnical = technicalPatterns.some((p) => p.test(rawMessage))
  if (isTechnical) {
    return defaultMessage
  }

  // 消息看起来可能是用户友好的（如后端返回的中文消息），直接使用
  if (/[\u4e00-\u9fa5]/.test(rawMessage) && rawMessage.length < 100) {
    return rawMessage
  }

  return defaultMessage
}

export default {
  ErrorCode,
  ErrorCategory,
  AppError,
  createErrorFromResponse,
  createNetworkError,
  formatErrorForDisplay,
  formatErrorMessage
}