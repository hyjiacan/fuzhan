// xxh3 哈希工具
export { xxh3Hash } from './xxhash'

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
// 提取"程序基名"：剥离文件名中的版本/数字段
// 尾部版本（支持多段版本号）+ 中部点分版本号
// 例：app_v1.2.3.exe → app；report(1).pdf → report；app-2.zip → app；backup-20240101.zip → backup
// pandoc-3.11-windows-x86_64.zip 与 pandoc-3.8.2.1-... 归并为同一基名 pandoc-windows-x86_64
const VERSION_SEGMENT_RE = /(?:[_\-.[\]()]*)(?:v?\d+(?:\.\d+)*)(?:[_\-.[\]()]*)$/i
// 中部点分版本号（如 pandoc-3.11-… 的 3.11）；要求带点，避免误剥 x86_64 / office2021 这类裸数字
const MID_VERSION_RE = /(?:[_\-.[\]()]*)(?:v?\d+\.\d+(?:\.\d+)*)(?:[_\-.[\]()]*)/g

export const extractBaseName = (filename) => {
  const name = String(filename || '')
  const dotIdx = name.lastIndexOf('.')
  const stem = dotIdx > 0 ? name.slice(0, dotIdx) : name
  const base = stem
    .replace(VERSION_SEGMENT_RE, '')
    .replace(MID_VERSION_RE, '-')
    .replace(/-{2,}/g, '-')
    .replace(/^[-_]+|[-_]+$/g, '')
  return (base.trim() || name).toLowerCase()
}

// 分析当前目录文件列表，返回"最新版本"文件的 path 集合
// 规则：基名相同的文件 ≥2 个时，修改时间最新的标记为最新版本
export const analyzeLatestVersions = (files) => {
  const latest = new Set()
  const groups = new Map()
  for (const f of files || []) {
    if (f.type === 'dir' || f.type === 'directory') continue
    const key = extractBaseName(f.name)
    if (!groups.has(key)) groups.set(key, [])
    groups.get(key).push(f)
  }
  for (const group of groups.values()) {
    if (group.length < 2) continue
    let newest = group[0]
    for (const f of group) {
      if (new Date(f.modifiedTime) > new Date(newest.modifiedTime)) newest = f
    }
    latest.add(newest.path)
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
  analyzeLatestVersions,
  TimeUtils,
  FileTypeUtils
}