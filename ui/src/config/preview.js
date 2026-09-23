// 预览配置（运行时从后端加载）
import { SystemApi } from '@/api'

let allowMimes = []
let allowExts = []
// 用户自定义 MIME 表（扩展名 -> MIME），使自定义扩展名也参与可预览判定（与后端预览识别一致）
let userMimeTable = {}

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
    // 解析用户自定义 MIME 表（key 归一化为小写、忽略点号前缀，与后端一致）
    if (data?.mimeTable) {
      userMimeTable = {}
      for (const [ext, mime] of Object.entries(data.mimeTable)) {
        const e = ext.trim().replace(/^\./, '').toLowerCase()
        if (e && mime) userMimeTable[e] = String(mime).trim()
      }
    }
  } catch (e) {
    console.error('加载预览配置失败', e)
  }
}

// 用 mimeType 匹配 allowMimes（含 text/* 通配），命中返回 true
function matchAllowMimes(mimeType) {
  if (!mimeType) return false
  mimeType = mimeType.toLowerCase()
  for (const m of allowMimes) {
    if (m.endsWith('/*')) {
      // 通配符匹配，如 text/*
      if (mimeType.startsWith(m.slice(0, -1))) return true
    } else if (m.toLowerCase() === mimeType) {
      return true
    }
  }
  return false
}

// 判断文件是否可预览
export function isPreviewable(filename, mimeType = '') {
  if (!filename) return false
  const ext = filename.split('.').pop()?.toLowerCase() || ''

  // 优先匹配 MIME 类型（若调用方传入了记录/识别的 MIME）
  if (mimeType && matchAllowMimes(mimeType)) return true

  // 用户自定义 MIME 表兜底：扩展名命中则用其映射的类型匹配 allowMimes，
  // 使「通过 mimeTable 自定义扩展名类型并配合 allow_mimes」的扩展名也显示为可预览
  const userMime = userMimeTable[ext]
  if (userMime && matchAllowMimes(userMime)) return true

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