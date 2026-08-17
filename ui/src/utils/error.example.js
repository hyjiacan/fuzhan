/**
 * 统一错误处理使用示例
 *
 * 本模块展示了如何使用新的错误处理系统
 */

import { FileApi, UploadApi } from '@/api'
import { useToast } from '@/composables/useToast'
import { AppError, ErrorCode } from '@/utils/error'

// ========== 示例 1: 使用 toast 快捷方法 ==========
const toast = useToast()

// 成功提示
function onUploadSuccess() {
  toast.success('文件上传成功')
}

// 错误提示（自动分类）
function onUploadError(error) {
  toast.fromError(error)
}

// ========== 示例 2: 带重试的错误处理 ==========
async function uploadWithRetry(file) {
  const maxRetries = 3
  let lastError

  for (let i = 0; i < maxRetries; i++) {
    try {
      // 使用分片上传 API
      const { createSession, uploadChunk, finalize } = UploadApi.session
      const response = await createSession({ filename: file.name, fileSize: file.size })
      if (!response.success) {
        throw new Error(response.message || '创建上传会话失败')
      }

      const { uploadId, totalChunks, chunkSize } = response.data

      // 上传分片
      for (let chunkIndex = 0; chunkIndex < totalChunks; chunkIndex++) {
        const start = chunkIndex * chunkSize
        const end = Math.min(start + chunkSize, file.size)
        const chunk = file.slice(start, end)

        const formData = new FormData()
        formData.append('uploadId', String(uploadId))
        formData.append('chunkIndex', String(chunkIndex))
        formData.append('chunk', chunk)

        await uploadChunk(formData)
      }

      // 完成上传
      const result = await finalize(uploadId)
      if (!result.success) {
        throw new Error(result.message || '完成上传失败')
      }

      toast.success('上传成功')
      return
    } catch (error) {
      lastError = error

      // 如果是不可重试的错误，直接退出
      if (!error.retryable) {
        toast.fromError(error)
        return
      }

      // 最后一次尝试失败后显示错误
      if (i === maxRetries - 1) {
        toast.fromError(error, () => uploadWithRetry(file))
        return
      }

      // 等待后重试
      await new Promise(resolve => setTimeout(resolve, 1000 * (i + 1)))
    }
  }
}

// ========== 示例 3: 自定义业务错误码 ==========

// 抛出业务错误
function validateFile(file) {
  const maxSize = 100 * 1024 * 1024 // 100MB
  if (file.size > maxSize) {
    throw new AppError(ErrorCode.FILE_TOO_LARGE, '文件超过大小限制')
  }

  const allowedTypes = ['.jpg', '.png', '.pdf']
  const ext = '.' + file.name.split('.').pop().toLowerCase()
  if (!allowedTypes.includes(ext)) {
    throw new AppError(ErrorCode.FILE_TYPE_NOT_ALLOWED, '不支持的文件类型')
  }
}

// ========== 示例 4: 在组件中使用 ==========
/*
// 在 Vue 组件中
<template>
  <button @click="handleClick">上传</button>
</template>

<script setup>
import { useToast } from '@/composables/useToast'
import { FileApi } from '@/api'

const toast = useToast()

async function handleClick() {
  try {
    const result = await FileApi.list('/')
    console.log(result)
  } catch (error) {
    // 错误会自动显示 toast（通过全局处理器）
    // 这里可以做额外的业务处理
    if (error.code === ErrorCode.PERMISSION_DENIED) {
      console.log('需要管理员权限')
    }
  }
}
</script>
*/

// ========== 示例 5: 区分不同类型的错误 ==========
function handleError(error) {
  switch (error.category) {
    case 'network':
      // 网络问题 - 可以显示网络状态指示器
      console.log('检查网络连接')
      break
    case 'auth':
      // 认证问题 - 跳转登录页
      window.location.href = '/login'
      break
    case 'business':
      // 业务错误 - 显示具体错误信息
      console.log('业务错误:', error.message)
      break
    case 'server':
      // 服务器问题 - 可以显示维护公告
      console.log('服务器维护中')
      break
  }
}

// ========== 错误码参考 ==========
/*
  客户端错误 (1xxx):
  - NETWORK_ERROR: 1001  网络连接失败
  - TIMEOUT: 1002        请求超时
  - PARSE_ERROR: 1003    数据解析失败
  - VALIDATION_ERROR: 1004  数据验证失败

  认证错误 (2xxx):
  - UNAUTHORIZED: 2001     未登录
  - TOKEN_EXPIRED: 2002    登录已过期
  - TOKEN_INVALID: 2003    登录信息无效
  - PERMISSION_DENIED: 2004  权限不足

  业务错误 (3xxx):
  - NOT_FOUND: 3001        资源不存在
  - FILE_TOO_LARGE: 3002   文件过大
  - QUOTA_EXCEEDED: 3003   存储空间不足
  - FILE_TYPE_NOT_ALLOWED: 3004  文件类型不支持
  - DUPLICATE_NAME: 3005   文件名已存在

  服务器错误 (5xxx):
  - INTERNAL_ERROR: 5001   服务器内部错误
  - SERVICE_UNAVAILABLE: 5002  服务暂时不可用
  - DATABASE_ERROR: 5003   数据库错误
*/

export { uploadWithRetry, validateFile, handleError }