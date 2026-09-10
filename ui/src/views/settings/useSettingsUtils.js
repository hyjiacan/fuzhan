import { NumberUtils } from '@/utils'

// 格式化字节为人类可读单位
export function formatToUnit(bytes) {
  if (!bytes || bytes === 0) return '0'
  if (bytes >= 1024 * 1024 * 1024 * 1024) {
    return (bytes / (1024 * 1024 * 1024 * 1024)).toFixed(1) + 't'
  }
  if (bytes >= 1024 * 1024 * 1024) {
    return (bytes / (1024 * 1024 * 1024)).toFixed(1) + 'g'
  }
  if (bytes >= 1024 * 1024) {
    return (bytes / (1024 * 1024)).toFixed(1) + 'm'
  }
  if (bytes >= 1024) {
    return (bytes / 1024).toFixed(1) + 'k'
  }
  return bytes.toString()
}

export function formatSize(bytes) {
  return NumberUtils.formatFileSize(bytes)
}