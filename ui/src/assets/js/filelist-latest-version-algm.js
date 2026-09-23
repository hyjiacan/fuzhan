/**
 * 最新版文件识别算法 (v8 设计文档对齐版)
 */

// ==================== 1. 常量定义（严格按文档） ====================
const DESCRIPTORS = {
  channel: new Set(['alpha', 'beta', 'rc', 'stable', 'dev', 'canary', 'nightly', 'preview', 'insider']),
  platform: new Set(['win', 'windows', 'mac', 'macos', 'linux', 'android', 'ios', 'web']),
  arch: new Set(['x86', 'x64', 'arm', 'arm64', 'amd64', 'i386', 'universal'])
};

const PRE_RELEASE_KEYWORDS = new Set([
  'alpha', 'beta', 'rc', 'dev', 'canary', 'nightly', 'preview', 'insider', 'snapshot', 'milestone'
]);

// 【对齐】rank权重表：__none__ > 任何具体值
const CHANNEL_RANK = { __none__: 110, stable: 100, rc: 90, beta: 80, alpha: 70, dev: 60, canary: 50, nightly: 40, preview: 30, insider: 20 };
const PLATFORM_RANK = { __none__: 100 }; // 具体platform未在文档中定义权重，默认0
const ARCH_RANK = { __none__: 100 };     // 具体arch未在文档中定义权重，默认0

const VERSION_TYPE_PRIORITY = { DATE: 4, VERSION: 3, PAREN: 2, NUM: 1 };

// ==================== 2. 基础解析 ====================
function extractExt(filename) {
  const doubleExt = filename.match(/\.tar\.[a-z0-9]+$/i);
  if (doubleExt) return { base: filename.slice(0, doubleExt.index), ext: doubleExt[0].slice(1).toLowerCase() };
  const idx = filename.lastIndexOf('.');
  if (idx > 0) return { base: filename.slice(0, idx), ext: filename.slice(idx + 1).toLowerCase() };
  return { base: filename, ext: '' };
}

function tokenizeBase(base) {
  const tokens = [];
  let cur = '', type = '';
  for (const ch of base) {
    const t = /[a-zA-Z]/.test(ch) ? 'A' : /[0-9]/.test(ch) ? 'N' : 'S';
    if (t === type && t !== 'S') { cur += ch; }
    else {
      if (cur) tokens.push({ type, text: type === 'A' ? cur.toLowerCase() : cur, value: type === 'N' ? parseInt(cur, 10) : undefined });
      if (t === 'S') tokens.push({ type: 'SEP', text: ch });
      else { cur = ch; type = t; }
    }
  }
  if (cur) tokens.push({ type, text: type === 'A' ? cur.toLowerCase() : cur, value: type === 'N' ? parseInt(cur, 10) : undefined });
  return tokens;
}

function tagToken(token) {
  if (token.type === 'SEP') return 'SEP';
  if (token.type === 'A') {
    if (DESCRIPTORS.channel.has(token.text) || PRE_RELEASE_KEYWORDS.has(token.text)) return 'WC';
    if (DESCRIPTORS.platform.has(token.text)) return 'WP';
    if (DESCRIPTORS.arch.has(token.text)) return 'WA';
    return 'A';
  }
  return token.type === 'N' ? 'N' : 'UNK';
}

// ==================== 3. 签名生成（【对齐】不含版本号、不含扩展名） ====================
function isSkippable(tag, config) {
  if (tag === 'WC') return config.channelGrouping !== 'separate';
  if (tag === 'WP') return config.platformGrouping !== 'separate';
  if (tag === 'WA') return config.archGrouping !== 'separate';
  return false;
}

function buildSignature(tokens, tags, config) {
  const parts = [];
  for (let i = 0; i < tags.length; i++) {
    const tag = tags[i];
    if (tag === 'SEP') continue;

    let skip = isSkippable(tag, config);

    // N-token跳过：仅当前一个有效token是可跳过描述符时跳过
    // 【对齐】版本号本身的N不会被跳过（因为前面是A或SEP，不是可跳过描述符）
    // 但版本号不参与签名——等等，文档说"签名不含版本号"
    // 这意味着所有独立N都应该被排除？不，文档示例中 app-1.0 和 app-2.0 签名相同
    // 所以：所有未被描述符修饰的独立N序列都应从签名中排除
    if (!skip && tag === 'N') {
      let p = i - 1;
      while (p >= 0 && tags[p] === 'SEP') p--;
      // 如果前面没有token，或者前面是不可跳过的A/N，则此N是版本号的一部分，应跳过
      if (p < 0 || tags[p] === 'A' || tags[p] === 'N') skip = true;
      // 只有前面是可跳过描述符(WC/WP/WA)时，N才作为附属数字被跳过（已在上面处理）
      // 这里额外处理：独立版本号N也应跳过
      else if (!isSkippable(tags[p], config)) skip = true;
    }

    if (!skip) parts.push(tokens[i].text);
  }
  return parts.join('');
}

// ==================== 4. 版本提取（【对齐】非法DATE回退NUM） ====================
function validateDate(s) {
  if (!/^\d{8}$/.test(s)) return false;
  const y = +s.slice(0, 4), m = +s.slice(4, 6), d = +s.slice(6, 8);
  if (y < 1900 || y > 2100 || m < 1 || m > 12 || d < 1) return false;
  const leap = (y % 4 === 0 && y % 100 !== 0) || (y % 400 === 0);
  const maxDays = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31];
  return d <= maxDays[m - 1];
}

function extractVersionInfo(tokens, base) {
  // 1. VERSION (X.Y.Z格式)
  const verMatch = base.match(/(\d+(?:\.\d+)+)/);
  if (verMatch) {
    const val = verMatch[1];
    const startPos = verMatch.index;
    const endPos = startPos + val.length;
    let acc = 0, startIdx = 0, endIdx = 0;
    for (let i = 0; i < tokens.length; i++) {
      const tLen = tokens[i].text.length;
      if (acc <= startPos && acc + tLen > startPos) startIdx = i;
      if (acc < endPos && acc + tLen >= endPos) { endIdx = i; break; }
      acc += tLen;
    }
    return { type: 'VERSION', value: val, startIdx, endIdx };
  }

  // 2. DATE（合法才返回，否则继续往下走）
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type === 'N' && tokens[i].text.length === 8 && validateDate(tokens[i].text)) {
      return { type: 'DATE', value: tokens[i].text, startIdx: i, endIdx: i };
    }
  }

  // 3. PAREN
  for (let i = 0; i < tokens.length - 2; i++) {
    if (tokens[i].text === '(' && tokens[i + 2]?.text === ')' &&
        (tokens[i + 1].type === 'N' || tokens[i + 1].type === 'A')) {
      return { type: 'PAREN', value: tokens[i + 1].text, startIdx: i, endIdx: i + 2 };
    }
  }

  // 4. NUM（【对齐】非法DATE会自然落到这里）
  let maxV = -1, maxI = -1;
  for (let i = 0; i < tokens.length; i++) {
    if (tokens[i].type === 'N' && tokens[i].value > maxV) { maxV = tokens[i].value; maxI = i; }
  }
  if (maxI >= 0) return { type: 'NUM', value: String(maxV), startIdx: maxI, endIdx: maxI };

  return null;
}

// ==================== 5. 预发布提取（【对齐】精确起始点） ====================
function extractPreRelease(tokens, versionInfo) {
  if (!versionInfo) return null;
  const parts = [];
  let started = false;
  for (let i = versionInfo.endIdx + 1; i < tokens.length; i++) {
    const t = tokens[i];
    if (!started) {
      // 允许跳过一个分隔符(-或.)
      if (t.type === 'SEP' && (t.text === '-' || t.text === '.')) continue;
      if (t.type === 'A' && PRE_RELEASE_KEYWORDS.has(t.text)) {
        started = true;
        parts.push(t.text);
      } else break; // 遇到非预发布关键词立即停止
    } else {
      if (t.type === 'N') parts.push(t.text);
      else if (t.type === 'SEP' && (t.text === '.' || t.text === '-')) parts.push(t.text);
      else if (t.type === 'A' && PRE_RELEASE_KEYWORDS.has(t.text)) parts.push(t.text);
      else break;
    }
  }
  return parts.length ? parts.join('') : null;
}

// ==================== 6. 描述符提取 ====================
function getDescriptor(tokens, tags, target) {
  for (let i = 0; i < tags.length; i++) if (tags[i] === target) return tokens[i].text;
  return null;
}

// ==================== 7. 解析入口 ====================
function parseFile(path, config) {
  const filename = path.split('/').pop();
  const { base, ext } = extractExt(filename);
  const tokens = tokenizeBase(base);
  const tags = tokens.map(tagToken);
  const verInfo = extractVersionInfo(tokens, base);

  return {
    path, filename, base, ext,
    sig: buildSignature(tokens, tags, config),
    version: verInfo?.value ?? null,
    versionType: verInfo?.type ?? null,
    pre: extractPreRelease(tokens, verInfo),
    channel: getDescriptor(tokens, tags, 'WC'),
    platform: getDescriptor(tokens, tags, 'WP'),
    arch: getDescriptor(tokens, tags, 'WA'),
    tokens, tags
  };
}

// ==================== 8. 分组（【对齐】扩展名作为二级分组键） ====================
function groupFiles(parsedFiles, config) {
  // 一级分组：签名
  const sigGroups = new Map();
  for (const pf of parsedFiles) {
    if (!sigGroups.has(pf.sig)) sigGroups.set(pf.sig, []);
    sigGroups.get(pf.sig).push(pf);
  }

  // Channel rank 后处理（在一级组内）
  if (config.channelGrouping === 'rank') {
    for (const [sig, files] of sigGroups) {
      const byChannel = new Map();
      for (const f of files) {
        const ch = f.channel || '__none__';
        if (!byChannel.has(ch)) byChannel.set(ch, []);
        byChannel.get(ch).push(f);
      }
      let bestCh = null, bestRank = -1;
      for (const [ch] of byChannel) {
        const r = CHANNEL_RANK[ch] ?? 0;
        if (r > bestRank) { bestRank = r; bestCh = ch; }
      }
      if (bestCh) sigGroups.set(sig, byChannel.get(bestCh));
    }
  }

  // Platform rank 后处理
  if (config.platformGrouping === 'rank') {
    for (const [sig, files] of sigGroups) {
      const byPlat = new Map();
      for (const f of files) {
        const p = f.platform || '__none__';
        if (!byPlat.has(p)) byPlat.set(p, []);
        byPlat.get(p).push(f);
      }
      let bestP = null, bestRank = -1;
      for (const [p] of byPlat) {
        const r = PLATFORM_RANK[p] ?? 0;
        if (r > bestRank) { bestRank = r; bestP = p; }
      }
      if (bestP) sigGroups.set(sig, byPlat.get(bestP));
    }
  }

  // Arch rank 后处理
  if (config.archGrouping === 'rank') {
    for (const [sig, files] of sigGroups) {
      const byArch = new Map();
      for (const f of files) {
        const a = f.arch || '__none__';
        if (!byArch.has(a)) byArch.set(a, []);
        byArch.get(a).push(f);
      }
      let bestA = null, bestRank = -1;
      for (const [a] of byArch) {
        const r = ARCH_RANK[a] ?? 0;
        if (r > bestRank) { bestRank = r; bestA = a; }
      }
      if (bestA) sigGroups.set(sig, byArch.get(bestA));
    }
  }

  // 【对齐】二级分组：同签名+rank过滤后，按扩展名再分子组
  const finalGroups = new Map();
  for (const [sig, files] of sigGroups) {
    const byExt = new Map();
    for (const f of files) {
      const key = `${sig}|||${f.ext}`;
      if (!byExt.has(key)) byExt.set(key, []);
      byExt.get(key).push(f);
    }
    for (const [key, extFiles] of byExt) {
      finalGroups.set(key, extFiles);
    }
  }

  return finalGroups;
}

// ==================== 9. 排序 ====================
function cmpVer(a, b) {
  const pa = a.split(/[.\-]/), pb = b.split(/[.\-]/);
  for (let i = 0; i < Math.max(pa.length, pb.length); i++) {
    const na = parseInt(pa[i] || '0', 10), nb = parseInt(pb[i] || '0', 10);
    if (!isNaN(na) && !isNaN(nb)) { if (na !== nb) return na - nb; }
    else { const sa = pa[i] || '', sb = pb[i] || ''; if (sa !== sb) return sa < sb ? -1 : 1; }
  }
  return 0;
}

function compareItem(a, b) {
  // 1. 版本号比较
  if (a.version && b.version) {
    if (a.version !== b.version) {
      if (a.versionType === 'DATE' && b.versionType === 'DATE') return +a.version - +b.version;
      if (a.versionType === 'PAREN' && b.versionType === 'PAREN') {
        const na = parseInt(a.version, 10), nb = parseInt(b.version, 10);
        if (!isNaN(na) && !isNaN(nb)) return na - nb;
        return a.version < b.version ? -1 : 1;
      }
      if (a.versionType === 'NUM' && b.versionType === 'NUM') return +a.version - +b.version;
      return cmpVer(a.version, b.version);
    }
    // 版本号相同，比较类型优先级
    const priA = VERSION_TYPE_PRIORITY[a.versionType] || 0;
    const priB = VERSION_TYPE_PRIORITY[b.versionType] || 0;
    if (priA !== priB) return priA - priB;
  }

  // 2. stable(无pre) > pre-release
  if (!a.pre && b.pre) return 1;
  if (a.pre && !b.pre) return -1;
  if (a.pre && b.pre && a.pre !== b.pre) return cmpVer(a.pre, b.pre);

  // 3. 兜底
  if (a.filename.length !== b.filename.length) return a.filename.length - b.filename.length;
  return a.filename.localeCompare(b.filename);
}

function pickLatest(group) {
  if (!group.length) return null;
  group.sort(compareItem);
  return group[group.length - 1];
}

// ==================== 10. 置信度 & 缓存 ====================
function computeConfidence(pf, size) {
  let s = 0;
  if (pf.version) s += pf.version.includes('.') ? 0.4 : 0.2;
  if (size > 1) s += 0.3;
  if (!pf.pre) s += 0.2;
  if (['exe','dmg','deb','rpm','zip','tar.gz','apk','ipa'].includes(pf.ext)) s += 0.1;
  return s;
}

class ParseCache {
  constructor(max = 1000) { this.max = max; this.map = new Map(); }
  get(k) { if (!this.map.has(k)) return; const v = this.map.get(k); this.map.delete(k); this.map.set(k, v); return v; }
  set(k, v) {
    if (this.map.has(k)) this.map.delete(k);
    else if (this.map.size >= this.max) this.map.delete(this.map.keys().next().value);
    this.map.set(k, v);
  }
}

// ==================== 11. 核心入口 ====================
function buildConfig(cfg = {}) {
  return {
    channelGrouping: cfg.channelGrouping || 'merge',
    platformGrouping: cfg.platformGrouping || 'rank',
    archGrouping: cfg.archGrouping || 'rank',
    includeLone: cfg.includeLone ?? false,
    cache: cfg.cache || null
  };
}

function analyzeLatestVersions(files, userCfg = {}) {
  const config = buildConfig(userCfg);
  const parsed = files.map(f => {
    if (config.cache) {
      let p = config.cache.get(f);
      if (!p) { p = parseFile(f, config); config.cache.set(f, p); }
      return p;
    }
    return parseFile(f, config);
  });

  const groups = groupFiles(parsed, config);
  const latestPaths = new Set();
  const confs = [];

  for (const [, group] of groups) {
    // 【对齐】孤例过滤在二级组(签名+扩展名)级别判断
    if (group.length === 1 && !config.includeLone) continue;

    const latest = pickLatest(group);
    if (latest) {
      latestPaths.add(latest.path);
      confs.push(computeConfidence(latest, group.length));
    }
  }

  return {
    latestPaths,
    confidence: confs.length ? confs.reduce((a, b) => a + b, 0) / confs.length : 0
  };
}

export default { analyzeLatestVersions, ParseCache, parseFile };
if (typeof module !== 'undefined' && module.exports) {
  module.exports = { analyzeLatestVersions, ParseCache, parseFile };
}