// 预览配置（运行时从后端加载）
import { SystemApi } from '@/api'

let allowMimes = []
let allowExts = []

// 加载预览配置（可选传入已获取的 options 数据）
export async function loadPreviewConfig(preloadedData) {
  try {
    let data
    if (preloadedData) {
      data = preloadedData
    } else {
      const res = await SystemApi.getOptions()
      if (!res.success) return
      data = res.data
    }

    if (data?.preview) {
      // 解析逗号分隔的 MIME 类型
      const mimes = data.preview.allowMimes
      if (mimes) {
        allowMimes = mimes.split(',').map(m => m.trim()).filter(m => m)
      }
      // 解析逗号分隔的扩展名
      const exts = data.preview.allowExts
      if (exts) {
        allowExts = exts.split(',').map(e => e.trim().replace(/^\./, '')).filter(e => e)
      }
    }
  } catch (e) {
    console.error('加载预览配置失败', e)
  }
}

// 判断文件是否可预览
export function isPreviewable(filename, mimeType = '') {
  if (!filename) return false
  const ext = filename.split('.').pop()?.toLowerCase() || ''

  // 优先匹配 MIME 类型
  if (mimeType) {
    mimeType = mimeType.toLowerCase()
    for (const m of allowMimes) {
      if (m.endsWith('/*')) {
        // 通配符匹配，如 text/*
        if (mimeType.startsWith(m.slice(0, -1))) return true
      } else if (m.toLowerCase() === mimeType) {
        return true
      }
    }
  }

  // 匹配扩展名
  for (const e of allowExts) {
    if (e.endsWith('*')) {
      // 通配符匹配
      if (ext.startsWith(e.slice(0, -1))) return true
    } else if (e.toLowerCase() === ext) {
      return true
    }
  }

  return false
}

// 获取 MIME 类型列表
export function getAllowMimes() {
  return [...allowMimes]
}

// 获取扩展名列表
export function getAllowExts() {
  return [...allowExts]
}