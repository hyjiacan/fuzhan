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

// 内置「扩展名 -> MIME」表，与后端 utils.GetMimeType 保持一致。
// 仅配置 allow_mimes（如 image/*）而扩展名未列入 allow_exts 时，
// 需要把扩展名还原成 MIME 才能命中，例如 png -> image/png。
const BUILTIN_EXT_MIME = {
  txt: 'text/plain', html: 'text/html', htm: 'text/html', css: 'text/css',
  js: 'application/javascript', json: 'application/json', xml: 'application/xml',
  md: 'text/markdown', yml: 'text/yaml', yaml: 'text/yaml', csv: 'text/csv',
  go: 'text/plain', java: 'text/plain', py: 'text/plain', c: 'text/plain',
  cpp: 'text/plain', h: 'text/plain', hpp: 'text/plain', cs: 'text/plain',
  rb: 'text/plain', php: 'text/plain', rs: 'text/plain', sh: 'text/plain',
  bat: 'text/plain', ps1: 'text/plain', sql: 'text/plain', vue: 'text/plain',
  tsx: 'text/plain', jsx: 'text/plain', ts: 'text/plain', scss: 'text/plain',
  less: 'text/plain', ini: 'text/plain', conf: 'text/plain', cfg: 'text/plain',
  log: 'text/plain',
  png: 'image/png', jpg: 'image/jpeg', jpeg: 'image/jpeg', gif: 'image/gif',
  webp: 'image/webp', svg: 'image/svg+xml', bmp: 'image/bmp', ico: 'image/x-icon',
  pdf: 'application/pdf'
}

// 依据扩展名解析 MIME：用户自定义表优先，其次内置表，未命中返回 ''
function resolveMimeByExt(ext) {
  if (!ext) return ''
  return userMimeTable[ext] || BUILTIN_EXT_MIME[ext] || ''
}

// 依据文件名解析 MIME（供无后端 MIME 信息时识别预览类型使用）
export function resolveMime(filename) {
  if (!filename) return ''
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  return resolveMimeByExt(ext)
}

// 判断文件是否可预览：取「MIME 匹配」与「扩展名匹配」的并集，任一命中即可预览
export function isPreviewable(filename, mimeType = '') {
  if (!filename) return false
  const ext = filename.split('.').pop()?.toLowerCase() || ''

  // MIME 匹配：调用方传入的 MIME 与按扩展名还原的 MIME 一并参与判定
  const mimes = [mimeType, resolveMimeByExt(ext)].filter(Boolean)
  if (mimes.some(m => matchAllowMimes(m))) return true

  // 扩展名匹配
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