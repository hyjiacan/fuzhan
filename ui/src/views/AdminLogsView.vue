<template>
  <div class="admin-logs page-container page-container--fill">
    <div class="header-section">
      <div class="header-left">
        <h2>日志</h2>
        <p class="description">
          查看服务端日志文件，支持增量跟随、级别与关键字过滤、向前翻页与时间定位
        </p>
      </div>
      <div class="header-right">
        <span v-if="logDir" class="log-dir" :title="logDir">{{ logDir }}</span>
        <el-button type="primary" size="small" :loading="loading" @click="reload">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </div>

    <!-- 顶栏：文件视角 + 级别过滤 + 关键字 + 调用方 -->
    <div class="log-toolbar">
      <div class="toolbar-group">
        <span class="toolbar-label">日志文件</span>
        <el-select
          v-model="file"
          size="small"
          class="file-select"
          placeholder="选择日志文件"
          :disabled="files.length === 0"
          @change="onFileChange"
        >
          <el-option v-for="item in files" :key="item.name" :label="item.name" :value="item.name">
            <span class="file-option-name">{{ item.name }}</span>
            <span class="file-option-size">{{ formatSize(item.size) }}</span>
          </el-option>
        </el-select>
      </div>
      <div class="toolbar-group">
        <span class="toolbar-label">级别</span>
        <el-button size="small" :type="isAllLevels ? 'primary' : 'default'" plain @click="setAllLevels">全部</el-button>
        <el-button size="small" :type="quickWarn ? 'warning' : 'default'" plain @click="setWarnPlus">警告及以上</el-button>
        <el-button size="small" :type="quickError ? 'danger' : 'default'" plain @click="setErrorPlus">错误及以上</el-button>
        <el-checkbox-group v-model="checkedLevels" size="small" class="level-checks">
          <el-checkbox-button v-for="lv in LEVEL_ORDER" :key="lv" :label="lv">{{ lv }}</el-checkbox-button>
        </el-checkbox-group>
      </div>
      <div class="toolbar-group toolbar-grow">
        <el-input
          v-model="keywordInput"
          size="small"
          clearable
          placeholder="关键字过滤（加载更早时同步回扫服务端）"
          class="keyword-input"
          @input="onKeywordInput"
        />
      </div>
      <div v-if="callerFilter" class="toolbar-group">
        <el-tag closable size="small" type="info" @close="callerFilter = ''">
          调用方 [{{ callerFilter }}]
        </el-tag>
      </div>
      <div class="toolbar-group">
        <el-button size="small" :disabled="lines.length === 0" @click="copyView">复制视图</el-button>
      </div>
    </div>

    <el-alert
      v-if="error"
      class="conn-error"
      type="error"
      :title="error.title"
      :description="error.message"
      show-icon
      :closable="false"
    />

    <el-empty
      v-else-if="!loading && files.length === 0"
      description="日志目录下暂无可查看的日志文件"
      :image-size="80"
    />

    <template v-else>
      <div class="log-status">
        <span>
          {{ file }} · {{ displayLines.length }} 行可见
          {{ following ? '· 跟随最新' : '· 已暂停跟随（上翻阅读中）' }}
        </span>
        <span class="status-right">
          每 {{ POLL_MS / 1000 }} 秒增量拉取 · 已缓存 {{ lines.length }} / 最多 {{ MAX_BUFFER_LINES }} 行
        </span>
      </div>

      <div class="log-main">
        <!-- 虚拟滚动：占位高度 = 可见行数 × 行高，仅渲染可视窗口切片 -->
        <div ref="viewportRef" class="log-viewport" @scroll="onScroll">
          <div class="log-phantom" :style="{ height: phantomHeight + 'px' }">
            <div class="log-window" :style="{ transform: 'translateY(' + offsetY + 'px)' }">
              <div
                v-for="row in visibleRows"
                :key="row.uid"
                class="log-row"
                :style="{ height: ROW_HEIGHT + 'px' }"
                :class="[
                  'log-row--' + row.item.level.toLowerCase(),
                  { 'log-row--selected': selectedUid === row.uid, 'log-row--highlight': highlightUid === row.uid },
                ]"
                @click="onRowSelect(row.uid)"
              >
                <span
                  class="log-col log-col--time"
                  :title="row.item.time + '（点击定位到该时间）'"
                  @click.stop="onTimeClick(row)"
                >{{ row.item.time }}</span>
                <span class="log-col log-col--level" :class="'lvl-' + row.item.level">{{ row.item.level }}</span>
                <span
                  class="log-col log-col--caller"
                  :title="'点击按调用方 [ ' + row.item.caller + ' ] 筛选'"
                  @click.stop="onCallerClick(row)"
                >{{ row.item.caller }}</span>
                <span class="log-col log-col--msg" :title="row.item.message">{{ row.item.message }}</span>
              </div>
              <div v-if="visibleRows.length === 0" class="log-empty-row">
                <span>{{ loading ? '加载中…' : '无可显示日志行（当前过滤条件下）' }}</span>
              </div>
            </div>
          </div>
        </div>

        <!-- 暂停跟随气泡 -->
        <transition name="el-fade-in">
          <div v-if="!following && pendingNew > 0" class="pause-bubble">
            <span class="bubble-text">已暂停，{{ pendingNew }} 条新日志</span>
            <el-button size="small" type="primary" @click="resumeFollowing">回到底部继续跟随</el-button>
          </div>
        </transition>

        <!-- 单行详情 -->
        <transition name="el-fade-in">
          <el-card v-if="selectedLine" class="detail-card" shadow="always">
            <div class="detail-header">
              <span class="detail-title">
                {{ selectedLine.time }} · {{ selectedLine.level }} · {{ selectedLine.caller }}
              </span>
              <span class="detail-actions">
                <el-button size="small" @click="copyLine(selectedLine)">复制该行</el-button>
                <el-button size="small" @click="selectedLine = null">收起</el-button>
              </span>
            </div>
            <pre class="detail-body">{{ detailBody }}</pre>
          </el-card>
        </transition>
      </div>

      <!-- 全局操作行：加载更早 / 时间定位 -->
      <div class="log-footerbar">
        <el-button
          size="small"
          :disabled="!canLoadOlder || olderLoading"
          :loading="olderLoading"
          @click="loadOlder"
        >
          {{ canLoadOlder ? '加载更早日志' : '已到日志起点' }}
        </el-button>
        <div class="locate-box">
          <el-input
            v-model="locateInput"
            size="small"
            placeholder="定位时间，如 2026-10-25 09:00:01"
            class="locate-input"
            @keyup.enter="locate()"
          />
          <el-button size="small" type="primary" :loading="locating" @click="locate()">定位</el-button>
        </div>
        <span class="locate-hint">点击行内时间可直接定位</span>
      </div>
    </template>
  </div>
</template>

<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { ElMessage } from 'element-plus'
import { AdminApi } from '@/api'

// 级别序（与服务端 normalizeLogLevel 归一后的 6 级一致）
const LEVEL_ORDER = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL']
const LEVEL_RANK = { TRACE: 0, DEBUG: 1, INFO: 2, WARN: 3, ERROR: 4, CRITICAL: 5 }

// 虚拟滚动依赖固定行高，改行高需同步修改样式
const ROW_HEIGHT = 24
// 可视区上下额外渲染的补偿行
const OVERSCAN = 20
// 前端缓冲封顶，超出后从头部裁剪，保证内存有界
const MAX_BUFFER_LINES = 20000
// 首屏 / 向前翻页 / 增量拉取行数
const INITIAL_LINES = 200
const FETCH_LINES = 300
// 增量轮询间隔
const POLL_MS = 2500

// ============ 状态 ============
const files = ref([])
const logDir = ref('')
const file = ref('app.log')
const loading = ref(false)
const error = ref(null)

// 已拉取的行缓冲（旧 → 新，最新在下）
const lines = ref([])
// 增量游标（上次响应的 next_offset）
const cursor = ref(null)
const hasMoreOlder = ref(true)

// 过滤器
const checkedLevels = ref(LEVEL_ORDER.slice())
const keywordInput = ref('')
const keyword = ref('')
const callerFilter = ref('')

// 虚拟滚动
const viewportRef = ref(null)
const scrollTop = ref(0)
const viewportHeight = ref(0)

// 跟随状态
const following = ref(true)
const pendingNew = ref(0)

// 交互状态
const selectedUid = ref(null)
const selectedLine = ref(null)
const highlightUid = ref(null)
const olderLoading = ref(false)
const locating = ref(false)
const locateInput = ref('')

// 非响应式私有字段
let reqSeq = 0
let pollTimer = null
let debounceTimer = null
let highlightTimer = null
let resizeObserver = null

// ============ 计算属性 ============
const displayLines = computed(() => {
  const kw = keyword.value
  return lines.value.filter((line) => {
    if (!checkedLevels.value.includes(line.level)) return false
    if (callerFilter.value && line.caller !== callerFilter.value) return false
    if (kw) {
      const hay1 = String(line.raw || '').toLowerCase()
      const hay2 = String(line.message || '').toLowerCase()
      if (hay1.indexOf(kw) < 0 && hay2.indexOf(kw) < 0) return false
    }
    return true
  })
})

const phantomHeight = computed(() => Math.max(displayLines.value.length, 1) * ROW_HEIGHT)

const sliceStart = computed(() => {
  const row = Math.floor(scrollTop.value / ROW_HEIGHT)
  return Math.max(0, row - OVERSCAN)
})

const sliceEnd = computed(() => {
  const rowsVisible = Math.ceil(viewportHeight.value / ROW_HEIGHT)
  return Math.min(displayLines.value.length, sliceStart.value + rowsVisible + OVERSCAN * 2)
})

const visibleRows = computed(() => {
  const start = sliceStart.value
  const end = Math.max(start, Math.min(displayLines.value.length, sliceEnd.value))
  const out = []
  for (let i = start; i < end; i += 1) {
    const line = displayLines.value[i]
    if (line) out.push({ uid: line.uid, item: line })
  }
  return out
})

const offsetY = computed(() => sliceStart.value * ROW_HEIGHT)

const canLoadOlder = computed(() => lines.value.length > 0 && lines.value[0].pos > 0)

const isAllLevels = computed(() => checkedLevels.value.length === LEVEL_ORDER.length)
const quickWarn = computed(() => levelSetEqual(['WARN', 'ERROR', 'CRITICAL']))
const quickError = computed(() => levelSetEqual(['ERROR', 'CRITICAL']))

// 服务端级别阈值（选中集最低级别；全选/空选不传）
const serverLevel = computed(() => {
  if (checkedLevels.value.length === 0 || isAllLevels.value) return ''
  let minRank = Infinity
  for (const lv of checkedLevels.value) {
    const rank = LEVEL_RANK[lv]
    if (rank === undefined) continue
    if (rank < minRank) minRank = rank
  }
  if (!Number.isFinite(minRank)) return ''
  return LEVEL_ORDER[minRank]
})

const detailBody = computed(() => {
  const line = selectedLine.value
  if (!line) return ''
  const parts = [`${line.time} ${line.level} ${line.caller}`]
  parts.push(String(line.message || ''))
  if (line.extra) parts.push(String(line.extra))
  return parts.join('\n')
})

// ============ 工具函数 ============
function levelSetEqual(arr) {
  return arr.slice().sort().join(',') === checkedLevels.value.slice().sort().join(',')
}

function formatSize(size) {
  if (!size && size !== 0) return '-'
  if (size < 1024) return `${size} B`
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KB`
  return `${(size / 1024 / 1024).toFixed(1)} MB`
}

// 时间串归一为可字典比较的 YYYY-MM-DD HH:MM:SS
function timeKey(text) {
  return String(text || '').replace('T', ' ').slice(0, 19)
}

function normalizeLine(line) {
  return {
    uid: `p${line.pos}`,
    time: line.time || '',
    level: line.level || 'INFO',
    caller: line.caller || '-',
    message: line.message || '',
    extra: line.extra || '',
    raw: line.raw || '',
    pos: line.pos,
  }
}

function errorTitleFor(err) {
  const apiCode = err && err.apiCode
  if (apiCode === 'VALIDATION') return '参数校验失败'
  if (apiCode === 'TIMEOUT') return '连接超时'
  if (apiCode === 'HTTP') return '后端返回异常状态'
  if (apiCode === 'PARSE') return '响应解析失败'
  if (apiCode === 'NETWORK') return '网络错误或跨域拦截'
  return '读取日志失败'
}

// ============ 缓冲维护 ============
function trimBuffer() {
  const overflow = lines.value.length - MAX_BUFFER_LINES
  if (overflow > 0) lines.value.splice(0, overflow)
}

function passesClientFilter(line) {
  if (!checkedLevels.value.includes(line.level)) return false
  if (callerFilter.value && line.caller !== callerFilter.value) return false
  if (keyword.value) {
    const hay1 = String(line.raw || '').toLowerCase()
    const hay2 = String(line.message || '').toLowerCase()
    if (hay1.indexOf(keyword.value) < 0 && hay2.indexOf(keyword.value) < 0) return false
  }
  return true
}

// 追加新行（isPagedBack=true 表示向前翻页，插到头部）
function addLines(incoming, isPagedBack) {
  const firstPos = lines.value.length ? lines.value[0].pos : -1
  const lastPos = lines.value.length ? lines.value[lines.value.length - 1].pos : -1
  const prepend = []
  const append = []
  for (const line of incoming) {
    if (!line || line.pos === undefined || line.pos === null) continue
    if (!isPagedBack && line.pos <= lastPos) continue
    if (isPagedBack && line.pos >= firstPos) continue
    if (isPagedBack) prepend.push(line)
    else append.push(line)
  }
  prepend.sort((a, b) => a.pos - b.pos)
  if (prepend.length) lines.value = prepend.concat(lines.value)
  if (append.length) lines.value = lines.value.concat(append)
  trimBuffer()

  if (!isPagedBack) {
    if (following.value) {
      nextTick(() => scrollToBottom())
    } else if (append.length) {
      pendingNew.value += append.filter(passesClientFilter).length
    }
  }
}

// ============ 数据加载 ============
async function loadFiles() {
  try {
    const res = await AdminApi.getLogFiles()
    if (res.success && res.data) {
      files.value = Array.isArray(res.data.files) ? res.data.files : []
      logDir.value = res.data.directory || ''
      if (files.value.length && !files.value.some((f) => f.name === file.value)) {
        file.value = files.value[0].name
      }
    }
  } catch (e) {
    files.value = []
  }
}

async function handleMount() {
  reqSeq += 1
  const seq = reqSeq
  loading.value = true
  error.value = null
  lines.value = []
  cursor.value = null
  hasMoreOlder.value = true
  selectedUid.value = null
  selectedLine.value = null
  highlightUid.value = null
  pendingNew.value = 0
  following.value = true
  scrollTop.value = 0
  try {
    const res = await AdminApi.getLogsTail({ file: file.value, lines: INITIAL_LINES })
    if (seq !== reqSeq) return
    const data = (res && res.data) || {}
    cursor.value = data.next_offset
    hasMoreOlder.value = !!data.has_more
    addLines((data.lines || []).map(normalizeLine), false)
    await nextTick()
    measureViewport()
    scrollToBottom()
    if (locateInput.value) locate(locateInput.value)
  } catch (err) {
    if (seq !== reqSeq) return
    error.value = { title: errorTitleFor(err), message: (err && err.message) || '未知错误' }
  } finally {
    if (seq === reqSeq) {
      loading.value = false
      schedulePoll()
    }
  }
}

async function reload() {
  await loadFiles()
  await handleMount()
}

function onFileChange() {
  locateInput.value = ''
  handleMount()
}

// ============ 增量轮询 ============
function schedulePoll() {
  if (pollTimer) clearTimeout(pollTimer)
  pollTimer = setTimeout(onPoll, POLL_MS)
}

async function onPoll() {
  if (locating.value || cursor.value === null) {
    schedulePoll()
    return
  }
  const seq = reqSeq
  try {
    const res = await AdminApi.getLogsTail({
      file: file.value,
      lines: FETCH_LINES,
      offset: cursor.value,
    })
    if (seq !== reqSeq) return
    const data = (res && res.data) || {}
    cursor.value = data.next_offset
    addLines((data.lines || []).map(normalizeLine), false)
  } catch (e) {
    // 轮询失败静默：保留现有视图与游标，下轮重试补拉
  } finally {
    if (seq === reqSeq) schedulePoll()
  }
}

// ============ 向前翻页 ============
async function loadOlder() {
  if (!canLoadOlder.value || olderLoading.value) return
  olderLoading.value = true
  const anchor = lines.value[0].pos
  const seq = reqSeq
  try {
    const res = await AdminApi.getLogsTail({
      file: file.value,
      lines: FETCH_LINES,
      before: anchor,
      level: serverLevel.value || '',
      keyword: keyword.value || '',
    })
    if (seq !== reqSeq) return
    const data = (res && res.data) || {}
    const older = (data.lines || []).map(normalizeLine)
    const prevScroll = viewportRef.value ? viewportRef.value.scrollTop : 0
    addLines(older, true)
    hasMoreOlder.value = !!data.has_more
    // 头部插行后平移滚动位置，保持当前阅读内容不跳动
    if (older.length && viewportRef.value) {
      await nextTick()
      viewportRef.value.scrollTop = prevScroll + older.length * ROW_HEIGHT
    }
  } catch (err) {
    ElMessage.error('加载更早日志失败：' + ((err && err.message) || '未知错误'))
  } finally {
    olderLoading.value = false
  }
}

// ============ 时间定位 ============
function normalizeLocateTime(input) {
  const s = String(input == null ? '' : input).trim()
  if (!s) return ''
  if (/^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}$/.test(s)) {
    return s.replace('T', ' ').slice(0, 19)
  }
  const d = new Date(s)
  if (Number.isNaN(d.getTime())) return ''
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

async function locate(target) {
  const t = normalizeLocateTime(target === undefined ? locateInput.value : target)
  if (!t) {
    ElMessage.warning('请输入合法时间（如 2026-10-25 09:00:01 或 ISO 时间）')
    return
  }
  locateInput.value = t
  if (locating.value) return
  locating.value = true
  highlightUid.value = null
  if (following.value) following.value = false
  pendingNew.value = 0
  let guard = 0
  try {
    // 目标时间早于当前最旧行时持续向前翻页，单轮无进展即停止
    while (
      displayLines.value.length
      && timeKey(displayLines.value[0].time) > t
      && canLoadOlder.value
      && guard < 40
    ) {
      const frontBefore = lines.value[0].pos
      await loadOlder()
      guard += 1
      if (lines.value.length && lines.value[0].pos === frontBefore) break
    }
    let idx = -1
    for (let i = 0; i < displayLines.value.length; i += 1) {
      if (timeKey(displayLines.value[i].time) >= t) {
        idx = i
        break
      }
    }
    if (idx < 0) {
      ElMessage.info('未能在已载入日志中找到该时间（可能早于缓冲或尚未产生）')
      return
    }
    const line = displayLines.value[idx]
    await nextTick()
    if (viewportRef.value) viewportRef.value.scrollTop = idx * ROW_HEIGHT
    scrollTop.value = idx * ROW_HEIGHT
    highlightUid.value = line.uid
    selectedUid.value = line.uid
    selectedLine.value = line
    if (highlightTimer) clearTimeout(highlightTimer)
    highlightTimer = setTimeout(() => { highlightUid.value = null }, 4000)
  } finally {
    locating.value = false
  }
}

// ============ 虚拟滚动 ============
function measureViewport() {
  const el = viewportRef.value
  if (el) viewportHeight.value = el.clientHeight
}

function onScroll() {
  const el = viewportRef.value
  if (!el) return
  scrollTop.value = el.scrollTop
  const threshold = ROW_HEIGHT * 3
  const nearBottom = el.scrollTop + el.clientHeight >= el.scrollHeight - threshold
  if (nearBottom) {
    following.value = true
    pendingNew.value = 0
  } else if (following.value) {
    following.value = false
  }
}

function scrollToBottom() {
  const el = viewportRef.value
  if (el) el.scrollTop = el.scrollHeight
}

function resumeFollowing() {
  following.value = true
  pendingNew.value = 0
  nextTick(() => scrollToBottom())
}

// ============ 过滤交互 ============
function setAllLevels() {
  checkedLevels.value = LEVEL_ORDER.slice()
}

function setWarnPlus() {
  checkedLevels.value = ['WARN', 'ERROR', 'CRITICAL']
}

function setErrorPlus() {
  checkedLevels.value = ['ERROR', 'CRITICAL']
}

function onKeywordInput() {
  if (debounceTimer) clearTimeout(debounceTimer)
  debounceTimer = setTimeout(() => {
    keyword.value = keywordInput.value.trim().toLowerCase()
    nextTick(() => {
      if (following.value) scrollToBottom()
    })
  }, 300)
}

function onCallerClick(row) {
  callerFilter.value = callerFilter.value === row.item.caller ? '' : row.item.caller
}

function onTimeClick(row) {
  locateInput.value = row.item.time
  locate(row.item.time)
}

function onRowSelect(uid) {
  selectedUid.value = uid
  selectedLine.value = lines.value.find((l) => l.uid === uid) || null
}

// ============ 复制 ============
function copyLine(line) {
  copyText(`${line.time} ${line.level} ${line.caller} ${line.message}${line.extra ? ' ' + line.extra : ''}`)
}

function copyView() {
  const text = displayLines.value
    .map((l) => `${l.time} ${l.level} ${l.caller} ${l.message}${l.extra ? ' ' + l.extra : ''}`)
    .join('\n')
  copyText(text)
}

function copyText(text) {
  const fallback = () => {
    const ta = document.createElement('textarea')
    ta.value = text
    ta.setAttribute('readonly', '')
    ta.style.position = 'fixed'
    ta.style.opacity = '0'
    document.body.appendChild(ta)
    ta.select()
    let ok = false
    try {
      ok = document.execCommand('copy')
    } catch (e) {
      ok = false
    }
    document.body.removeChild(ta)
    return ok
  }
  let done = Promise.resolve(false)
  if (navigator.clipboard && navigator.clipboard.writeText) {
    done = navigator.clipboard.writeText(text).then(() => true).catch(() => fallback())
  } else {
    done = Promise.resolve(fallback())
  }
  done.then((ok) => {
    if (ok) ElMessage.success('已复制到剪贴板')
    else ElMessage.warning('复制失败')
  })
}

// ============ 生命周期 ============
onMounted(async () => {
  resizeObserver = new ResizeObserver(measureViewport)
  if (viewportRef.value) resizeObserver.observe(viewportRef.value)
  await loadFiles()
  if (files.value.length === 0) {
    loading.value = false
    return
  }
  await handleMount()
  // 视图就绪后再观察一次（viewport 在数据渲染后才稳定）
  await nextTick()
  if (viewportRef.value && resizeObserver) resizeObserver.observe(viewportRef.value)
  measureViewport()
})

onBeforeUnmount(() => {
  reqSeq += 1
  if (pollTimer) clearTimeout(pollTimer)
  if (debounceTimer) clearTimeout(debounceTimer)
  if (highlightTimer) clearTimeout(highlightTimer)
  if (resizeObserver) resizeObserver.disconnect()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-logs {
  min-height: 0;

  .log-dir {
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: @font-family-mono;
    font-size: @font-size-xs;
    color: @text-color-placeholder;
  }

  .log-toolbar {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
    margin-bottom: 10px;
    flex-shrink: 0;

    .toolbar-group {
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .toolbar-grow {
      flex: 1 1 200px;
    }

    .toolbar-label {
      flex-shrink: 0;
      color: @text-color-placeholder;
      font-size: @font-size-xs;
    }

    .file-select {
      width: 190px;
    }

    .file-option-name {
      float: left;
    }

    .file-option-size {
      float: right;
      color: @text-color-placeholder;
      font-size: @font-size-xs;
    }

    .level-checks {
      margin-left: 2px;
    }

    .keyword-input {
      max-width: 280px;
    }
  }

  .conn-error {
    margin-bottom: 12px;
  }

  .log-status {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    color: @text-color-placeholder;
    font-size: @font-size-xs;
    margin-bottom: 6px;
    flex-shrink: 0;

    .status-right {
      flex-shrink: 0;
    }
  }

  .log-main {
    position: relative;
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    background: @bg-color;
    border: 1px solid @border-color-light;
    border-radius: @content-radius;
    overflow: hidden;
  }

  .log-viewport {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    overflow-x: hidden;
  }

  .log-phantom {
    position: relative;
    width: 100%;
  }

  .log-window {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    will-change: transform;
  }

  .log-row {
    display: flex;
    align-items: center;
    box-sizing: border-box;
    padding: 0 8px;
    font-size: @font-size-xs;
    font-family: @font-family-mono;
    border-bottom: 1px solid var(--el-fill-color-lighter);
    cursor: pointer;
    overflow: hidden;
    white-space: nowrap;
    color: @text-color;

    &:hover {
      background: var(--el-fill-color-light);
    }

    &.log-row--selected {
      background: var(--el-color-primary-light-9);
    }

    &.log-row--highlight {
      background: var(--el-color-warning-light-7);
    }
  }

  .log-col {
    flex-shrink: 0;
    box-sizing: border-box;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .log-col--time {
    width: 152px;
    color: @text-color-placeholder;
  }

  .log-col--level {
    width: 76px;
    text-align: center;
  }

  .log-col--caller {
    width: 170px;
    color: @info-color;
  }

  .log-col--msg {
    flex: 1;
    min-width: 0;
  }

  .lvl-TRACE {
    color: @text-color-placeholder;
  }

  .lvl-DEBUG {
    color: @success-color;
  }

  .lvl-INFO {
    color: @info-color;
  }

  .lvl-WARN {
    color: @warning-color;
    font-weight: 700;
  }

  .lvl-ERROR {
    color: @error-color;
    font-weight: 700;
  }

  .lvl-CRITICAL {
    color: #a8071a;
    font-weight: 700;
  }

  .log-empty-row {
    height: 96px;
    display: flex;
    align-items: center;
    justify-content: center;
    color: @text-color-placeholder;
    font-size: @font-size-sm;
  }

  .pause-bubble {
    position: absolute;
    left: 50%;
    bottom: 16px;
    transform: translateX(-50%);
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 6px 12px;
    background: @bg-color-dark;
    color: #fff;
    border-radius: 20px;
    box-shadow: @shadow-md;

    .bubble-text {
      font-size: @font-size-sm;
    }
  }

  .detail-card {
    position: absolute;
    left: 8px;
    right: 8px;
    bottom: 8px;
    z-index: 6;
    border-radius: @content-radius;

    :deep(.el-card__body) {
      padding: 10px 14px;
    }

    .detail-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      gap: 12px;
      margin-bottom: 8px;

      .detail-title {
        font-size: @font-size-sm;
        color: @text-color;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }

      .detail-actions {
        flex-shrink: 0;
        display: flex;
        gap: 8px;
      }
    }

    .detail-body {
      margin: 0;
      max-height: 180px;
      overflow: auto;
      white-space: pre-wrap;
      word-break: break-all;
      font-size: @font-size-xs;
      line-height: 1.6;
      color: @text-color;
      font-family: @font-family-mono;
    }
  }

  .log-footerbar {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-top: 10px;
    flex-shrink: 0;

    .locate-box {
      display: flex;
      align-items: center;
      gap: 6px;
    }

    .locate-input {
      width: 240px;
    }

    .locate-hint {
      color: @text-color-placeholder;
      font-size: @font-size-xs;
    }
  }
}
</style>
