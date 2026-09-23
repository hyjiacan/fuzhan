// xxh3 哈希工具
export { xxh3Hash } from './xxhash'

// ============================================
// 表格全局配置（el-table-v2 数值类 prop，统一为单一常量便于调整）
// ============================================
export const TableConst = {
  ROW_HEIGHT: 32
}

// 字符串工具函数
export const StringUtils = {
  // 转义HTML特殊字符
  escapeHtml(str) {
    if (!str) return ''
    return str.replace(/[&<>"']/g, function (s) {
      const entityMap = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#39;'
      }
      return entityMap[s]
    })
  },

  // 截断字符串
  truncate(str, length = 50, suffix = '...') {
    if (!str) return ''
    if (str.length <= length) return str
    return str.substring(0, length) + suffix
  }
}

// 数字工具函数
export const NumberUtils = {
  // 格式化文件大小
  formatFileSize(bytes) {
    if (bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  },

  // 解析人类可读的大小字符串为字节
  // 支持: 100, 100b, 100kb, 100m, 100g, 100t (不区分大小写，自动转大写)
  // 特殊: "无限制", "unlimited", "0" -> 0
  parseFileSize(str) {
    if (typeof str === 'number') return str
    if (!str || typeof str !== 'string') return 0

    str = str.trim()
    if (!str) return 0

    // 处理无限制
    const unlimitedPatterns = ['无限制', 'unlimited', '∞', 'inf', 'infinite']
    if (unlimitedPatterns.includes(str.toLowerCase())) return 0

    // 纯数字直接返回
    if (/^\d+$/.test(str)) return parseInt(str, 10)

    const k = 1024
    const unitMap = {
      'B': 1,
      'K': k,
      'KB': k,
      'M': k * k,
      'MB': k * k,
      'G': k * k * k,
      'GB': k * k * k,
      'T': k * k * k * k,
      'TB': k * k * k * k
    }

    const match = str.match(/^(\d+(?:\.\d+)?)\s*([a-zA-Z]+)?$/)
    if (!match) return 0

    const num = parseFloat(match[1])
    const unit = match[2] ? match[2].toUpperCase() : 'B'

    const multiplier = unitMap[unit]
    if (!multiplier) return 0

    return Math.round(num * multiplier)
  },

  // 格式化数字
  formatNumber(num) {
    if (num >= 1000000) {
      return (num / 1000000).toFixed(1) + 'M'
    } else if (num >= 1000) {
      return (num / 1000).toFixed(1) + 'K'
    }
    return num.toString()
  }
}

// 文件工具函数
export const FileUtils = {
  // 获取文件扩展名
  getFileExtension(filename) {
    if (!filename) return ''
    const parts = filename.split('.')
    return parts.length > 1 ? parts[parts.length - 1].toLowerCase() : ''
  },

  // 判断是否为图片文件
  isImageFile(filename) {
    const ext = this.getFileExtension(filename)
    const imageExts = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp', 'svg']
    return imageExts.includes(ext)
  },

  // 判断是否为文本文件
  isTextFile(filename) {
    const ext = this.getFileExtension(filename)
    const textExts = ['txt', 'md', 'log', 'json', 'xml', 'csv', 'html', 'css', 'js', 'ts', 'vue', 'yml', 'yaml', 'properties', 'conf', 'config', 'sh', 'bat', 'ps1', 'py', 'java', 'go', 'rs', 'c', 'cpp', 'h', 'hpp', 'sql', 'ini', 'toml']
    return textExts.includes(ext)
  }
}

// 路径工具函数
export const PathUtils = {
  // 编码文件路径中的每个段，用于构造 URL
  // 输入: "rootName/中文 dir/file.txt" → 输出: "rootName/%E4%B8%AD%E6%96%87%20dir/file.txt"
  encodeFilePath(path) {
    if (!path) return ''
    return path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
  }
}

// 文件名排序比较器：英文（ASCII）排在中文等非 ASCII 名称前面
// 组内排序使用 localeCompare（中文按拼音），避免 zh-CN 默认把中文排在英文前
export const compareFileNames = (a, b) => {
  const aIsAscii = /^[\x00-\x7F]/.test(a)
  const bIsAscii = /^[\x00-\x7F]/.test(b)
  if (aIsAscii && !bIsAscii) return -1
  if (!aIsAscii && bIsAscii) return 1
  return a.localeCompare(b, undefined, { sensitivity: 'base' })
}

// ========== 版本标记分析 ==========
// 目标：在文件列表中识别"同一软件/同一文档家族"中版本最高者并高亮。
//
// 分三个步骤：
//  1) extractBaseName：提取"文件家族基名"——剥离版本段 + 通用描述词独立段
//     （发布通道 stable/beta/dev、发布形态 official/offline/portable/installer、
//      操作系统 windows/linux/mac、CPU 架构 x64/arm64 等），使同一软件的不同
//      命名变体能归并为同一家族（如 chrome 的 stable/beta 安装包归并为 chrome64）。
//  2) analyzeLatestVersions：按"基名 + 扩展名"分组（保留扩展名，.zip 与 .exe 视为
//     不同产物，各自独立高亮），组内 ≥2 个文件时按版本号最高者标记。
//  3) 版本号比较：支持点分多段/ v 前缀/单一数字序号(report(1))/日期编号(20240101)
//     等多种格式；版本并列时用修改时间决出唯一，仍并列则取文件名较新者。
//
// 判断规则（与 docs/02-文件管理模块.md「版本标记」一致）：
//  - 组内存在可解析版本号的文件 => 从其中选版本号最高者
//  - 组内全部无法解析版本号   => 回退为修改时间最新者（如 report.pdf/report(1).pdf）
//  - 组内仅 1 个文件（无相似文件）不标记；目录不参与；搜索模式不标记（由调用方控制）

// 尾部版本段：剥离文件名末尾的数字/版本段（如 app_v1.2.3.exe 的 1.2.3）
const VERSION_SEGMENT_RE = /(?:[_\-.[\]()]*)(?:v?\d+(?:\.\d+)*)(?:[_\-.[\]()]*)$/i
// 中部点分版本号（如 pandoc-3.11-… 的 3.11）；要求带点，避免误剥 x86_64 / office2021 这类裸数字
const MID_VERSION_RE = /(?:[_\-.[\]()]*)(?:v?\d+\.\d+(?:\.\d+)*)(?:[_\-.[\]()]*)/g

// 通用描述词：仅剥离作为"独立段"出现的完整词（前后为分隔符或边界），
// 避免误伤主体名中的子串（如 x64studio、winzip 中的 win/x64 不会误剥）。
const DESCRIPTOR_WORDS = [
  // 发布通道
  'stable', 'beta', 'alpha', 'dev', 'canary', 'nightly', 'preview', 'release', 'rc',
  // 发布形态
  'official', 'offline', 'online', 'portable', 'standalone', 'installer', 'setup', 'updated', 'latest',
  // 操作系统平台（长词在前，避免短词前缀抢占）
  'windows', 'macos', 'linux', 'ubuntu', 'debian', 'centos', 'freebsd', 'darwin', 'win32', 'win64', 'osx', 'win', 'mac',
  // CPU 架构
  'x86-64', 'x86_64', 'i386', 'i486', 'i686', 'x86', 'x64', '64bit', '32bit', '64-bit', '32-bit', 'arm64', 'aarch64', 'amd64', 'arm'
].join('|')
// 捕获前导分隔符（或 ^），仅删除描述词本身，保留该分隔符，后续统一归一
const DESCRIPTOR_RE = new RegExp(`(^|[_\\-.\\[\\]()])((?:${DESCRIPTOR_WORDS})(?=[_\\-.\\[\\]()]|$))`, 'gi')

export const extractBaseName = (filename) => {
  const name = String(filename || '')
  const dotIdx = name.lastIndexOf('.')
  const stem = dotIdx > 0 ? name.slice(0, dotIdx) : name
  const base = stem
    .replace(VERSION_SEGMENT_RE, '')
    .replace(MID_VERSION_RE, '-')
    .replace(DESCRIPTOR_RE, '$1')
    .replace(/-{2,}/g, '-')
    .replace(/^[-_]+|[-_]+$/g, '')
  return (base.trim() || name).toLowerCase()
}

// 提取文件名中的版本号 → 数字数组（如 "1.2.3" → [1,2,3]），无法解析返回 null
export const extractVersionParts = (rawName) => {
  const name = String(rawName || '')
  // 优先使用点分版本号，取"分段数最多"者作为主版本（避免 1.2.3-beta 场景取到 beta 段）
  const dotted = name.match(/\d+\.\d+(?:\.\d+){0,5}/g)
  if (dotted && dotted.length) {
    let ver = dotted[0]
    for (const cand of dotted) {
      if (cand.split('.').length > ver.split('.').length) ver = cand
    }
    return ver.split('.').map(Number)
  }
  // 兜底：单一数字段（如 report(1)、backup-20240101 的编号）
  const single = name.match(/(?:^|[_\-.[\]()])(v?\d+)(?=[_\-.[\]()]|$)/)
  return single ? single[1].replace(/^v/i, '').split('.').map(Number) : null
}

// 版本号逐段比较：a > b 返回正数，a < b 返回负数，相等返回 0
export const compareVersionParts = (a, b) => {
  const len = Math.max(a.length, b.length)
  for (let i = 0; i < len; i++) {
    const x = a[i] || 0
    const y = b[i] || 0
    if (x !== y) return x - y
  }
  return 0
}

const getFileExt = (name) => {
  const s = String(name || '')
  const i = s.lastIndexOf('.')
  return i > 0 ? s.slice(i + 1).toLowerCase() : ''
}

// 分析当前目录文件列表，返回"最新版本"文件的 path 集合
export const analyzeLatestVersions = (files) => {
  const latest = new Set()
  const groups = new Map()
  for (const f of files || []) {
    if (f.type === 'dir' || f.type === 'directory') continue
    // 分组：基名 + 扩展名（不同扩展名视为不同产物，各自独立比较版本）
    const key = `${extractBaseName(f.name)}|${getFileExt(f.name)}`
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key).push(f)
  }
  const timeOf = (f) => new Date(f.modifiedTime || 0).getTime()
  for (const group of groups.values()) {
    if (group.length < 2) continue
    const versioned = group.filter((f) => extractVersionParts(f.name) !== null)
    const pool = versioned.length >= 1 ? versioned : group
    let best = pool[0]
    for (const f of pool) {
      let cmp = 0
      if (versioned.length >= 1) {
        cmp = compareVersionParts(extractVersionParts(f.name), extractVersionParts(best.name))
      }
      if (cmp > 0 || (cmp === 0 && timeOf(f) > timeOf(best))) best = f
    }
    latest.add(best.path)
  }
  return latest
}

// ========== 时间工具函数 ==========
export const TimeUtils = {
  // 相对时间格式化
  formatRelativeTime(dateStr) {
    if (!dateStr) return '-'
    const date = new Date(dateStr)
    const now = new Date()
    const diff = now - date
    const minutes = Math.floor(diff / 60000)
    const hours = Math.floor(diff / 3600000)
    const days = Math.floor(diff / 86400000)

    if (minutes < 1) return '刚刚'
    if (minutes < 60) return `${minutes} 分钟前`
    if (hours < 24) return `${hours} 小时前`
    if (days < 30) return `${days} 天前`
    return date.toLocaleDateString('zh-CN')
  },

  // 格式化日期时间（统一格式：YYYY-MM-DD HH:mm:ss）
  formatDateTime(dateStr) {
    if (!dateStr) return '-'
    const d = new Date(dateStr)
    const Y = d.getFullYear()
    const M = String(d.getMonth() + 1).padStart(2, '0')
    const D = String(d.getDate()).padStart(2, '0')
    const h = String(d.getHours()).padStart(2, '0')
    const m = String(d.getMinutes()).padStart(2, '0')
    const s = String(d.getSeconds()).padStart(2, '0')
    return `${Y}-${M}-${D} ${h}:${m}:${s}`
  },

  // 格式化日期
  formatDate(dateStr) {
    if (!dateStr) return '-'
    return new Date(dateStr).toLocaleDateString('zh-CN')
  },

  // 判断是否在 24 小时内
  isRecent24h(dateStr) {
    if (!dateStr) return false
    const now = Date.now()
    const date = new Date(dateStr).getTime()
    return (now - date) >= 0 && (now - date) < 86400000
  }
}

// ========== 文件类型工具函数 ==========
export const FileTypeUtils = {
  // 获取文件类型图标 emoji
  getFileIcon(fileType) {
    if (!fileType) return '📄'
    const type = fileType.toLowerCase()
    const imageExts = ['jpg', 'jpeg', 'png', 'gif', 'bmp', 'webp', 'svg']
    const videoExts = ['mp4', 'avi', 'mov', 'wmv', 'mkv']
    const audioExts = ['mp3', 'wav', 'flac', 'aac']
    const docExts = ['pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx']
    const archiveExts = ['zip', 'rar', '7z', 'tar', 'gz']
    const codeExts = ['js', 'ts', 'vue', 'jsx', 'tsx']
    const webExts = ['html', 'css', 'less', 'scss']
    const langExts = ['go', 'java', 'py', 'c', 'cpp', 'rs']

    if (imageExts.includes(type)) return '🖼️'
    if (videoExts.includes(type)) return '🎬'
    if (audioExts.includes(type)) return '🎵'
    if (docExts.includes(type)) return '📕'
    if (archiveExts.includes(type)) return '📦'
    if (codeExts.includes(type)) return '📜'
    if (webExts.includes(type)) return '🌐'
    if (langExts.includes(type)) return '⚙️'
    return '📄'
  }
}

export default {
  StringUtils,
  NumberUtils,
  FileUtils,
  PathUtils,
  compareFileNames,
  extractBaseName,
  extractVersionParts,
  compareVersionParts,
  analyzeLatestVersions,
  TimeUtils,
  FileTypeUtils
}