/**
 * 最新版文件识别算法（与 docs「文件列表最新版本自动识别与高亮算法文档」对齐）
 *
 * 设计：
 *  1) tokenizeStem：先提取原子 token（VERSION/DATE/PAREN），再把剩余部分按非字母数字分隔符切成 RAW 段，
 *     保证点分版本、日期、括号序号、x64/arm64/office2021 这类带数字的主体词不会被拆散。
 *  2) signature：结构签名 = 非版本主题词 + 描述词（按 三态配置 决定是否纳入）+ 扩展名。
 *  3) analyzeLatestVersions：按签名分"家族"，按扩展名分子"产物组"；族内 >=2 或 includeLone 时，
 *     各组按 版本 → 预发布 → 平台 → 架构 → 修改时间 → 文件名 的确定性优先级选出最高者高亮。
 *
 * 纯函数、无全局状态；缓存/Worker/中断等工程措施由调用方负责（见文档第 11、12 章）。
 */

// ==================== 1. 常量定义 ====================
const DESCRIPTORS = {
  channel: new Set(['stable', 'beta', 'alpha', 'rc', 'dev', 'canary', 'nightly', 'preview', 'release', 'insider']),
  platform: new Set(['windows', 'win32', 'win64', 'win', 'macos', 'osx', 'darwin', 'mac', 'linux', 'ubuntu', 'debian', 'freebsd', 'android', 'ios', 'web']),
  arch: new Set(['x86', 'x64', 'x86_64', 'x86-64', 'x64_86', 'arm', 'arm64', 'aarch64', 'amd64', 'i386', 'i486', 'i686', 'universal'])
}

const PRE_RELEASE_KEYWORDS = new Set([
  'alpha', 'beta', 'rc', 'dev', 'canary', 'nightly', 'preview', 'insider', 'snapshot', 'milestone'
])

// 通道/平台/架构三态分组配置：merge / separate / rank
const CHANNEL_RANK = { __none__: 110, stable: 100, rc: 90, beta: 80, alpha: 70, dev: 60, canary: 50, nightly: 40, preview: 30, insider: 20 }
const PLATFORM_RANK = { __none__: 100, windows: 30, win: 30, macos: 20, mac: 20, linux: 10 }
const ARCH_RANK = { __none__: 100, x64: 30, amd64: 30, arm64: 20, arm: 20, x86: 10, i386: 10, i686: 10, universal: 1 }

// 版本类型优先级：跨类型比较时（数值不同也按类型优先），DATE 明确日期 > 语义化版本 > 括号序号 > 裸数字
const VERSION_TYPE_PRIORITY = { DATE: 4, VERSION: 3, PAREN: 2, NUM: 1 }

// 双扩展名（视为一层扩展，多字节顺序无关）
const DEFAULT_MULTI_EXTS = ['.tar.gz', '.tar.bz2', '.tar.xz', '.tar.zst', '.tar.lz', '.tar.lzma'].sort((a, b) => b.length - a.length)

// ==================== 2. 扩展名与原子 token 提取 ====================
// 纯数字"扩展名"视作版本尾部（如 app-2.0 的 .0，避免误拆分成不同产物组）
function looksLikeNumericExt(s) {
  return /^\d{1,3}$/.test(s)
}

function extractExt(filename) {
  const lower = filename.toLowerCase()
  for (const ext of DEFAULT_MULTI_EXTS) {
    if (lower.endsWith(ext)) return { base: filename.slice(0, filename.length - ext.length), ext: ext.slice(1) }
  }
  const idx = filename.lastIndexOf('.')
  if (idx > 0) {
    const ext = filename.slice(idx + 1)
    if (looksLikeNumericExt(ext)) return { base: filename, ext: '' }
    return { base: filename.slice(0, idx), ext }
  }
  return { base: filename, ext: '' }
}

function validateDate(s) {
  let y, m, d
  if (s.includes('-')) {
    const p = s.split('-'); y = +p[0]; m = +p[1]; d = +p[2]
  } else {
    y = +s.slice(0, 4); m = +s.slice(4, 6); d = +s.slice(6, 8)
  }
  if (!(y >= 1900 && y <= 2100) || m < 1 || m > 12 || d < 1) return false
  const max = [31, (y % 4 === 0 && y % 100 !== 0) || y % 400 === 0 ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  return d <= max[m - 1]
}

// 先取出 VERSION / DATE / PAREN / 复合架构词 等原子 token，防止被分隔符切散。
// ARCH：x86_64 / x86-64 这类带分隔符的复合架构词若按普通切分会被误拆为 x86+64，
// 需整词保护并归一化（见 ARCH_COMBO_NORM），否则会被误判成 32 位 x86。
const ATOM_PATTERNS = [
  { type: 'DATE', re: /\d{4}-\d{2}-\d{2}|\d{8}/g, validate: validateDate },
  { type: 'PAREN', re: /[\[(]\d+[\])]/g },
  { type: 'VERSION', re: /v?\d+(?:\.\d+){1,5}/gi },
  { type: 'ARCH', re: /x86[-_]64|x64_86/gi }
]

// 复合架构词统一归一化为 64 位 x64 语义（无分隔单 token，复用现有 x64 的 rank/描述）
const ARCH_COMBO_NORM = { 'x86_64': 'x64', 'x86-64': 'x64', 'x64_86': 'x64' }

function extractAtoms(stem) {
  const atoms = []
  for (const { type, re, validate } of ATOM_PATTERNS) {
    re.lastIndex = 0
    let m
    while ((m = re.exec(stem))) {
      const text = m[0]
      if (validate && !validate(text)) continue
      const ok = type === 'VERSION'
        ? !(text.length === 8 && /^\d{8}$/.test(text) && validateDate(text)) // 8 位纯数字且是合法日期，归 DATE
        : true
      if (!ok) continue
      atoms.push({ type, text, start: m.index, end: m.index + text.length })
    }
  }
  atoms.sort((a, b) => a.start - b.start || a.end - b.end)
  const filtered = []
  let lastEnd = -1
  for (const a of atoms) {
    if (a.start >= lastEnd) { filtered.push(a); lastEnd = a.end }
  }
  return filtered
}

// 分隔符：非字母/数字/汉字 的其它字符（标点、空格、连字符、下划线等）视为切分点。
// 汉字(CJK)必须保留为 token——若把汉字也当分隔符丢弃，纯中文文件名会得到空签名，
// 导致两个无关的中文文件（如 28所.zip 与 智华.zip）被误归为同一家族。
const SEP_RE = /[^A-Za-z0-9\u4e00-\u9fff]+/
const PURE_NUM_RE = /^\d+$/
// 预发布关键字 + 紧跟数字（如 rc1/beta2）：仅含序号部分，不应进入结构签名，否则 rc1/rc2 分属不同家族
const SIG_PRERE_NUM_RE = /^(?:alpha|beta|rc|dev|canary|nightly|preview|insider|snapshot|milestone)\d+$/

function addRuns(s, out) {
  for (const p of s.split(SEP_RE)) {
    if (p) out.push({ type: 'RAW', text: p })
  }
}

// 生成有序 token 序列（原子 + RAW）
function tokenizeStem(stem) {
  const atoms = extractAtoms(stem)
  const tokens = []
  let cursor = 0
  for (const a of atoms) {
    if (a.start > cursor) addRuns(stem.slice(cursor, a.start), tokens)
    tokens.push({ type: a.type, text: a.text, start: a.start })
    cursor = a.end
  }
  if (cursor < stem.length) addRuns(stem.slice(cursor), tokens)
  return tokens
}

const tokenText = (t) => t.text.toLowerCase()

// ==================== 3. 描述符 / 版本 / 预发布提取 ====================
function classifyDescriptor(text) {
  if (DESCRIPTORS.channel.has(text)) return 'WC'
  if (DESCRIPTORS.platform.has(text)) return 'WP'
  if (DESCRIPTORS.arch.has(text)) return 'WA'
  return 'T'
}

// 版本选择：VERSION > DATE > PAREN > NUM（NUM 为扫到的最大裸数字段）
function extractVersion(tokens) {
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type === 'VERSION') {
      return { type: 'VERSION', text: tokens[i].text.replace(/^v/i, ''), index: i, parts: tokens[i].text.replace(/^v/i, '').split('.').map(Number) }
    }
  }
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type === 'DATE') {
      const t = tokens[i].text
      const parts = t.includes('-')
        ? t.split('-').map(Number)
        : [+t.slice(0, 4), +t.slice(4, 6), +t.slice(6, 8)]
      return { type: 'DATE', text: t, index: i, parts }
    }
  }
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type === 'PAREN') {
      const num = Number(tokens[i].text.replace(/[\[()\]]/g, ''))
      return { type: 'PAREN', text: String(num), index: i, parts: [num] }
    }
  }
  let best = null
  for (let i = 0; i < tokens.length; i++) {
    const t = tokens[i]
    if (t.type === 'RAW' && PURE_NUM_RE.test(t.text)) {
      const v = Number(t.text)
      if (!best && v === Number(tokens[i].text)) best = { type: 'NUM', text: t.text, index: i, parts: [v] }
      else if (best && v > best.parts[0]) best = { type: 'NUM', text: t.text, index: i, parts: [v] }
    }
  }
  return best
}

const PRERE_TAG_RE = /^(alpha|beta|rc|dev|canary|nightly|preview|insider|snapshot|milestone)([._-]?)(\d*)$/

// 预发布：仅统计版本索引之后的 RAW 段，识别关键字及其可选序号
function extractPre(tokens, versionIndex) {
  if (versionIndex == null) return null
  for (let i = versionIndex + 1; i < tokens.length; i++) {
    const t = tokens[i]
    if (t.type !== 'RAW') continue
    const lower = t.text.toLowerCase()
    if (PRE_RELEASE_KEYWORDS.has(lower)) {
      // 关键字单独成段
      const num = i + 1 < tokens.length && tokens[i + 1].type === 'RAW' && PURE_NUM_RE.test(tokens[i + 1].text)
        ? Number(tokens[i + 1].text) : 0
      return { text: num ? `${lower}.${num}` : lower, tag: lower, num }
    }
    const m = lower.match(PRERE_TAG_RE)
    if (m && PRE_RELEASE_KEYWORDS.has(m[1])) {
      const num = m[3] ? Number(m[3]) : 0
      return { text: num ? `${m[1]}.${num}` : m[1], tag: m[1], num }
    }
  }
  return null
}

function getDescriptorText(tokens, tag) {
  for (const t of tokens) {
    if (t.type === 'ARCH' && tag === 'WA') {
      const v = ARCH_COMBO_NORM[t.text.toLowerCase()]
      if (v) return v
      continue
    }
    if (t.type === 'RAW') {
      const lower = t.text.toLowerCase()
      if (tag === 'WC' && DESCRIPTORS.channel.has(lower)) return lower
      if (tag === 'WP' && DESCRIPTORS.platform.has(lower)) return lower
      if (tag === 'WA' && DESCRIPTORS.arch.has(lower)) return lower
    }
  }
  return null
}

// ==================== 4. 结构签名 ====================
function isSkippable(tag, config) {
  if (tag === 'WC') return config.channelGrouping !== 'separate'
  if (tag === 'WP') return config.platformGrouping !== 'separate'
  if (tag === 'WA') return config.archGrouping !== 'separate'
  return false
}

// 签名（不含版本、不含扩展名）：作为"家族"分组依据
function buildSignature(tokens, config) {
  const seen = new Set()
  const subjects = []
  for (const t of tokens) {
    if (t.type !== 'RAW') continue
    const lower = t.text.toLowerCase()
    if (PURE_NUM_RE.test(lower)) continue // 裸数字（预发布序号/旧编号）不进签名
    if (SIG_PRERE_NUM_RE.test(lower)) continue // 预发布关键字+序号（rc1/beta2）序号不进签名
    const tag = classifyDescriptor(lower)
    if (tag !== 'T' && isSkippable(tag, config)) continue
    if (!seen.has(lower)) { seen.add(lower); subjects.push(lower) }
  }
  return { sig: subjects.join('_'), subjects }
}

// ==================== 5. 分析入口（解析单个文件）====================
function parseFile(input, config) {
  const cfg = buildConfig(config)
  let path, filename
  if (typeof input === 'string') { path = input; filename = input.split('/').pop() }
  else { path = String(input.path != null ? input.path : input.name); filename = String(input.name || input.path || '') }

  const { base, ext } = extractExt(filename)
  const tokens = tokenizeStem(base)
  const ver = extractVersion(tokens)
  const pre = extractPre(tokens, ver ? ver.index : null)
  const { sig } = buildSignature(tokens, cfg)

  return {
    path,
    filename,
    base,
    ext,
    sig,
    sigKey: `${sig}|${ext}`, // 签名 + 扩展名：作为"产物组"分组依据
    version: ver ? ver.text : null,
    versionType: ver ? ver.type : null,
    vers: ver ? ver.parts : null,
    pre: pre ? pre.text : null,
    preTag: pre ? pre.tag : null,
    preNum: pre ? pre.num : 0,
    time: typeof input === 'object' ? new Date(input.modifiedTime || 0).getTime() : 0,
    channel: getDescriptorText(tokens, 'WC'),
    platform: getDescriptorText(tokens, 'WP'),
    arch: getDescriptorText(tokens, 'WA'),
    tokens
  }
}

// ==================== 6. 组内比较 ====================
const PRE_ORDER = { rc: 3, beta: 2, alpha: 1, preview: 1, dev: 0, nightly: 0, canary: 0, insider: 0, snapshot: 0, milestone: 1 }

function cmpSeg(a, b) {
  const len = Math.max(a.length, b.length)
  for (let i = 0; i < len; i++) {
    const x = a[i] || 0, y = b[i] || 0
    if (x !== y) return x - y
  }
  return 0
}

// 返回正数表示 a 比 b "更新"
function compareItem(a, b, config) {
  // 1. 版本（跨类型按类型优先级；同类型按数值逐段）
  if (a.vers && b.vers) {
    if (a.versionType !== b.versionType) {
      const pa = VERSION_TYPE_PRIORITY[a.versionType] || 0
      const pb = VERSION_TYPE_PRIORITY[b.versionType] || 0
      if (pa !== pb) return pa - pb
    } else {
      const c = cmpSeg(a.vers, b.vers)
      if (c !== 0) return c
    }
  } else if (a.vers && !b.vers) {
    return 1
  } else if (!a.vers && b.vers) {
    return -1
  }

  // 2. 预发布：无预发布(稳定版) > 预发布；同为预发布按 rc > beta > alpha > 序号
  const pa = a.preTag ? (PRE_ORDER[a.preTag] != null ? PRE_ORDER[a.preTag] : 0) : null
  const pb = b.preTag ? (PRE_ORDER[b.preTag] != null ? PRE_ORDER[b.preTag] : 0) : null
  if (pa === null && pb !== null) return 1
  if (pa !== null && pb === null) return -1
  if (pa !== null && pb !== null) {
    if (pa !== pb) return pa - pb
    if (a.preNum !== b.preNum) return a.preNum - b.preNum
  }

  // 3. 平台优先级（无标记 __none__ 权重最高）
  const ra = (config.platformRank[a.platform || '__none__']) || 0
  const rb = (config.platformRank[b.platform || '__none__']) || 0
  if (ra !== rb) return ra - rb

  // 4. 架构优先级
  const aa = (config.archRank[a.arch || '__none__']) || 0
  const ab = (config.archRank[b.arch || '__none__']) || 0
  if (aa !== ab) return aa - ab

  // 5. 修改时间
  if (a.time !== b.time) return a.time - b.time

  // 6. 文件名（稳定兜底）
  if (a.filename.length !== b.filename.length) return a.filename.length - b.filename.length
  return a.filename < b.filename ? -1 : a.filename > b.filename ? 1 : 0
}

// ==================== 7. 分组（家族 → rank 预筛 → 产物组/扩展名）====================
// rank 预筛：同一家族内，仅保留"某个维度（通道/平台/架构）权重最高"的那批文件，其余剔除；
// 无标记(__none__)权重最高，因此 rank 模式下"无标记"的产物优先，与通道 stable 优先语义对称。
function pickRanked(files, keyOf, rankMap) {
  const by = new Map()
  for (const f of files) {
    const k = keyOf(f) || '__none__'
    if (!by.has(k)) by.set(k, [])
    by.get(k).push(f)
  }
  let bestK = null, best = -1
  for (const k of by.keys()) {
    const r = rankMap[k] != null ? rankMap[k] : 0
    if (r > best) { best = r; bestK = k }
  }
  return bestK !== null ? by.get(bestK) : files
}

// 按"家族"（结构签名）分组。注意：rank 预筛不在此层做，
// 因为跨扩展名 rank 会把低优先级平台/架构的独立产物（如 linux 的 .tar.gz）整批剔除；
// rank 应在"产物组（同扩展名）"内应用，只对同一产物的不同变体选优先者。
function groupFiles(parsedFiles) {
  const familyMap = new Map()
  for (const pf of parsedFiles) {
    if (pf.type === 'dir' || pf.type === 'directory') continue
    if (!familyMap.has(pf.sig)) familyMap.set(pf.sig, [])
    familyMap.get(pf.sig).push(pf)
  }
  return Array.from(familyMap.entries()).map(([sig, family]) => ({ sig, family }))
}

// ==================== 8. 配置 ====================
function buildConfig(cfg = {}) {
  return {
    channelGrouping: cfg.channelGrouping || 'merge',
    platformGrouping: cfg.platformGrouping || 'rank',
    archGrouping: cfg.archGrouping || 'rank',
    includeLone: cfg.includeLone ?? false,
    platformRank: Object.assign({}, PLATFORM_RANK, cfg.platformRank),
    archRank: Object.assign({}, ARCH_RANK, cfg.archRank),
    cache: cfg.cache || null
  }
}

// ==================== 9. 置信度 ====================
function computeConfidence(pfArr) {
  const group = pfArr
  const sigSet = new Set(group.map(f => f.sig + '|' + f.ext))
  const consistency = sigSet.size === 1 ? 1 : 1 / sigSet.size
  const withVer = group.filter(f => f.version).length
  const parseability = group.length ? withVer / group.length : 0
  const sorted = group.filter(f => f.version).slice().sort((a, b) => cmpSeg(b.vers, a.vers))
  let distinctness = 0
  if (sorted.length >= 2) distinctness = cmpSeg(sorted[0].vers, sorted[1].vers) !== 0 ? 1 : 0.5
  else if (sorted.length === 1) distinctness = 0.3
  return 0.35 * consistency + 0.30 * parseability + 0.20 * distinctness + 0.15
}

function overallConfidence(groupList) {
  let total = 0, weight = 0
  for (const files of groupList) {
    total += computeConfidence(files) * files.length
    weight += files.length
  }
  return weight ? total / weight : 0
}

// ==================== 10. 核心入口 ====================
function analyzeLatestVersions(files, userCfg = {}) {
  const config = buildConfig(userCfg)
  const parsed = []
  for (const f of files || []) {
    if (typeof f === 'object' && (f.type === 'dir' || f.type === 'directory')) continue
    let p = null
    if (config.cache) {
      p = config.cache.get(f)
      if (!p) { p = parseFile(f, config); config.cache.set(f, p) }
    } else {
      p = parseFile(f, config)
    }
    parsed.push(p)
  }

  const families = groupFiles(parsed, config)
  const latestPaths = new Set()
  const confidences = {}
  const souls = []

  for (const { family } of groupFiles(parsed)) {
    // 按扩展名拆"产物组"；"是否同类/是否有版本依据"以产物组（预筛前）为粒度
    const byExt = new Map()
    for (const f of family) {
      if (!byExt.has(f.ext)) byExt.set(f.ext, [])
      byExt.get(f.ext).push(f)
    }
    for (const [, preFiles] of byExt) {
      // 没有同类（产物组只有 1 个文件）不标记
      if (preFiles.length < 2 && !config.includeLone) continue
      // 产物组内没有版本号 → 无版本依据，不标记
      if (!preFiles.some((f) => f.version)) continue
      // rank 预筛在产物组内进行：只对同一产物的不同平台/架构/通道变体选优先者，
      // 不因跨产物（不同扩展名）的权重差异剔除整批独立产物
      let cand = preFiles
      if (config.channelGrouping === 'rank') cand = pickRanked(cand, (f) => f.channel, CHANNEL_RANK)
      if (config.platformGrouping === 'rank') cand = pickRanked(cand, (f) => f.platform, config.platformRank)
      if (config.archGrouping === 'rank') cand = pickRanked(cand, (f) => f.arch, config.archRank)
      let best = cand[0]
      for (let i = 1; i < cand.length; i++) {
        if (compareItem(cand[i], best, config) > 0) best = cand[i]
      }
      latestPaths.add(best.path)
      confidences[best.path] = computeConfidence(preFiles)
      souls.push({ path: best.path, sig: best.sig, count: preFiles.length, confidence: confidences[best.path] })
    }
  }

  return {
    latestPaths,
    groups: souls,
    confidence: families.length ? overallConfidence(families.map((g) => g.family)) : 0,
    confidences
  }
}

// ==================== 11. 缓存（LRU）====================
class ParseCache {
  constructor(max = 1000) { this.max = max; this.map = new Map() }
  get(k) { if (!this.map.has(k)) return; const v = this.map.get(k); this.map.delete(k); this.map.set(k, v); return v }
  set(k, v) {
    if (this.map.has(k)) this.map.delete(k)
    else if (this.map.size >= this.max) this.map.delete(this.map.keys().next().value)
    this.map.set(k, v)
  }
}

export default { analyzeLatestVersions, ParseCache, parseFile }
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { analyzeLatestVersions, ParseCache, parseFile }
}