<template>
  <!-- 日志查看（Web 二期 C1-C7）：虚拟滚动 + 实时跟随 + 级别/关键字/模块过滤 +
       向前翻页 + 时间定位（C7 从监测页跳转）。逻辑流为「旧→新、最新在下」
       （C5：反时间序阅读，追新在底部）；渲染仅可视区（C4 虚拟滚动）。
       数据面：GET /logs/tail（B-L1/b-L2 增量游标 offset / 向前翻页 before）。 -->
  <div class="log-viewer">
    <!-- 顶栏：文件视角 + 级别过滤 + 关键字 + 模块 + 动作 -->
    <div class="log-toolbar">
      <div class="toolbar-group">
        <span class="toolbar-label">日志文件</span>
        <el-radio-group v-model="viewFile" size="small" @change="switchFile">
          <el-radio-button label="main">主日志 collector.log</el-radio-button>
          <el-radio-button label="warning">警告 warning.log</el-radio-button>
        </el-radio-group>
      </div>
      <div class="toolbar-group">
        <span class="toolbar-label">级别</span>
        <el-button size="mini" :type="isAllLevels ? 'primary' : 'default'" plain @click="setAllLevels">全部</el-button>
        <el-button size="mini" :type="quickWarn ? 'warning' : 'default'" plain @click="setWarnPlus">仅警告及以上</el-button>
        <el-button size="mini" :type="quickError ? 'danger' : 'default'" plain @click="setErrorPlus">仅错误及以上</el-button>
        <el-checkbox-group v-model="checkedLevels" size="mini" class="level-checks">
          <el-checkbox-button v-for="lv in LEVEL_ORDER" :key="lv" :label="lv">{{ lv }}</el-checkbox-button>
        </el-checkbox-group>
      </div>
      <div class="toolbar-group toolbar-grow">
        <el-input v-model="keywordInput" size="mini" clearable placeholder="关键字即时过滤（可传服务端回扫）"
          class="keyword-input" @input="onKeywordInput" />
      </div>
      <div class="toolbar-group">
        <el-tag v-if="moduleFilter" closable size="mini" class="module-tag" @close="clearModule">
          模块 [{{ moduleFilter }}]
        </el-tag>
      </div>
      <div class="toolbar-group">
        <el-button size="mini" @click="copyView">复制视图</el-button>
        <el-tooltip content="切换到另一份文件会重置已载入日志" placement="top">
          <span class="view-file-hint">{{ viewFile === 'warning' ? 'warning.log 天然即「警告及以上」' : '主日志' }}</span>
        </el-tooltip>
      </div>
    </div>

    <template v-if="loading && rowsNotReady">
      <el-progress class="conn-loading-bar" :percentage="100" :indeterminate="true"
        :duration="3" :stroke-width="2" :show-text="false" />
      <skeleton-list :rows="4" />
    </template>

    <el-alert v-else-if="error" class="conn-error" type="error" :title="error.title"
      :description="error.message" show-icon :closable="false" />

    <template v-else-if="rowsReady">
      <div class="log-status">
        <span>已连接 {{ viewFile === 'warning' ? 'warning.log' : 'collector.log' }} · {{ displayLines.length }} 行可见
          {{ following ? '· 跟随最新' : '· 已暂停跟随（上翻阅读中）' }}</span>
        <span class="status-right">每 {{ pollMs / 1000 }} 秒增量拉取 · 已缓存 {{ allLines.length }} / 最多 {{ MAX_BUFFER_LINES }} 行（满则移除最早行）</span>
      </div>

      <!-- 虚拟滚动视图 -->
      <div ref="viewport" class="log-viewport" @scroll="onScroll">
        <!-- C4 虚拟滚动：占位高度 = 全部行数 × 行高；仅渲染可视窗口切片 -->
        <div class="log-phantom" :style="{ height: phantomHeight + 'px' }">
          <div class="log-window" :style="{ transform: 'translateY(' + offsetY + 'px)' }">
            <div
              v-for="row in visibleRows"
              :key="row.uid"
              class="log-row"
              :style="{ height: rowHeight + 'px' }"
              :class="[
                'log-row--' + row.item.level.toLowerCase(),
                { 'log-row--selected': selectedUid === row.uid, 'log-row--highlight': highlightUid === row.uid },
              ]"
              @click="onRowSelect(row.uid)"
            >
              <!-- C5 时间/级别/模块/消息分列；时间点选 = 定位过滤条件（C5）；模块点击 = 筛选（C6） -->
              <span class="log-col log-col--time"
                :title="row.item.time + '（点击定位到该时间）'" @click.stop="onTimeClick(row)">{{ row.item.time }}</span>
              <span class="log-col log-col--level" :class="'lvl-' + row.item.level">{{ row.item.level }}</span>
              <span class="log-col log-col--module"
                :title="'点击按模块 [ ' + row.item.module + ' ] 筛选'" @click.stop="onModuleClick(row)">[{{ row.item.module }}]</span>
              <span class="log-col log-col--msg" :title="row.item.message">{{ row.item.message }}</span>
            </div>
            <div v-if="visibleRows.length === 0" class="log-empty-row">
              <span>无可显示日志行（当前过滤条件下）</span>
            </div>
          </div>
        </div>
      </div>

      <!-- 全局操作行：加载更早 / 时间定位 -->
      <div class="log-footerbar">
        <el-button size="mini" :disabled="!canLoadOlder || olderLoading" :loading="olderLoading"
          @click="loadOlder">
          {{ canLoadOlder ? '加载更早日志' : '已到日志起点' }}
        </el-button>
        <div class="locate-box">
          <el-input v-model="locateInput" size="mini" placeholder="定位时间，如 2026-10-20 09:00:01"
            class="locate-input" @keyup.enter.native="locate" />
          <el-button size="mini" type="primary" :loading="locating" @click="locate">定位</el-button>
        </div>
        <span class="locate-hint">时间点选/监测页跳转自动定位（C7）</span>
      </div>

      <!-- C1 暂停跟随气泡 -->
      <transition name="el-fade-in">
        <div v-if="!following && pendingNew > 0" class="pause-bubble">
          <span class="bubble-text">已暂停，{{ pendingNew }} 条新日志</span>
          <el-button size="mini" type="primary" @click="resumeFollowing">回到底部继续跟随</el-button>
        </div>
      </transition>

      <!-- C5 单行详情条：完整 message + extra + suggestion + 一键复制 -->
      <el-card v-if="selectedLine" class="detail-card" shadow="never">
        <div slot="header" class="detail-header">
          <span>{{ selectedLine.time }} · {{ selectedLine.level }} · [{{ selectedLine.module }}]{{ selectedLine.ds_id ? '[' + selectedLine.ds_id + ']' : '' }}</span>
          <span class="detail-actions">
            <el-button size="mini" @click="copyLine(selectedLine)">复制该行</el-button>
            <el-button size="mini" @click="selectedLine = null">收起</el-button>
          </span>
        </div>
        <pre class="detail-body">{{ detailBody }}</pre>
      </el-card>
    </template>
  </div>
</template>

<script>
// 日志查看（Web 二期 C1-C7，逐条落实见模板/方法注释）。
//
// 数据模型：
//   allLines   —— 已拉取的原始行缓冲（旧→新，最新在下；MAX_BUFFER_LINES 封顶，超出从头部
//                裁剪保内存有界——C4「行数受控上限」）。
//   followCursor —— 增量游标（B-L1 next_offset）：每轮轮询传 offset=followCursor 只拉新行，
//                新行恒在最新卷尾部追加、旧游标轮转后仍正确（B-L1 旋转语义）。
//   displayLines —— allLines 经客户端过滤（级别复选 + 关键字 + 模块）后的可见行（computed）。
//   before 翻页   —— 「加载更早」传 before=<最旧行 pos>，服务端返回该锚点之前的行（B-L1；
//                打开日志时同步传当前级别下界 level 与关键字 keyword 作服务端回扫——C2/C3
//                双保险：服务端预缩小范围、客户端再精确过滤）。
//
// C1 实时滚动：默认跟随最新——轮询增量拉取、新行自动追加底部且滚动保持贴底；滚动离开底部
//   自动暂停跟随（轮询继续、只计数）；气泡「已暂停，N 条新日志」一键回底续跟。
// C2 级别过滤：快速一键「仅警告及以上 / 仅错误及以上」+ 6 级别复选（子集）；客户端精确过滤
//   + 服务端 level（选中集最低级别作阈值）双保险。
// C3 关键字：输入框即时（300ms 防抖）内存过滤已拉取行；「加载更早」时传 keyword 服务端回扫。
// C4 虚拟滚动：可视区仅渲染 (可视行数 + overscan) 行，行高固定 24px（含新增不跳、滚动稳定）。
// C5 反时间序（最新在下）+ 级别着色（WARN 黄/ERROR 红/CRITICAL 深红）+ 分列 + 单行详情条
//   （完整 message+extra+suggestion，一键复制）+ 时间点选为定位过滤条件。
// C6 免下载浏览：全程在线；模块 [ES] 点击筛选（与关键字/级别同过滤管道）；「复制视图」文本导出。
// C7 监测联动：支持 ?t=<时间> 查询参数（监测页“最近采集/开始时间”跳转），本页把 UTC ISO
//   归一为日志本地时间串后向前翻页定位到该时间上下文并高亮。
//
// 请求生命周期：M-2 _reqSeq 序号守卫 + L-1 AbortController（与既有页面同一约定）；
// 轮询 created 起调度、setTimeout 链式（pollMs=2500，B-L2 建议 2-3s）：上一请求
// 结束（含失败）后才调度下一轮，避免 setInterval 在慢请求下自我 abort/堆积；
// beforeDestroy 清除。
import {
  getLogsTail,
  readStoredApiBase,
  validateAndNormalizeApiBase,
  writeStoredApiBase,
  API_BASE_CHANGED_EVENT,
} from '../api/index.js'
import SkeletonList from '../components/SkeletonList.vue'

// 级别序（与服务端 _LEVEL_ORDER 同源；WARNING/FATAL 已归一）
const LEVEL_ORDER = ['TRACE', 'DEBUG', 'INFO', 'WARN', 'ERROR', 'CRITICAL']
const LEVEL_RANK = { TRACE: 0, DEBUG: 1, INFO: 2, WARN: 3, ERROR: 4, CRITICAL: 5 }

// 行高（虚拟滚动依赖固定行高；改行高必须同步改 .log-row 样式）
const ROW_HEIGHT = 24
// 可视区上下各多渲染的补偿行（避免快速滚动露白）
const OVERSCAN = 20
// 缓冲封顶（C4 行数受控）
const MAX_BUFFER_LINES = 20000
// 首屏/翻页/轮询的拉取行数
const INITIAL_LINES = 200
const FETCH_LINES = 300
// 轮询间隔（B-L2 默认 ~2-3s）
const POLL_MS = 2500

// 首物理行时间 → 可字典比较的 YYYY-MM-DD HH:MM:SS（本地日志时间格式同源）
function timeKey(text) {
  return String(text || '').replace('T', ' ').slice(0, 19)
}

export default {
  name: 'LogViewer',
  components: { SkeletonList },
  data() {
    return {
      LEVEL_ORDER,
      MAX_BUFFER_LINES,
      rowHeight: ROW_HEIGHT,
      pollMs: POLL_MS,
      apiBaseInput: '',
      viewFile: 'main',
      rows: null, // 初始 tail 响应（rowsReady 前置）
      error: null,
      // 拉取缓冲与游标
      allLines: [],
      followCursor: null,
      hasMoreOlder: true,
      // 过滤器
      checkedLevels: LEVEL_ORDER.slice(),
      keywordInput: '',
      keyword: '',
      moduleFilter: '',
      // 虚拟滚动
      scrollTop: 0,
      viewportHeight: 0,
      // 跟随状态
      following: true,
      pendingNew: 0,
      // 交互状态
      selectedUid: null,
      selectedLine: null,
      highlightUid: null,
      olderLoading: false,
      locating: false,
      locateInput: '',
      loadingOlder: false,
    }
  },
  computed: {
    rowsNotReady() {
      return this.rows === null
    },
    rowsReady() {
      return this.rows !== null
    },
    loading() {
      return this.rowsNotReady
    },
    // 客户端过滤后的可见行（C2/C3/C6：级别复选 + 关键字 + 模块）
    displayLines() {
      const kw = this.keyword.toLowerCase()
      return this.allLines.filter((line) => {
        if (!this.checkedLevels.includes(line.level)) return false
        if (this.moduleFilter && line.module !== this.moduleFilter) return false
        if (kw) {
          const hay1 = String(line.raw || '').toLowerCase()
          const hay2 = String(line.message || '').toLowerCase()
          if (hay1.indexOf(kw) < 0 && hay2.indexOf(kw) < 0) return false
        }
        return true
      })
    },
    phantomHeight() {
      return Math.max(this.displayLines.length, 1) * ROW_HEIGHT
    },
    // 可视窗口首行下标（虚拟滚动切片）
    sliceStart() {
      const row = Math.floor(this.scrollTop / ROW_HEIGHT)
      return Math.max(0, row - OVERSCAN)
    },
    sliceEnd() {
      const rowsVisible = Math.ceil(this.viewportHeight / ROW_HEIGHT)
      return Math.min(this.displayLines.length, this.sliceStart + rowsVisible + OVERSCAN * 2)
    },
    visibleRows() {
      const start = Math.max(0, this.sliceStart)
      const end = Math.max(start, Math.min(this.displayLines.length, this.sliceEnd))
      const rows = []
      for (let i = start; i < end; i += 1) {
        const line = this.displayLines[i]
        if (line) rows.push({ uid: line.uid, item: line })
      }
      return rows
    },
    offsetY() {
      return this.sliceStart * ROW_HEIGHT
    },
    canLoadOlder() {
      if (this.allLines.length === 0) return false
      const front = this.allLines[0]
      return front.pos > 0
    },
    isAllLevels() {
      return this.checkedLevels.length === LEVEL_ORDER.length
    },
    quickWarn() {
      return this.levelSetEqual(['WARN', 'ERROR', 'CRITICAL'])
    },
    quickError() {
      return this.levelSetEqual(['ERROR', 'CRITICAL'])
    },
    // 服务端 level 参数（阈值 = 选中集最低级别；整集/空集不传）
    serverLevel() {
      if (this.checkedLevels.length === 0) return ''
      if (this.checkedLevels.length === LEVEL_ORDER.length) return ''
      let minRank = Infinity
      for (const lv of this.checkedLevels) {
        const rank = LEVEL_RANK[lv]
        if (rank === undefined) continue
        if (rank < minRank) minRank = rank
      }
      if (!Number.isFinite(minRank)) return ''
      return LEVEL_ORDER[minRank]
    },
    detailBody() {
      const line = this.selectedLine
      if (!line) return ''
      const parts = [
        `${line.time} ${line.level} [${line.module}${line.ds_id ? '][' + line.ds_id : ''}]`,
      ]
      if (line.file && line.line) parts.push(`${line.file}:${line.line}`)
      parts.push(String(line.message || ''))
      if (line.extra) parts.push(String(line.extra))
      if (line.suggestion) parts.push(String(line.suggestion))
      return parts.join('  ')
    },
  },
  created() {
    this._reqSeq = 0
    this._debounceTimer = null
    this._highlightTimer = null
    this.apiBaseInput = readStoredApiBase()
    // 页头 api-base 变更监听（App.vue 广播；页头为唯一编辑入口，变更后重建视图重取）
    this._onApiBaseChanged = (evt) => {
      this.apiBaseInput = (evt && evt.detail && evt.detail.base) || ''
      this.handleMount()
    }
    window.addEventListener(API_BASE_CHANGED_EVENT, this._onApiBaseChanged)
    this.viewFile = (String(this.$route.query && this.$route.query.file) === 'warning')
      ? 'warning' : 'main'
    // C7：监测页跳转携带 ?t= 时，进入自动定位
    const target = this.$route.query && this.$route.query.t
    this.locateInput = this.normalizeLocateTime(target)
    this.handleMount()
  },
  mounted() {
    this.measureViewport()
    window.addEventListener('resize', this.measureViewport)
  },
  beforeDestroy() {
    this._reqSeq += 1
    if (this._controller) this._controller.abort()
    if (this._timer) window.clearTimeout(this._timer)
    if (this._debounceTimer) window.clearTimeout(this._debounceTimer)
    if (this._highlightTimer) window.clearTimeout(this._highlightTimer)
    window.removeEventListener('resize', this.measureViewport)
    window.removeEventListener(API_BASE_CHANGED_EVENT, this._onApiBaseChanged)
  },
  methods: {
    // ---- 初始化 / 切文件 ----
    async handleMount() {
      this._reqSeq += 1
      if (this._controller) this._controller.abort()
      this._controller = new AbortController()
      const seq = this._reqSeq
      this.rows = null
      this.error = null
      this.allLines = []
      this.followCursor = null
      this.hasMoreOlder = true
      this.syncBase()
      try {
        const data = await this.fetchTail(INITIAL_LINES, this._controller.signal)
        if (seq !== this._reqSeq) return
        this.rows = data
        this.followCursor = data.next_offset
        this.hasMoreOlder = !!data.has_more
        this.addLines((data.lines || []).map((l) => this.normalizeLine(l)), false)
        this.$nextTick(() => {
          this.scrollToBottom()
          if (this.locateInput) {
            this.locate(this.locateInput)
          }
        })
      } catch (err) {
        if (seq !== this._reqSeq) return
        this.rows = {}
        this.error = {
          title: this.errorTitleFor(err),
          message: err && err.message ? err.message : '未知错误',
        }
      }
      // 轮询（setTimeout 链式：上一请求完成后才调度下一轮；重新计数避免重复定时器）
      this._schedulePoll()
    },
    switchFile(file) {
      // el-radio-group @change 在 v-model 已更新后触发，此处 file 恒等于 viewFile；
      // 仅真正切换（change 本就不会在重复选中时触发）时重建视图即可。
      this.viewFile = file
      this.handleMount()
    },
    syncBase() {
      const validated = validateAndNormalizeApiBase(this.apiBaseInput)
      if (!validated.ok) {
        this.error = { title: '输入校验失败', message: validated.message }
        return null
      }
      const base = validated.base
      if (base) this.apiBaseInput = base
      writeStoredApiBase(base)
      return base
    },
    currentBase() {
      const validated = validateAndNormalizeApiBase(this.apiBaseInput)
      return validated.ok ? validated.base : ''
    },
    fetchTail(lines, signal) {
      return getLogsTail('tail', { file: this.viewFile, lines }, {
        base: this.currentBase(),
        signal: this._controller ? this._controller.signal : signal,
      })
    },
    normalizeLine(line) {
      // uid：首物理行 pos 在整个流内唯一稳定（B-L1 游标锚点；被虚拟列表当 key）
      return {
        uid: String('p' + line.pos),
        time: line.time,
        level: line.level || 'INFO',
        module: line.module || 'SYS',
        ds_id: line.ds_id,
        file: line.file,
        line: line.line,
        message: line.message,
        extra: line.extra,
        suggestion: line.suggestion,
        raw: line.raw,
        pos: line.pos,
      }
    },
    // 追加新行（旧→新）；dedupe 依据：行 pos 必须大于游标（被轮询保证在游标之后）
    addLines(lines, isPagedBack) {
      const first = this.allLines.length ? this.allLines[0].pos : -1
      const last = this.allLines.length
        ? this.allLines[this.allLines.length - 1].pos
        : -1
      let prepend = []
      let append = []
      for (const line of lines) {
        if (!line || line.pos === undefined || line.pos === null) continue
        if (!isPagedBack && line.pos <= last) continue // 增量去重
        if (isPagedBack && line.pos >= first) continue // 向前翻页去重
        if (isPagedBack) prepend.unshift(line)
        else append.push(line)
      }
      if (prepend.length) this.allLines = prepend.concat(this.allLines)
      if (append.length) this.allLines = this.allLines.concat(append)
      // C4/上限：实时追加与向前翻页后统一裁剪；达到最大行数时最早的行（头部）移除
      this._trimBuffer()
      // 跟随态：追加后回到底部；暂停态：仅累计可见新行数（气泡）
      if (!isPagedBack) {
        if (this.following) {
          this.$nextTick(() => this.scrollToBottom())
        } else if (append.length) {
          let visible = 0
          for (const line of append) {
            if (this.passesClientFilter(line)) visible += 1
          }
          this.pendingNew += visible
        }
      }
    },
    passesClientFilter(line) {
      if (!this.checkedLevels.includes(line.level)) return false
      if (this.moduleFilter && line.module !== this.moduleFilter) return false
      const kw = this.keyword.toLowerCase()
      if (kw) {
        const hay1 = String(line.raw || '').toLowerCase()
        const hay2 = String(line.message || '').toLowerCase()
        if (hay1.indexOf(kw) < 0 && hay2.indexOf(kw) < 0) return false
      }
      return true
    },
    // #3 最大行数上限：达到 MAX_BUFFER_LINES 时从头部移除最早行，保证内存有界、
    // 页面始终只保留最近 N 行（实时更新下最早的项随时间被裁掉，为有意为之）。
    _trimBuffer() {
      const overflow = this.allLines.length - MAX_BUFFER_LINES
      if (overflow > 0) this.allLines.splice(0, overflow)
    },

    // ---- 轮询（C1 增量跟随；setTimeout 链式）----
    // 调度下一轮：仅在上轮 onPoll 已结束（成功或失败）后触发。setTimeout 链式
    // 保证同一时刻至多一个在途请求；单请求若耗时超过 pollMs，也不会像
    // setInterval 那样由下一轮把在途请求 abort（放缓后一旦请求慢于轮询间隔，
    // 会陷入「请求常发、每个都被自身下一轮取消、永不追加」的循环，见 #2）。
    // 轮询复用 handleMount 重建的 controller：切文件/卸载时由 handleMount 与
    // beforeDestroy abort，seq 守卫丢弃过期结果。
    _schedulePoll() {
      if (this._timer) window.clearTimeout(this._timer)
      this._timer = window.setTimeout(() => this.onPoll(), POLL_MS)
    },
    async onPoll() {
      if (this.locating) { this._schedulePoll(); return }
      if (this.followCursor === undefined || this.followCursor === null) {
        this._schedulePoll(); return
      }
      const seq = this._reqSeq
      try {
        const data = await getLogsTail('follow', {
          file: this.viewFile, lines: FETCH_LINES, offset: this.followCursor,
        }, { base: this.currentBase(), signal: this._controller ? this._controller.signal : undefined })
        if (seq !== this._reqSeq) return
        const newLines = data.lines || []
        this.followCursor = data.next_offset
        this.hasMoreOlder = this.hasMoreOlder || !!data.has_more
        this.addLines(newLines.map((l) => this.normalizeLine(l)), false)
      } catch (err) {
        // 轮询失败静默（保留现有视图与游标）；失败不中断轮询——下轮用同一
        // followCursor 重试补拉，恢复后积压行仍会追加。请求级 seq 丢弃过期结果。
        void err
      } finally {
        // 组件未销毁/未切源（seq 未变）才调度下一轮；成功与失败均继续轮询
        if (seq === this._reqSeq) this._schedulePoll()
      }
    },

    // ---- 向前翻页（加载更早 / 服务端回扫 C2/C3）----
    loadOlder() {
      if (!this.canLoadOlder || this.olderLoading) return Promise.resolve()
      this.olderLoading = true
      const anchor = this.allLines[0].pos
      const signal = new AbortController().signal
      const seq = this._reqSeq
      return getLogsTail('older', {
        file: this.viewFile,
        lines: FETCH_LINES,
        before: anchor,
        level: this.serverLevel || '',
        keyword: this.keyword || '',
      }, { base: this.currentBase(), signal })
        .then((data) => {
          if (seq !== this._reqSeq) return
          const older = (data.lines || []).map((l) => this.normalizeLine(l))
          const count = older.length
          const prevScroll = this.$refs.viewport ? this.$refs.viewport.scrollTop : 0
          this.addLines(older, true)
          this.hasMoreOlder = !!data.has_more
          // 头部插行后平移滚动量，保持当前视口内容不跳
          if (count && this.$refs.viewport) {
            this.$nextTick(() => {
              this.$refs.viewport.scrollTop = prevScroll + count * ROW_HEIGHT
            })
          }
        })
        .catch((err) => {
          this.$message({ type: 'error', message: '加载更早日志失败：' + (err && err.message ? err.message : '未知错误') })
        })
        .finally(() => {
          this.olderLoading = false
        })
    },

    // ---- 时间定位（C5 点选 / C7 监测跳转）----
    normalizeLocateTime(input) {
      const s = String(input == null ? '' : input).trim()
      if (!s) return ''
      if (/^\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}:\d{2}$/.test(s)) {
        return s.replace('T', ' ').slice(0, 19)
      }
      const d = new Date(s)
      if (Number.isNaN(d.getTime())) return ''
      const p = (n) => String(n).padStart(2, '0')
      return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
    },
    async locate(target) {
      let t = target
      if (t === undefined) t = this.normalizeLocateTime(this.locateInput)
      else t = this.normalizeLocateTime(t)
      if (!t) {
        this.$message({ type: 'warning', message: '请输入合法时间（如 2026-10-20 09:00:01 或 ISO 时间）' })
        return
      }
      if (t !== this.locateInput) this.locateInput = t
      this.locating = true
      this.highlightUid = null
      // 上翻阅读期间暂停跟随（定位到历史上下文，不被打断）
      if (this.following) this.following = false
      this.pendingNew = 0
      let guard = 0
      try {
        // 时间太旧需向前翻页直至可视最旧行 ≤ 目标时间（或到流起点）；单轮无进展即停
        while (this.allLines.length
          && timeKey(this.displayLines.length ? this.displayLines[0].time : this.allLines[0].time) > t
          && this.canLoadOlder && guard < 40) {
          const frontBefore = this.allLines[0].pos
          await this.loadOlder()
          guard += 1
          if (this.allLines.length && this.allLines[0].pos === frontBefore) break
        }
        // 在可见行中找第一个 time >= t 的行
        let idx = -1
        for (let i = 0; i < this.displayLines.length; i += 1) {
          if (timeKey(this.displayLines[i].time) >= t) { idx = i; break }
        }
        if (idx < 0) {
          this.$message({ type: 'info', message: '未能在已载入日志中找到该时间（可能早于缓冲或尚未产生）' })
        } else {
          const line = this.displayLines[idx]
          this.$nextTick(() => {
            const el = this.$refs.viewport
            if (el) el.scrollTop = idx * ROW_HEIGHT
          })
          this.highlightUid = line.uid
          this.selectedUid = line.uid
          this.selectedLine = line
          if (this._highlightTimer) window.clearTimeout(this._highlightTimer)
          this._highlightTimer = window.setTimeout(() => { this.highlightUid = null }, 4000)
        }
      } finally {
        this.locating = false
      }
    },

    // ---- 虚拟滚动 ----
    measureViewport() {
      const el = this.$refs.viewport
      if (el) this.viewportHeight = el.clientHeight
    },
    onScroll() {
      const el = this.$refs.viewport
      if (!el) return
      this.scrollTop = el.scrollTop
      const threshold = ROW_HEIGHT * 3
      const nearBottom = (el.scrollTop + el.clientHeight) >= (el.scrollHeight - threshold)
      if (nearBottom) {
        // 贴底 → 恢复跟随并清气泡
        this.following = true
        this.pendingNew = 0
      } else if (this.following) {
        // 离开底部 → 暂停跟随（轮询继续、只计数）
        this.following = false
      }
    },
    scrollToBottom() {
      const el = this.$refs.viewport
      if (el) el.scrollTop = el.scrollHeight
    },
    resumeFollowing() {
      this.following = true
      this.pendingNew = 0
      this.$nextTick(() => this.scrollToBottom())
    },

    // ---- 过滤交互 ----
    levelSetEqual(arr) {
      const a = arr.slice().sort().join(',')
      const b = this.checkedLevels.slice().sort().join(',')
      return a === b
    },
    setAllLevels() {
      this.checkedLevels = LEVEL_ORDER.slice()
    },
    setWarnPlus() {
      this.checkedLevels = ['WARN', 'ERROR', 'CRITICAL']
    },
    setErrorPlus() {
      this.checkedLevels = ['ERROR', 'CRITICAL']
    },
    onKeywordInput() {
      if (this._debounceTimer) window.clearTimeout(this._debounceTimer)
      this._debounceTimer = window.setTimeout(() => {
        this.keyword = this.keywordInput.trim().toLowerCase()
      }, 300)
    },
    onModuleClick(row) {
      if (this.moduleFilter === row.item.module) this.moduleFilter = ''
      else this.moduleFilter = row.item.module
    },
    clearModule() {
      this.moduleFilter = ''
    },
    onTimeClick(row) {
      // C5 时间点选 = 定位过滤条件（高亮该时间上下文）
      this.locateInput = row.time
      this.locate(row.time)
    },
    onRowSelect(uid) {
      this.selectedUid = uid
      const line = this.allLines.find((l) => l.uid === uid)
      this.selectedLine = line || null
    },

    // ---- 复制 ----
    copyLine(line) {
      const text = this.detailBody
      this.copyText(line.time + ' ' + line.level + ' [' + line.module + '] ' + line.message
        + (line.extra ? '  ' + line.extra : '') + (line.suggestion ? '  ' + line.suggestion : ''))
    },
    copyView() {
      const text = this.displayLines.map((l) => {
        return `${l.time} ${l.level} [${l.module}] ${l.message}${l.extra ? '  ' + l.extra : ''}`
      }).join('\n')
      this.copyText(text)
    },
    copyText(text) {
      const fallback = () => {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.setAttribute('readonly', '')
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        let ok = false
        try { ok = document.execCommand('copy') } catch (e) { ok = false }
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
        this.$message({ type: ok ? 'success' : 'warning', message: ok ? '已复制到剪贴板' : '复制失败' })
      })
    },

    errorTitleFor(err) {
      const apiCode = err && err.apiCode
      if (apiCode === 'VALIDATION') return '输入校验失败'
      if (apiCode === 'TIMEOUT') return '连接超时'
      if (apiCode === 'HTTP') return '后端返回异常状态'
      if (apiCode === 'PARSE') return '响应解析失败'
      if (apiCode === 'NETWORK') return '网络错误或跨域拦截'
      return '连接失败'
    },
  },
}
</script>

<style scoped>
.log-viewer {
  padding: 8px 4px;
}

.log-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 12px;
  margin-bottom: 12px;
}

.toolbar-group {
  display: flex;
  align-items: center;
  gap: 6px;
}

.toolbar-grow {
  flex: 1 1 180px;
}

.toolbar-label {
  flex-shrink: 0;
  color: #606266;
  font-size: 12px;
}

.level-checks {
  margin-left: 2px;
}

.keyword-input {
  max-width: 240px;
}

.module-tag {
  cursor: default;
}

.view-file-hint {
  color: #c0c4cc;
  font-size: 12px;
}

.conn-loading-bar {
  margin-bottom: 4px;
}

.conn-error {
  margin-bottom: 12px;
}

.log-status {
  display: flex;
  justify-content: space-between;
  color: #909399;
  font-size: 12px;
  margin-bottom: 6px;
}

.status-right {
  color: #c0c4cc;
}

/* C4 虚拟滚动容器 */
.log-viewport {
  height: 520px;
  overflow-y: auto;
  border: 1px solid #ebeef5;
  border-radius: 4px;
  background: #fff;
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
  font-size: 12px;
  font-family: 'Consolas', 'Menlo', monospace;
  border-bottom: 1px solid #f7f7f7;
  cursor: pointer;
  overflow: hidden;
  white-space: nowrap;
  color: #303133;
}

.log-row:hover {
  background: #f5f7fa;
}

.log-row--selected {
  background: #ecf5ff;
}

.log-row--highlight {
  background: #fff3cd !important;
}

/* C5 级别着色：WARN 黄 / ERROR 红 / CRITICAL 深红 */
.log-row--warn .log-col--level {
  color: #e6a23c;
  font-weight: 700;
}

.log-row--error .log-col--level {
  color: #f56c6c;
  font-weight: 700;
}

.log-row--critical .log-col--level {
  color: #a8071a;
  font-weight: 700;
}

.lvl-TRACE {
  color: #909399;
}

.lvl-DEBUG {
  color: #67c23a;
}

.lvl-INFO {
  color: #409eff;
}

.lvl-WARN {
  color: #e6a23c;
  font-weight: 700;
}

.lvl-ERROR {
  color: #f56c6c;
  font-weight: 700;
}

.lvl-CRITICAL {
  color: #a8071a;
  font-weight: 700;
}

.log-col {
  flex-shrink: 0;
  box-sizing: border-box;
  overflow: hidden;
  text-overflow: ellipsis;
}

.log-col--time {
  width: 148px;
  color: #909399;
}

.log-col--level {
  width: 72px;
  text-align: center;
}

.log-col--module {
  width: 120px;
  color: #409eff;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.log-col--msg {
  flex: 1;
  min-width: 0;
  color: #303133;
}

.log-empty-row {
  height: 96px;
  display: flex;
  align-items: center;
  justify-content: center;
  color: #c0c4cc;
  font-size: 13px;
}

.log-footerbar {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 10px;
}

.locate-box {
  display: flex;
  align-items: center;
  gap: 6px;
}

.locate-input {
  width: 220px;
}

.locate-hint {
  color: #c0c4cc;
  font-size: 12px;
}

/* C1 暂停跟随气泡 */
.pause-bubble {
  position: fixed;
  left: 50%;
  bottom: 48px;
  transform: translateX(-50%);
  z-index: 20;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 14px;
  background: #303133;
  color: #fff;
  border-radius: 20px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.2);
}

.bubble-text {
  font-size: 13px;
}

/* C5 单行详情条 */
.detail-card {
  margin-top: 12px;
}

.detail-card ::v-deep .el-card__header {
  padding: 10px 16px;
}

.detail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  color: #303133;
  font-size: 13px;
}

.detail-actions {
  display: flex;
  gap: 8px;
}

.detail-body {
  margin: 0;
  white-space: pre-wrap;
  word-break: break-all;
  font-size: 13px;
  line-height: 1.6;
  color: #303133;
  max-height: 220px;
  overflow: auto;
  font-family: 'Consolas', 'Menlo', monospace;
}
</style>
