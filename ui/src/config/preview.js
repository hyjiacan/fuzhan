// 预览配置（运行时从后端加载）
// 判定口径与后端 utils/preview_utils.go 保持一致：
// 全局开关关闭时一律不可预览；开启时，命中任一已启用内置分组（按各组配置的 MIME/扩展名列表匹配，
// 优先序 image > audio > video > document > text）或命中已启用的自定义组（custom，默认列表为空，
// 仅已配置的列表参与匹配）即可预览。
import { SystemApi } from '@/api'

// 预览类型默认值以后端为唯一来源：options 接口的 preview.groups 为已解析默认表的生效配置，
// preview.defaults 为内置默认表（供设置页「恢复默认」使用）。前端不内置默认表，
// options 加载成功前所有文件均判定为不可预览。
let previewDefaults = null

// 内置默认表（来自后端 options），未加载时返回 null
export function getPreviewDefaults() {
  return previewDefaults
}
// 内置分组匹配优先序，与后端保持一致
const GROUP_ORDER = ['image', 'audio', 'video', 'document', 'text']

// 预览全局开关（options 加载成功前视为关闭，一切文件不可预览）
let previewEnabled = false
// 各分组解析后的匹配规则（仅来自后端 options，未加载时为空）
let groupRules = {}

// ===== 预编译结论表（性能核心）=====
// 把"分组规则遍历"编译成"扩展名/MIME -> 是否可预览"的哈希查表，判定降到 O(1)。
// - extMap / mimeMap：精确匹配，value 为命中的分组名（布尔需求下也可视作 true）
// - extPrefix / mimePrefix：通配符规则无法进哈希表，退化为短前缀数组遍历（通常 0~2 项）
// 编译期按 GROUP_ORDER（后接 custom）顺序合并，先到先得，天然固化分组优先序。
let extMap = new Map()
let mimeMap = new Map()
let extPrefix = []
let mimePrefix = []
// 配置版本号：每次配置成功加载后自增，供调用方做缓存失效 / 响应式依赖
let configVersion = 0

// 解析逗号分隔的分组配置为匹配规则
function buildGroupRules(name, cfg) {
  const mimeExact = new Set()
  const mimeWildcard = []
  const extExact = new Set()
  const extWildcard = []
  for (const m of (cfg.mimes || '').split(',')) {
    const v = m.trim().toLowerCase()
    if (!v) continue
    if (v.endsWith('/*')) mimeWildcard.push(v.slice(0, -1))
    else mimeExact.add(v)
  }
  for (const e of (cfg.exts || '').split(',')) {
    const v = e.trim().replace(/^\./, '').toLowerCase()
    if (!v) continue
    if (v.endsWith('*')) extWildcard.push(v.slice(0, -1))
    else extExact.add(v)
  }
  const isEmpty =
    mimeExact.size === 0 &&
    mimeWildcard.length === 0 &&
    extExact.size === 0 &&
    extWildcard.length === 0
  return {
    enabled: cfg.enabled !== false,
    mimeExact,
    mimeWildcard,
    extExact,
    extWildcard,
    isEmpty
  }
}

// 以后端返回的分组生效配置（已解析默认值）构建匹配规则；后端为唯一来源
function initGroupRules(groupsConfig) {
  groupRules = {}
  for (const [name, cfg] of Object.entries(groupsConfig || {})) {
    groupRules[name] = buildGroupRules(name, cfg)
  }
}

// 把分组规则预编译成结论表。仅在 loadPreviewConfig 成功后调用一次。
function rebuildMatchTables() {
  extMap = new Map()
  mimeMap = new Map()
  extPrefix = []
  mimePrefix = []

  // 按优先序合并：先到先得，保留第一个分组的归属，固化优先级语义
  const order = [...GROUP_ORDER, 'custom']
  for (const g of order) {
    const r = groupRules[g]
    if (!r || !r.enabled || r.isEmpty) continue
    for (const e of r.extExact) {
      if (!extMap.has(e)) extMap.set(e, g)
    }
    for (const m of r.mimeExact) {
      if (!mimeMap.has(m)) mimeMap.set(m, g)
    }
    for (const p of r.extWildcard) extPrefix.push({ prefix: p, group: g })
    for (const p of r.mimeWildcard) mimePrefix.push({ prefix: p, group: g })
  }
}

// 文件是否命中指定分组（保留该函数供需要"分组归属"的场景使用；
// isPreviewable 已改走结论表，不再依赖此函数）
function matchGroup(name, ext, mimeType) {
  const r = groupRules[name]
  if (!r || !r.enabled) return false
  if (mimeType) {
    if (r.mimeExact.has(mimeType)) return true
    for (const p of r.mimeWildcard) {
      if (mimeType.startsWith(p)) return true
    }
  }
  if (ext) {
    if (r.extExact.has(ext)) return true
    for (const p of r.extWildcard) {
      if (ext.startsWith(p)) return true
    }
  }
  return false
}

// 预览全局开关是否开启
export function isPreviewEnabled() {
  return previewEnabled
}

// 配置版本号：调用方可将其放入响应式依赖，配置变更后触发重算
export function getPreviewConfigVersion() {
  return configVersion
}

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
      // 全局开关：仅显式 false 时关闭
      previewEnabled = data.preview.enabled !== false
      // 各分组：后端返回的是已解析默认值的生效配置（唯一来源）
      initGroupRules(data.preview.groups)
      // 预编译结论表（性能核心）
      rebuildMatchTables()
      // 内置默认表：供设置页「恢复默认」使用
      previewDefaults = data.preview.defaults || null
    }
    // 解析用户自定义 MIME 表（key 归一化为小写、忽略点号前缀，与后端一致）
    if (data?.mimeTable) {
      userMimeTable = {}
      for (const [ext, mime] of Object.entries(data.mimeTable)) {
        const e = ext.trim().replace(/^\./, '').toLowerCase()
        if (e && mime) userMimeTable[e] = String(mime).trim()
      }
    }
    // 配置已变更，版本号自增，供调用方失效缓存 / 触发重算
    configVersion++
  } catch (e) {
    console.error('加载预览配置失败', e)
  }
}

// 用户自定义 MIME 表（扩展名 -> MIME），使自定义扩展名也参与可预览判定（与后端预览识别一致）
let userMimeTable = {}

// 内置「扩展名 -> MIME」表，与后端 utils.GetMimeType 保持一致。
// 部分判定需要把扩展名还原成 MIME 参与分组匹配。
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
  pdf: 'application/pdf',
  mp3: 'audio/mpeg', wav: 'audio/wav', ogg: 'audio/ogg', oga: 'audio/ogg',
  m4a: 'audio/mp4', aac: 'audio/aac', flac: 'audio/flac', opus: 'audio/opus',
  mid: 'audio/midi', weba: 'audio/webm',
  mp4: 'video/mp4', webm: 'video/webm', mov: 'video/quicktime', m4v: 'video/x-m4v',
  avi: 'video/x-msvideo', mkv: 'video/x-matroska', ogv: 'video/ogg', mpg: 'video/mpeg',
  flv: 'video/x-flv', wmv: 'video/x-ms-wmv', '3gp': 'video/3gpp'
}

// 依据扩展名解析 MIME：用户自定义表优先，其次内置表，未命中返回 ''
function resolveMimeByExt(ext) {
  if (!ext) return ''
  return userMimeTable[ext] || BUILTIN_EXT_MIME[ext] || ''
}

// 从文件名解析扩展名（统一口径：无点或隐藏文件视为无扩展名，与后端一致）
function getExt(filename) {
  if (!filename) return ''
  const i = filename.lastIndexOf('.')
  return i > 0 ? filename.slice(i + 1).toLowerCase() : ''
}

// 依据文件名解析 MIME（供无后端 MIME 信息时识别预览类型使用）
export function resolveMime(filename) {
  if (!filename) return ''
  return resolveMimeByExt(getExt(filename))
}

// 判断文件是否可预览（与后端 utils.IsPreviewable 口径一致）
// 性能：走预编译结论表，精确命中 O(1)；扩展名精确命中时跳过 MIME 还原；
// 仅通配符规则退化为短前缀数组遍历。
export function isPreviewable(filename, mimeType = '') {
  if (!previewEnabled || !filename) return false
  const ext = getExt(filename)

  // 快路径 1：扩展名精确命中（无需还原 MIME）
  if (ext && extMap.has(ext)) return true

  // 需要 MIME 参与匹配时再还原，避免无谓查表
  const m = (mimeType || resolveMimeByExt(ext)).toLowerCase()

  // 快路径 2：MIME 精确命中
  if (m && mimeMap.has(m)) return true

  // 兜底：通配符前缀匹配（通常 0~2 项）
  if (ext) {
    for (const p of extPrefix) {
      if (ext.startsWith(p.prefix)) return true
    }
  }
  if (m) {
    for (const p of mimePrefix) {
      if (m.startsWith(p.prefix)) return true
    }
  }
  return false
}
