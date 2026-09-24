<template>
  <div class="admin-behavior-analytics">
    <div class="header-section">
      <div class="header-left">
        <h2>行为分析</h2>
        <p class="description">按访问 / 上传 / 下载三种行为分行聚合，支持按用户与按 IP 两种维度分析</p>
      </div>
      <div class="header-right">
        <el-button type="primary" size="small" :loading="loading" @click="loadAll">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </div>

    <!-- ============ 时间范围筛选 ============ -->
    <el-card shadow="never" class="filter-card">
      <div class="filter-row">
        <span class="filter-label">时间范围</span>
        <el-radio-group v-model="preset" size="default" @change="loadAll">
          <el-radio-button label="7d">近 7 天</el-radio-button>
          <el-radio-button label="30d">近 30 天</el-radio-button>
          <el-radio-button label="custom">自定义</el-radio-button>
        </el-radio-group>
        <el-date-picker
          v-if="preset === 'custom'"
          v-model="customRange"
          type="datetimerange"
          range-separator="至"
          start-placeholder="开始时间"
          end-placeholder="结束时间"
          :clearable="false"
          style="width: 360px"
          @change="loadAll"
        />
        <span class="filter-label">下载趋势粒度</span>
        <el-select v-model="granularity" size="default" style="width: 100px" @change="loadTrend">
          <el-option label="按日" value="day" />
          <el-option label="按周" value="week" />
          <el-option label="按月" value="month" />
        </el-select>
      </div>
    </el-card>

    <!-- ============ 访问 / 上传行为（基于最近操作记录，按用户/按IP） ============ -->
    <el-card
      v-for="def in behaviorDefs"
      :key="def.key"
      shadow="never"
      class="chart-card behavior-card"
      v-loading="behavior[def.key].loading"
    >
      <template #header>
        <div class="card-title">
          <span>{{ def.label }} <span class="sub">{{ def.desc }}</span></span>
          <el-radio-group
            :model-value="behavior[def.key].dim"
            size="small"
            @change="(v) => { behavior[def.key].dim = v; renderBehaviorChart(behavior[def.key]) }"
          >
            <el-radio-button label="user">按用户</el-radio-button>
            <el-radio-button label="ip">按 IP</el-radio-button>
          </el-radio-group>
        </div>
      </template>
      <div class="behavior-metrics">
        <div class="metric-card"><div class="metric-value">{{ behavior[def.key].summary.total }}</div><div class="metric-title">总次数</div></div>
        <div class="metric-card"><div class="metric-value">{{ behavior[def.key].summary.uniqueUsers }}</div><div class="metric-title">独立用户</div></div>
        <div class="metric-card"><div class="metric-value">{{ behavior[def.key].summary.uniqueIPs }}</div><div class="metric-title">独立 IP</div></div>
      </div>
      <div :ref="(el) => { if (el) behavior[def.key].el = el }" class="chart-box small"></div>
      <el-table :data="behaviorRows(behavior[def.key])" border size="small" class="top-table">
        <el-table-column type="index" label="#" width="44" align="center" />
        <el-table-column prop="key" :label="behavior[def.key].dim === 'ip' ? 'IP 地址' : '用户'" min-width="180" show-overflow-tooltip>
          <template #default="{ row }">
            <span :class="{ 'dim-empty': !row.key }">{{ row.key || (behavior[def.key].dim === 'ip' ? '未知' : '匿名') }}</span>
          </template>
        </el-table-column>
        <el-table-column prop="count" label="次数" width="100" align="right" />
        <el-table-column label="占比" width="100" align="right">
          <template #default="{ row }">{{ row.percent }}</template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- ============ 下载行为 ============ -->
    <div class="section-title"><span class="section-title-text">下载行为</span><span class="section-title-sub">基于公开文件下载记录的聚合分析：趋势、热度、时段、来源与失败/异常情况</span></div>

    <!-- ============ 概览指标卡片 ============ -->
    <section class="metric-grid">
      <div class="metric-card">
        <div class="metric-value">{{ summary.totalDownloads }}</div>
        <div class="metric-title">成功下载（次）</div>
      </div>
      <div class="metric-card">
        <div class="metric-value">{{ summary.uniqueFiles }}</div>
        <div class="metric-title">去重文件（个）</div>
      </div>
      <div class="metric-card">
        <div class="metric-value">{{ summary.uniqueIPs }}</div>
        <div class="metric-title">独立 IP（个）</div>
      </div>
      <div class="metric-card">
        <div class="metric-value">{{ summary.uniqueUsers }}</div>
        <div class="metric-title">活跃用户（位）</div>
      </div>
      <div class="metric-card">
        <div class="metric-value">{{ fmtBytes(summary.totalBytes) }}</div>
        <div class="metric-title">下载体积</div>
      </div>
      <div class="metric-card danger">
        <div class="metric-value">{{ summary.failed }}</div>
        <div class="metric-title">失败 / 拦截（次）</div>
      </div>
    </section>

    <!-- ============ 下载趋势 ============ -->
    <el-card shadow="never" class="chart-card">
      <template #header>
        <div class="card-title">
          <span>下载趋势</span>
          <span class="selected-file" v-if="selectedFromFile">
            单文件：{{ selectedFromFile.fileName }}（{{ selectedFromFile.fullPath }}）
            <el-button link type="primary" size="small" @click="clearSelectedFile">清除单文件</el-button>
          </span>
        </div>
      </template>
      <div ref="trendEl" class="chart-box"></div>
    </el-card>

    <!-- ============ 时段热度分布 ============ -->
    <el-card shadow="never" class="chart-card">
      <template #header><span class="card-title">时段热度分布 <span class="sub">周几 × 24 小时的成功下载热力</span></span></template>
      <div ref="heatmapEl" class="chart-box"></div>
    </el-card>

    <!-- ============ 生命周期 / 衰减曲线 ============ -->
    <el-card shadow="never" class="chart-card">
      <template #header><span class="card-title">生命周期 / 衰减曲线 <span class="sub">自上传以来各天聚集的下载量</span></span></template>
      <div ref="lifecycleEl" class="chart-box"></div>
    </el-card>

    <!-- ============ 热门下载文件 ============ -->
    <el-card shadow="never" class="chart-card">
      <template #header>
        <div class="card-title">
          <span>热门下载文件</span>
          <el-radio-group v-model="topSort" size="small" @change="loadTopFiles">
            <el-radio-button label="count">按下载数</el-radio-button>
            <el-radio-button label="spread">按独立 IP（扩散）</el-radio-button>
          </el-radio-group>
        </div>
      </template>
      <div ref="topFilesEl" class="chart-box small"></div>
      <el-table :data="topFiles" border size="small" :show-header="true" class="top-table" @row-click="onTopFileClick">
        <el-table-column type="index" label="#" width="44" align="center" />
        <el-table-column prop="fileName" label="文件名" min-width="170" show-overflow-tooltip />
        <el-table-column prop="fullPath" label="路径" min-width="170" show-overflow-tooltip />
        <el-table-column prop="count" label="下载数" width="86" align="right" />
        <el-table-column prop="spread" label="独立 IP" width="86" align="right" />
        <el-table-column label="扩散比" width="86" align="right">
          <template #default="{ row }">{{ row.count > 0 ? (row.spread / row.count).toFixed(2) : '-' }}</template>
        </el-table-column>
        <el-table-column v-if="buttonsEnabled" label="操作" width="70" align="center">
          <template #default="{ row }">
            <el-button link type="primary" size="small" :disabled="!row.sourceId">下钻</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- ============ 来源分布（按 IP / 按用户） ============ -->
    <el-card shadow="never" class="chart-card">
      <template #header>
        <div class="card-title">
          <span>下载来源分布</span>
          <el-radio-group v-model="sourceBy" size="small" @change="loadSources">
            <el-radio-button label="ip">按 IP</el-radio-button>
            <el-radio-button label="user">按用户</el-radio-button>
          </el-radio-group>
        </div>
      </template>
      <div ref="sourcesEl" class="chart-box small"></div>
      <el-table :data="sourcesRows" border size="small" class="top-table">
        <el-table-column type="index" label="#" width="44" align="center" />
        <el-table-column prop="key" :label="sourceBy === 'ip' ? 'IP 地址' : '用户'" min-width="180" show-overflow-tooltip />
        <el-table-column prop="count" label="下载数" width="100" align="right" />
        <el-table-column prop="fileCount" label="涉文件数" width="100" align="right" />
      </el-table>
    </el-card>

    <!-- ============ 目录 / 类型聚合 ============ -->
    <el-card shadow="never" class="chart-card">
      <template #header>
        <div class="card-title">
          <span>目录 / 类型聚合</span>
          <el-radio-group v-model="aggDim" size="small" @change="loadAggregate">
            <el-radio-button label="dir">按目录</el-radio-button>
            <el-radio-button label="type">按扩展名</el-radio-button>
          </el-radio-group>
        </div>
      </template>
      <div class="agg-wrap">
        <div ref="aggregateEl" class="chart-box stack-chart"></div>
        <el-table :data="aggregate" border size="small" class="agg-table">
          <el-table-column type="index" label="#" width="44" align="center" />
          <el-table-column prop="key" :label="aggDim === 'dir' ? '目录' : '扩展名'" min-width="180" show-overflow-tooltip />
          <el-table-column prop="count" label="下载数" width="100" align="right" />
          <el-table-column prop="files" label="涉文件数" width="100" align="right" />
        </el-table>
      </div>
    </el-card>

    <!-- ============ 失败 / 异常情况 ============ -->
    <el-card shadow="never" class="chart-card">
      <template #header><span class="card-title">失败 / 异常情况</span></template>
      <div class="fail-wrap">
        <div ref="failEl" class="chart-box stack-chart"></div>
        <el-table :data="failures" border size="small" class="fail-table">
          <el-table-column prop="reason" label="失败原因" min-width="160" />
          <el-table-column prop="count" label="次数" width="110" align="right" />
          <el-table-column prop="ips" label="涉事 IP" width="120" align="right" />
        </el-table>
      </div>
    </el-card>

    <!-- ============ 单文件下钻 ============ -->
    <el-card v-if="fileDetail" shadow="never" class="chart-card">
      <template #header>
        <span class="card-title">
          单文件下钻：{{ fileDetail.fileName }}
          <span class="sub">共 {{ fileDetail.total }} 次成功 · {{ fileDetail.failed }} 次失败</span>
        </span>
      </template>
      <div ref="fileDetailEl" class="chart-box"></div>
      <div class="two-col">
        <div class="source-list">
          <h4>按 IP</h4>
          <ul>
            <li v-for="item in fileDetail.byIP" :key="item.key">{{ item.key }}：{{ item.count }} 次</li>
            <li v-if="!fileDetail.byIP.length" class="empty">暂无</li>
          </ul>
        </div>
        <div class="source-list">
          <h4>按用户</h4>
          <ul>
            <li v-for="item in fileDetail.byUser" :key="item.key">{{ item.key }}：{{ item.count }} 次</li>
            <li v-if="!fileDetail.byUser.length" class="empty">暂无</li>
          </ul>
        </div>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import * as echarts from 'echarts/lib/echarts'
import 'echarts/lib/chart/line'
import 'echarts/lib/chart/bar'
import 'echarts/lib/chart/heatmap'
import 'echarts/lib/component/grid'
import 'echarts/lib/component/tooltip'
import 'echarts/lib/component/legend'
import 'echarts/lib/component/visualMap'
import 'echarts/lib/component/graphic'
import { BehaviorAnalyticsApi, FileApi } from '../api'

const loading = ref(false)
const preset = ref('30d')
const customRange = ref([])
const granularity = ref('day')
const sourceBy = ref('ip')
const topSort = ref('count')
const aggDim = ref('dir')

const summary = reactive({ totalDownloads: 0, uniqueFiles: 0, uniqueIPs: 0, uniqueUsers: 0, totalBytes: 0, failed: 0 })
const topFiles = ref([])
const failures = ref([])
const heatmap = ref([])
const lifecycle = ref([])
const aggregate = ref([])
const selectedFromFile = ref(null)
const fileDetail = ref(null)
const sourcesRows = ref([])

const trendEl = ref()
const topFilesEl = ref()
const sourcesEl = ref()
const failEl = ref()
const heatmapEl = ref()
const lifecycleEl = ref()
const aggregateEl = ref()
const fileDetailEl = ref()

// ================= 访问 / 上传行为 =================
// 复用 /files/recent 现有记录做轻量聚合（不新增后端接口）。"访问"以预览记录近似。
const behaviorDefs = [
  { key: 'access', label: '访问行为', action: 'preview', color: '#16a085', desc: '基于最近预览记录' },
  { key: 'upload', label: '上传行为', action: 'upload', color: '#fa8c16', desc: '基于最近上传记录' }
]

const behavior = {}
for (const def of behaviorDefs) {
  behavior[def.key] = reactive({
    key: def.key,
    action: def.action,
    color: def.color,
    dim: 'user',
    loading: false,
    el: null,
    summary: { total: 0, uniqueUsers: 0, uniqueIPs: 0 },
    byUser: [],
    byIP: []
  })
}

// 把 {key: count} 聚合成排序后的行（含占比），按次数降序
const toRows = (map) => {
  const total = Object.values(map).reduce((s, v) => s + v, 0)
  return Object.entries(map)
    .map(([key, count]) => ({ key, count, percent: total > 0 ? ((count / total) * 100).toFixed(1) + '%' : '-' }))
    .sort((a, b) => b.count - a.count)
}

async function loadBehavior(st) {
  st.loading = true
  try {
    const res = await FileApi.recent(1, 100, st.action)
    const records = (res.data && res.data.records) || []
    const byUser = {}
    const byIP = {}
    for (const r of records) {
      const u = r.userID ? r.userID : '匿名'
      const ip = r.clientIP ? r.clientIP : '未知'
      byIP[ip] = (byIP[ip] || 0) + 1
      byUser[u] = (byUser[u] || 0) + 1
    }
    st.byUser = toRows(byUser)
    st.byIP = toRows(byIP)
    st.summary.total = (res.data && res.data.total) || records.length
    st.summary.uniqueUsers = Object.keys(byUser).length
    st.summary.uniqueIPs = Object.keys(byIP).length
  } catch (e) {
    st.byUser = []
    st.byIP = []
  } finally {
    st.loading = false
    renderBehaviorChart(st)
  }
}

const behaviorRows = (st) => (st.dim === 'ip' ? st.byIP : st.byUser)

function renderBehaviorChart(st) {
  const rows = (st.dim === 'ip' ? st.byIP : st.byUser).slice(0, 10)
  const name = 'behavior-' + st.key
  if (!st.el) return
  if (rows.length === 0) {
    renderChart(name, st.el, emptyHorizBarOption(), true)
    return
  }
  const c = axisColors()
  renderChart(name, st.el, {
    color: [st.color],
    tooltip: { trigger: 'axis' },
    grid: baseGridOpt(),
    xAxis: { type: 'value', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
    yAxis: { type: 'category', inverse: true, data: rows.map((r) => r.key), axisLabel: { color: c.text, formatter: (v) => truncateLabel(v, 140) }, axisLine: { show: false } },
    series: [{ type: 'bar', barMaxWidth: 18, data: rows.map((r) => r.count) }]
  }, true)
}

// ================= 下载 =================
const buttonsEnabled = computed(() => topFiles.value.some((f) => f.sourceId > 0))

// 由档位/自定义解析出 RFC3339 的 from/to
function resolveRange() {
  const now = new Date()
  let from, to
  if (preset.value === '7d') {
    from = new Date(now.getTime() - 7 * 24 * 3600 * 1000)
    to = now
  } else if (preset.value === 'custom' && customRange.value && customRange.value.length === 2) {
    from = customRange.value[0]
    to = customRange.value[1]
  } else {
    from = new Date(now.getTime() - 30 * 24 * 3600 * 1000)
    to = now
  }
  return { from: from.toISOString(), to: to.toISOString() }
}

function fmtBytes(val) {
  if (!val) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = 0
  let v = val
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i++
  }
  return `${v.toFixed(v >= 100 ? 0 : 1)} ${units[i]}`
}

function axisColors() {
  const dark = window.matchMedia?.('(prefers-color-scheme: dark)').matches
  return { tick: dark ? '#8a8f98' : '#909399', line: dark ? '#3a3f45' : '#e4e7ed', text: dark ? '#c8ccd2' : '#606266' }
}

function baseGridOpt() {
  return { left: 16, right: 24, top: 32, bottom: 8, containLabel: true }
}

// echarts 4.x 不支持 axisLabel 的 width/overflow 截断，用 formatter 实现
function truncateLabel(v, max) {
  v = v == null ? '' : String(v)
  return v.length > max ? v.slice(0, max - 1) + '…' : v
}

// 横向柱状图空态：无数据时仍渲染坐标轴并居中提示"暂无数据"，避免整个图表区域空白。
function emptyHorizBarOption() {
  const c = axisColors()
  return {
    grid: baseGridOpt(),
    xAxis: { type: 'value', axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
    yAxis: { type: 'category', inverse: true, data: [], axisLabel: { color: c.text }, axisLine: { show: false } },
    series: [{ type: 'bar', data: [] }],
    graphic: { type: 'text', left: 'center', top: 'middle', style: { text: '暂无数据', textAlign: 'center', fill: c.tick, fontSize: 13 } }
  }
}

// ================= 统一图表渲染（修复容器塌陷）=================
// 若容器尚无尺寸则推迟到下一帧再初始化，避免 echarts.init 在高度为 0 的容器上画出塌陷画布。
const chartInst = {}
function renderChart(name, el, option, notMerge) {
  if (!el) return
  if (!chartInst[name]) {
    if (!el.clientWidth || !el.clientHeight) {
      requestAnimationFrame(() => renderChart(name, el, option, notMerge))
      return
    }
    chartInst[name] = echarts.init(el)
  }
  chartInst[name].setOption(option, !!notMerge)
}

async function loadSummary() {
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getSummary({ from, to })
  if (res.success && res.data) Object.assign(summary, res.data)
}

async function loadTrend() {
  const { from, to } = resolveRange()
  const params = { from, to, granularity: granularity.value }
  if (selectedFromFile.value && selectedFromFile.value.sourceId) {
    params.id = selectedFromFile.value.sourceId
  }
  const res = await BehaviorAnalyticsApi.getTrend(params)
  const data = (res.data || []).filter((p) => p.time)
  const single = !!selectedFromFile.value
  const c = axisColors()
  renderChart('trend', trendEl.value, {
    color: ['#2080f0', '#f56c6c'],
    tooltip: { trigger: 'axis' },
    legend: { top: 0, data: ['成功', '失败'], textStyle: { color: c.text } },
    grid: baseGridOpt(),
    xAxis: { type: 'category', data: data.map((p) => p.time), axisLabel: { color: c.tick }, axisLine: { lineStyle: { color: c.line } } },
    yAxis: { type: 'value', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
    series: [
      { name: '成功', type: 'line', smooth: true, areaStyle: { opacity: 0.15 }, showSymbol: false, data: data.map((p) => p.count) },
      { name: '失败', type: 'line', smooth: true, showSymbol: false, data: data.map((p) => p.failed) }
    ]
  }, !single)
}

async function loadTopFiles() {
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getTopFiles({ from, to, limit: 10, sort: topSort.value })
  topFiles.value = res.data || []
  const rows = topFiles.value.slice(0, 10)
  if (rows.length === 0) {
    renderChart('topFiles', topFilesEl.value, emptyHorizBarOption(), true)
    return
  }
  const c = axisColors()
  renderChart('topFiles', topFilesEl.value, {
    color: ['#2080f0'],
    tooltip: { trigger: 'axis' },
    grid: baseGridOpt(),
    xAxis: { type: 'value', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
    yAxis: {
      type: 'category',
      inverse: true,
      data: rows.map((r) => r.fileName),
      axisLabel: { color: c.text, formatter: (v) => truncateLabel(v, 130) },
      axisLine: { show: false }
    },
    series: [{
      type: 'bar', barMaxWidth: 18,
      data: rows.map((r) => (topSort.value === 'spread' ? r.spread : r.count))
    }]
  }, true)
}

async function loadSources() {
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getSources({ from, to, by: sourceBy.value, limit: 10 })
  sourcesRows.value = res.data || []
  const rows = sourcesRows.value.slice(0, 10)
  if (rows.length === 0) {
    renderChart('sources', sourcesEl.value, emptyHorizBarOption(), true)
    return
  }
  const c = axisColors()
  renderChart('sources', sourcesEl.value, {
    color: ['#16a085'],
    tooltip: { trigger: 'axis' },
    grid: baseGridOpt(),
    xAxis: { type: 'value', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
    yAxis: { type: 'category', inverse: true, data: rows.map((r) => r.key), axisLabel: { color: c.text, formatter: (v) => truncateLabel(v, 120) }, axisLine: { show: false } },
    series: [{ type: 'bar', barMaxWidth: 18, data: rows.map((r) => r.count) }]
  }, true)
}

async function loadFailures() {
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getFailures({ from, to, limit: 10 })
  failures.value = res.data || []
  const rows = failures.value.slice(0, 8)
  if (rows.length === 0) {
    renderChart('fail', failEl.value, emptyHorizBarOption(), true)
    return
  }
  const c = axisColors()
  renderChart('fail', failEl.value, {
    color: ['#f56c6c'],
    tooltip: { trigger: 'axis' },
    grid: baseGridOpt(),
    xAxis: { type: 'value', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
    yAxis: { type: 'category', inverse: true, data: rows.map((r) => r.reason), axisLabel: { color: c.text, formatter: (v) => truncateLabel(v, 130) }, axisLine: { show: false } },
    series: [{ type: 'bar', barMaxWidth: 18, data: rows.map((r) => r.count) }]
  }, true)
}

async function loadHeatmap() {
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getHeatmap({ from, to })
  heatmap.value = res.data || []
  if (!heatmapEl.value) return
  const c = axisColors()
  const weekdays = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']
  const cells = (res.data || []).map((cell) => [cell.weekday - 1, cell.hour, cell.count])
  const max = Math.max(1, ...cells.map((d) => d[2]))
  renderChart('heatmap', heatmapEl.value, {
    tooltip: {
      position: 'top',
      formatter: (p) => `${weekdays[p.value[0]]} ${String(p.value[1]).padStart(2, '0')}:00<br/>下载 ${p.value[2]} 次`
    },
    grid: { left: 56, right: 24, top: 16, bottom: 40 },
    xAxis: { type: 'category', data: weekdays, splitArea: { show: true }, axisLabel: { color: c.tick }, axisLine: { lineStyle: { color: c.line } } },
    yAxis: {
      type: 'category',
      data: Array.from({ length: 24 }, (_, i) => `${String(i).padStart(2, '0')}:00`),
      inverse: true,
      splitArea: { show: true },
      axisLabel: { color: c.tick, fontSize: 10 },
      axisLine: { lineStyle: { color: c.line } }
    },
    visualMap: {
      min: 0,
      max,
      calculable: true,
      orient: 'horizontal',
      left: 'center',
      bottom: 0,
      textStyle: { color: c.text },
      inRange: { color: ['#ebf4ff', '#2080f0', '#f5222d'] }
    },
    series: [{ type: 'heatmap', data: cells, label: { show: false }, itemStyle: { borderColor: '#fff', borderWidth: 1 } }]
  }, true)
}

async function loadLifecycle() {
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getLifecycle({ from, to, capDays: 30 })
  lifecycle.value = res.data || []
  if (!lifecycleEl.value) return
  const c = axisColors()
  const points = res.data || []
  renderChart('lifecycle', lifecycleEl.value, {
    color: ['#2080f0', '#fa8c16'],
    tooltip: { trigger: 'axis' },
    legend: { top: 0, data: ['下载量', '涉文件数'], textStyle: { color: c.text } },
    grid: baseGridOpt(),
    xAxis: { type: 'category', data: points.map((p) => `第 ${p.day} 天`), axisLabel: { color: c.tick, interval: 2 }, axisLine: { lineStyle: { color: c.line } } },
    yAxis: [
      { type: 'value', name: '下载量', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
      { type: 'value', name: '文件数', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { show: false } }
    ],
    series: [
      { name: '下载量', type: 'line', smooth: true, areaStyle: { opacity: 0.15 }, showSymbol: false, data: points.map((p) => p.downloads) },
      { name: '涉文件数', type: 'line', smooth: true, yAxisIndex: 1, showSymbol: false, data: points.map((p) => p.files) }
    ]
  }, true)
}

async function loadAggregate() {
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getAggregate({ from, to, dimension: aggDim.value, limit: 12 })
  aggregate.value = res.data || []
  const rows = aggregate.value.slice(0, 12)
  if (rows.length === 0) {
    renderChart('aggregate', aggregateEl.value, emptyHorizBarOption(), true)
    return
  }
  const c = axisColors()
  const label = aggDim.value === 'dir' ? '目录' : '扩展名'
  renderChart('aggregate', aggregateEl.value, {
    color: ['#722ed1'],
    tooltip: { trigger: 'axis' },
    grid: baseGridOpt(),
    xAxis: { type: 'value', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
    yAxis: { type: 'category', inverse: true, data: rows.map((r) => r.key), axisLabel: { color: c.text, formatter: (v) => truncateLabel(aggDim.value === 'type' ? '.' + v : v, 180) }, axisLine: { show: false } },
    series: [{ name: label, type: 'bar', barMaxWidth: 18, data: rows.map((r) => r.count) }]
  }, true)
}

async function loadFileDetail() {
  const sel = selectedFromFile.value
  if (!sel || !sel.sourceId) return
  const { from, to } = resolveRange()
  const res = await BehaviorAnalyticsApi.getFileDetail({ id: sel.sourceId, from, to })
  if (res.success && res.data) {
    if (!fileDetail.value) fileDetail.value = res.data
    else Object.assign(fileDetail.value, res.data)
    const c = axisColors()
    const points = fileDetail.value.trend || []
    renderChart('fileDetail', fileDetailEl.value, {
      color: ['#2080f0', '#f56c6c'],
      tooltip: { trigger: 'axis' },
      legend: { top: 0, data: ['成功', '失败'], textStyle: { color: c.text } },
      grid: baseGridOpt(),
      xAxis: { type: 'category', data: points.map((p) => p.time), axisLabel: { color: c.tick }, axisLine: { lineStyle: { color: c.line } } },
      yAxis: { type: 'value', minInterval: 1, axisLabel: { color: c.tick }, splitLine: { lineStyle: { color: c.line } } },
      series: [
        { name: '成功', type: 'line', smooth: true, areaStyle: { opacity: 0.15 }, showSymbol: false, data: points.map((p) => p.count) },
        { name: '失败', type: 'line', smooth: true, showSymbol: false, data: points.map((p) => p.failed) }
      ]
    }, false)
  }
}

async function loadAll() {
  loading.value = true
  if (selectedFromFile.value && !selectedFromFile.value.sourceId) clearSelectedFile()
  try {
    await loadFileDetail()
    await Promise.all([
      loadSummary(), loadTrend(), loadTopFiles(), loadSources(), loadFailures(), loadHeatmap(), loadLifecycle(), loadAggregate()
    ])
    // 访问 / 上传行为（复用最近记录，前端聚合）
    behaviorDefs.forEach((def) => loadBehavior(behavior[def.key]))
  } finally {
    loading.value = false
    nextTick(resizeCharts)
  }
}

function onTopFileClick(row) {
  if (!row.sourceId) {
    return
  }
  selectedFromFile.value = row
  fileDetail.value = null
  loadAll()
}

function clearSelectedFile() {
  selectedFromFile.value = null
  fileDetail.value = null
  loadAll()
}

function resizeCharts() {
  Object.values(chartInst).forEach((ch) => ch && ch.resize())
}

onMounted(async () => {
  await loadAll()
  window.addEventListener('resize', resizeCharts)
})

onUnmounted(() => {
  window.removeEventListener('resize', resizeCharts)
  Object.values(chartInst).forEach((ch) => {
    if (ch) ch.dispose()
  })
})
</script>

<style scoped>
.admin-behavior-analytics {
  padding: 8px 2px;
  min-width: 0;
}
.header-section {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-bottom: 14px;
}
.header-left h2 {
  margin: 0 0 4px;
  font-size: 20px;
}
.header-left .description {
  margin: 0;
  color: var(--el-text-color-placeholder);
  font-size: 13px;
}

.el-card {
  /* 修复塌陷问题 */
  overflow: unset;
}

.filter-card {
  margin-bottom: 14px;
}
.filter-row {
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
}
.filter-label {
  color: var(--el-text-color-secondary);
  font-size: 14px;
}
.section-title {
  display: flex;
  align-items: baseline;
  gap: 10px;
  margin: 4px 0 12px;
}
.section-title-text {
  font-size: 16px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}
.section-title-sub {
  font-size: 13px;
  color: var(--el-text-color-placeholder);
}
.behavior-card {
  margin-bottom: 14px;
}
.behavior-metrics {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}
.metric-grid {
  display: grid;
  grid-template-columns: repeat(6, 1fr);
  gap: 12px;
  margin-bottom: 14px;
}
.metric-card {
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
}
.metric-card .metric-value {
  font-size: 24px;
  font-weight: 600;
  color: #2080f0;
  word-break: break-all;
}
.metric-card.danger .metric-value {
  color: #f56c6c;
}
.metric-card .metric-title {
  margin-top: 6px;
  font-size: 13px;
  color: var(--el-text-color-placeholder);
}
.chart-card {
  margin-bottom: 14px;
}
.card-title {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-weight: 600;
  font-size: 15px;
}
.card-title .sub {
  font-size: 13px;
  color: var(--el-text-color-placeholder);
  font-weight: 400;
  margin-left: 8px;
}
.selected-file {
  font-size: 13px;
  color: #e6a23c;
  font-weight: 400;
}
.chart-box {
  width: 100%;
  height: 320px;
  min-width: 0;
  min-height: 0;
}
.chart-box.small {
  height: 260px;
}
.chart-box.stack-chart {
  width: 100%;
  height: 300px;
  flex: none;
}
.two-col {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}
.dim-empty {
  color: var(--el-text-color-placeholder);
}
.top-table {
  margin-top: 10px;
}
.agg-wrap {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.agg-table {
  width: 100%;
}
.fail-wrap {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.fail-table {
  width: 100%;
}
.source-list {
  padding: 4px 8px;
}
.source-list h4 {
  margin: 6px 0;
  color: var(--el-text-color-secondary);
  font-size: 14px;
}
.source-list ul {
  list-style: none;
  margin: 0;
  padding: 0;
  max-height: 160px;
  overflow: auto;
}
.source-list li {
  padding: 4px 0;
  font-size: 13px;
  color: var(--el-text-color-secondary);
  border-bottom: 1px dashed var(--el-border-color);
}
.source-list li.empty {
  color: var(--el-text-color-placeholder);
}

@media (max-width: 1100px) {
  .metric-grid {
    grid-template-columns: repeat(3, 1fr);
  }
  .behavior-metrics {
    grid-template-columns: repeat(3, 1fr);
  }
  .two-col {
    grid-template-columns: 1fr;
  }
}
</style>
