<template>
  <div class="admin-resource">
    <div class="header-section">
      <div class="header-left">
        <h2>服务器资源监控</h2>
        <p class="description">服务器整体与本程序的 CPU、内存、磁盘占用与磁盘 IO（实时 + 历史）</p>
      </div>
      <div class="header-right">
        <el-tag v-if="!enabled" type="warning">资源监控未启用</el-tag>
        <el-tag v-else :type="collectorRunning ? 'success' : 'info'" effect="light">
          {{ collectorRunning ? '采集运行中' : '等待首帧' }}
        </el-tag>
        <el-button type="primary" size="small" :loading="loading" @click="refreshAll">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </div>

    <el-alert
      v-if="!enabled"
      type="warning"
      :closable="false"
      class="enable-tip"
      title="资源监控未启用，当前展示的是实时快照与历史曲线的最新可用数据。请在「设置」→「服务器资源监控」中开启 resource.enabled。"
    />

    <!-- ============ 顶部 4 指标块 ============ -->
    <section class="metric-grid">
      <!-- CPU 仪表盘 -->
      <el-card shadow="never" class="metric-card">
        <div class="metric-title">CPU 占用率</div>
        <div ref="cpuGaugeEl" class="gauge-box"></div>
        <div class="gauge-legend">
          <span class="legend-item"><i class="dot" style="background:#FFA500"></i>服务器 {{ fmtPercent(server.cpu) }}</span>
          <span class="legend-item"><i class="dot" style="background:#2080f0"></i>本程序 {{ fmtPercent(program.cpu) }}</span>
        </div>
      </el-card>

      <!-- 内存仪表盘 -->
      <el-card shadow="never" class="metric-card">
        <div class="metric-title">内存占用</div>
        <div ref="memGaugeEl" class="gauge-box"></div>
        <div class="gauge-legend">
          <span class="legend-item"><i class="dot" style="background:#FFA500"></i>服务器 {{ fmtPercent(memPercent(server)) }}</span>
          <span class="legend-item"><i class="dot" style="background:#2080f0"></i>本程序 {{ fmtPercent(memPercent(program)) }}</span>
        </div>
      </el-card>

      <!-- 磁盘：背景色表示用量 -->
      <el-card shadow="never" class="metric-card">
        <div class="metric-title">磁盘占用</div>
        <div class="disk-usage">
          <div class="disk-row">
            <span class="disk-label">服务器</span>
            <div class="disk-bar">
              <div class="disk-fill" :style="fillStyle(diskPercent(server))"></div>
            </div>
            <span class="disk-pct">{{ diskPercent(server) }}%</span>
          </div>
          <div class="disk-row">
            <span class="disk-label">本程序</span>
            <div class="disk-bar">
              <div class="disk-fill" :style="fillStyle(diskPercent(program))"></div>
            </div>
            <span class="disk-pct">{{ diskPercent(program) }}%</span>
          </div>
          <div class="disk-note">服务器 {{ fmtBytes(server.diskUsed) }} / {{ fmtBytes(server.diskTotal) }} · 本程序 {{ fmtBytes(program.diskUsed) }}</div>
        </div>
      </el-card>

      <!-- 磁盘 IO：数值 -->
      <el-card shadow="never" class="metric-card">
        <div class="metric-title">磁盘 IO</div>
        <div class="io-numeric">
          <div class="io-row info">
            <span class="io-scope">服务器</span>
            <span class="io-val"><b class="read">读 ↓</b> {{ fmtRate(server.diskIoRead) }}</span>
            <span class="io-val"><b class="write">写 ↑</b> {{ fmtRate(server.diskIOWrite) }}</span>
          </div>
          <div class="io-row">
            <span class="io-scope">本程序</span>
            <span class="io-val"><b class="read">读 ↓</b> {{ fmtRate(program.diskIoRead) }}</span>
            <span class="io-val"><b class="write">写 ↑</b> {{ fmtRate(program.diskIOWrite) }}</span>
          </div>
        </div>
      </el-card>
    </section>

    <!-- ============ 资源曲线（整合实时+历史，服务器与本程序同图） ============ -->
    <section class="chart-section">
      <div class="chart-header">
        <h3>资源曲线</h3>
        <div class="chart-actions">
          <el-radio-group v-model="hiRange" size="small" @change="onRangeChange">
            <el-radio-button label="1h">近 1 小时</el-radio-button>
            <el-radio-button label="6h">近 6 小时</el-radio-button>
            <el-radio-button label="24h">近 24 小时</el-radio-button>
            <el-radio-button label="7d">近 7 天</el-radio-button>
          </el-radio-group>
        </div>
      </div>
      <div v-loading="loading" class="chart-grid">
        <el-card shadow="never" class="chart-card"><div ref="cpuChartEl" class="chart-box"></div></el-card>
        <el-card shadow="never" class="chart-card"><div ref="memChartEl" class="chart-box"></div></el-card>
        <el-card shadow="never" class="chart-card"><div ref="diskChartEl" class="chart-box"></div></el-card>
        <el-card shadow="never" class="chart-card"><div ref="ioChartEl" class="chart-box"></div></el-card>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted, onUnmounted, nextTick } from 'vue'
import * as echarts from 'echarts/core'
import { GaugeChart, LineChart } from 'echarts/charts'
import { GridComponent, TooltipComponent, TitleComponent, LegendComponent } from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'
import { Refresh } from '@element-plus/icons-vue'
import { ConfigApi, ResourceApi } from '@/api'

echarts.use([GaugeChart, LineChart, GridComponent, TooltipComponent, TitleComponent, LegendComponent, CanvasRenderer])

// ============ 状态 ============
const loading = ref(false)
const enabled = ref(true)
const collectorRunning = ref(false)
const samplingInterval = ref(5)
const hiRange = ref('1h')

const emptyPoint = {
  cpu: 0, memory: 0, memoryTotal: 0,
  diskUsed: 0, diskTotal: 0, diskIoRead: 0, diskIOWrite: 0
}
const server = reactive({ ...emptyPoint })
const program = reactive({ ...emptyPoint })

// ============ 格式化 ============
const fmtPercent = (v) => (v == null ? '-' : `${Number(v).toFixed(1)}%`)
const memPercent = ({ memory, memoryTotal }) =>
  memoryTotal > 0 ? Math.min(100, Math.round((memory / memoryTotal) * 1000) / 10) : 0
const diskPercent = ({ diskUsed, diskTotal }) =>
  diskTotal > 0 ? Math.min(100, Math.round((diskUsed / diskTotal) * 1000) / 10) : 0

// 磁盘占用：用量越高背景色越偏红
const usageColor = (pct) => pct >= 90 ? '#d03050' : pct >= 70 ? '#e6a23c' : '#18a058'
const fillStyle = (pct) => ({ width: `${pct}%`, background: usageColor(pct) })

function fmtBytes(n) {
  n = Number(n) || 0
  if (n <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB', 'PB']
  let i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n >= 100 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`
}
function fmtRate(bytesPerSec) {
  bytesPerSec = Number(bytesPerSec) || 0
  if (bytesPerSec <= 0) return '0 B/s'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = bytesPerSec
  let i = 0
  while (n >= 1024 && i < units.length - 1) { n /= 1024; i++ }
  return `${n >= 100 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}/s`
}

const ORANGE = '#FFA500'
const BLUE = '#2080f0'

// ============ ECharts 实例 ============
const cpuGaugeEl = ref(null)
const memGaugeEl = ref(null)
const cpuChartEl = ref(null)
const memChartEl = ref(null)
const diskChartEl = ref(null)
const ioChartEl = ref(null)

const gaugeCpu = ref(null)
const gaugeMem = ref(null)
const chCpu = ref(null)
const chMem = ref(null)
const chDisk = ref(null)
const chIo = ref(null)

// 实时缓冲（内存 5 秒采样）与最近一次历史数据，用于整合渲染
const rtRealtime = { server: [], program: [] }
const lastHist = { server: [], program: [] }

// ============ 顶部仪表盘 ============
function gaugeBaseOption() {
  return {
    series: [{
      type: 'gauge', min: 0, max: 100, startAngle: 200, endAngle: -20,
      radius: '95%', center: ['50%', '62%'],
      progress: { show: true, roundCap: true, width: 10 },
      axisLine: { lineStyle: { width: 10 } },
      axisTick: { show: false }, splitLine: { show: false },
      axisLabel: { show: false },
      pointer: { show: true, length: '70%', width: 5, itemStyle: { color: 'auto' } },
      detail: { show: false }, title: { show: false }
    }]
  }
}

function renderGauges() {
  const cpuData = [
    { value: +Number(server.cpu || 0).toFixed(1), name: '服务器', itemStyle: { color: ORANGE } },
    { value: +Number(program.cpu || 0).toFixed(1), name: '本程序', itemStyle: { color: BLUE } }
  ]
  gaugeCpu.value?.setOption({ series: [{ ...gaugeBaseOption().series[0], data: cpuData }] }, { notMerge: true })

  const memData = [
    { value: memPercent(server), name: '服务器', itemStyle: { color: ORANGE } },
    { value: memPercent(program), name: '本程序', itemStyle: { color: BLUE } }
  ]
  gaugeMem.value?.setOption({ series: [{ ...gaugeBaseOption().series[0], data: memData }] }, { notMerge: true })
}

// ============ 曲线（实时+历史整合，服务器与程序同图，动态追加刷新） ============
// 数据点统一存储为 [timestamp, value...]；内存序列额外携带使用量字节用于 tooltip。
const seriesData = {}
const seriesKeys = ['cpu-s', 'cpu-p', 'mem-s', 'mem-p', 'disk-s', 'disk-p', 'ioR-s', 'ioR-p', 'ioW-s', 'ioW-p']
for (const k of seriesKeys) seriesData[k] = []

const seriesVal = {
  'cpu-s': (pt) => [+Number(pt.cpu || 0).toFixed(2)],
  'cpu-p': (pt) => [+Number(pt.cpu || 0).toFixed(2)],
  'mem-s': (pt) => [memPercent(pt), pt.memory || 0],
  'mem-p': (pt) => [memPercent(pt), pt.memory || 0],
  'disk-s': (pt) => [pt.diskUsed || 0],
  'disk-p': (pt) => [pt.diskUsed || 0],
  'ioR-s': (pt) => [pt.diskIoRead || 0],
  'ioR-p': (pt) => [pt.diskIoRead || 0],
  'ioW-s': (pt) => [pt.diskIOWrite || 0],
  'ioW-p': (pt) => [pt.diskIOWrite || 0]
}
const scopeKeys = (scope) => seriesKeys.filter((k) => k.endsWith('-' + scope))

function rangeMs(r) {
  switch (r) {
    case '1h': return 3600e3
    case '6h': return 6 * 3600e3
    case '24h': return 24 * 3600e3
    case '7d': return 7 * 24 * 3600e3
    default: return 3600e3
  }
}

function mergePoints(histPts, recentPts, from) {
  const map = new Map()
  for (const p of histPts || []) if (p.timestamp >= from) map.set(p.timestamp, p)
  for (const p of recentPts || []) if (p.timestamp >= from) map.set(p.timestamp, p)
  return [...map.values()].sort((a, b) => a.timestamp - b.timestamp)
}

// 依据 lastHist + rtRealtime 重建各序列数据（受当前时间窗口约束）
function rebuildSeries() {
  const from = Date.now() - rangeMs(hiRange.value)
  const s = mergePoints(lastHist.server, rtRealtime.server, from)
  const p = mergePoints(lastHist.program, rtRealtime.program, from)
  const src = { s, p }
  for (const key of seriesKeys) {
    const scope = key.slice(-1)
    const arr = seriesData[key]
    arr.length = 0
    const val = seriesVal[key]
    for (const pt of src[scope]) arr.push([pt.timestamp, ...val(pt)])
  }
}

function lineSeries(name, data, color) {
  return {
    name, type: 'line', showSymbol: false, smooth: true, data,
    lineStyle: { width: 1.5, color }, itemStyle: { color },
    areaStyle: { opacity: 0.08, color }
  }
}

function mkOption({ title, yMax, yName, yFormatter, valueFormatter, customTooltip, series }) {
  return {
    title: { text: title, textStyle: { fontSize: 13, fontWeight: 600, color: '#333' }, left: 4, top: 2 },
    legend: { top: 2, right: 8, itemWidth: 12, itemHeight: 8, textStyle: { fontSize: 11 } },
    grid: { left: 52, right: 16, top: 36, bottom: 28 },
    xAxis: { type: 'time', min: () => Date.now() - rangeMs(hiRange.value), max: () => Date.now(), axisLabel: { fontSize: 10 } },
    yAxis: { type: 'value', min: 0, max: yMax, name: yName, axisLabel: { fontSize: 10, formatter: yFormatter } },
    tooltip: {
      trigger: 'axis', confine: true,
      valueFormatter: valueFormatter,
      formatter: customTooltip || ((params) => mkTooltip(params, valueFormatter))
    },
    series
  }
}

function mkTooltip(params, valueFormatter) {
  if (!params || !params.length) return ''
  const p = params[0]
  const d = new Date(p.value[0])
  const pad = (x) => String(x).padStart(2, '0')
  let html = `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  for (const item of params) {
    const v = valueFormatter ? valueFormatter(item.value[1]) : item.value[1]
    html += `<br/>${item.marker}${item.seriesName}: <b>${v}</b>`
  }
  return html
}

// 内存 tooltip：同时显示使用量百分比与字节数
function memTooltip(params) {
  if (!params || !params.length) return ''
  const p = params[0]
  const d = new Date(p.value[0])
  const pad = (x) => String(x).padStart(2, '0')
  let html = `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  for (const item of params) {
    const pct = Number(item.value[1] || 0)
    const bytes = Number(item.value[2] || 0)
    html += `<br/>${item.marker}${item.seriesName}: <b>${pct.toFixed(1)}%</b> <span style="color:#a0a4ab">(${fmtBytes(bytes)})</span>`
  }
  return html
}

// ============ 动态刷新（官方 BP：持有一个 option，往数据数组 push，merge setOption） ============
// chartOpt 对象持久持有，其 series.data 引用 seriesData 数组；
// xAxis.min/max 为函数，每次渲染按 now 重新求值，实现窗口自动滚动。
const chartOpt = { cpu: null, mem: null, disk: null, io: null }
const chartRefs = { cpu: chCpu, mem: chMem, disk: chDisk, io: chIo }

function buildChartOpt() {
  chartOpt.cpu = mkOption({
    title: 'CPU 占用率', yName: '%', yMax: 100,
    valueFormatter: (v) => `${Number(v).toFixed(1)}%`,
    series: [
      lineSeries('服务器', seriesData['cpu-s'], ORANGE),
      lineSeries('本程序', seriesData['cpu-p'], BLUE)
    ]
  })

  chartOpt.mem = mkOption({
    title: '内存占用（%）', yName: '%', yMax: 100,
    customTooltip: memTooltip,
    series: [
      lineSeries('服务器', seriesData['mem-s'], ORANGE),
      lineSeries('本程序', seriesData['mem-p'], BLUE)
    ]
  })

  chartOpt.disk = mkOption({
    title: '磁盘占用',
    valueFormatter: (v) => fmtBytes(v), yFormatter: (v) => fmtBytes(v),
    series: [
      lineSeries('服务器', seriesData['disk-s'], ORANGE),
      lineSeries('本程序', seriesData['disk-p'], BLUE)
    ]
  })

  chartOpt.io = mkOption({
    title: '磁盘 IO',
    valueFormatter: (v) => fmtRate(v), yFormatter: (v) => fmtRate(v),
    series: [
      lineSeries('服务器读', seriesData['ioR-s'], ORANGE),
      lineSeries('服务器写', seriesData['ioW-s'], '#B97700'),
      lineSeries('本程序读', seriesData['ioR-p'], BLUE),
      lineSeries('本程序写', seriesData['ioW-p'], '#5B8FF9')
    ]
  })
}

// 全量绘制（初始加载 / 切换时间范围）
function drawAll() {
  buildChartOpt()
  for (const name of ['cpu', 'mem', 'disk', 'io']) chartRefs[name].value?.setOption(chartOpt[name])
}

function lastTs(arr) {
  let m = -Infinity
  for (const it of arr) if (it[0] > m) m = it[0]
  return m
}

// 追加式刷新：把新到达的实时点 push 进 seriesData，再 merge setOption（不整图重建）
function liveRefresh() {
  const from = Date.now() - rangeMs(hiRange.value)
  for (const scope of ['s', 'p']) {
    const recent = scope === 's' ? rtRealtime.server : rtRealtime.program
    let anchor = -Infinity
    for (const key of scopeKeys(scope)) anchor = Math.max(anchor, lastTs(seriesData[key]))
    const tail = recent
      .filter((pt) => pt.timestamp > anchor && pt.timestamp >= from)
      .sort((a, b) => a.timestamp - b.timestamp)
    if (!tail.length) continue
    for (const key of scopeKeys(scope)) {
      const val = seriesVal[key]
      const arr = seriesData[key]
      for (const pt of tail) arr.push([pt.timestamp, ...val(pt)])
    }
  }

  // 裁剪窗口外旧点，控制数据量（同一数组引用，merge 会重新读取）
  for (const key of seriesKeys) {
    const arr = seriesData[key]
    while (arr.length && arr[0][0] < from) arr.shift()
  }

  // 平滑滚动窗口，按需更新所有图
  for (const name of ['cpu', 'mem', 'disk', 'io']) {
    chartRefs[name].value?.setOption(chartOpt[name])
  }
}

// ============ 数据获取 ============
async function loadSnapshot() {
  try {
    const res = await ResourceApi.snapshot()
    const d = res.data || {}
    collectorRunning.value = !!d.running
    if (d.samplingInterval) samplingInterval.value = d.samplingInterval
    const apply = (target, p) => {
      target.cpu = p?.cpu ?? 0
      target.memory = p?.memory ?? 0
      target.memoryTotal = p?.memoryTotal ?? 0
      target.diskUsed = p?.diskUsed ?? 0
      target.diskTotal = p?.diskTotal ?? 0
      target.diskIoRead = p?.diskIoRead ?? 0
      target.diskIOWrite = p?.diskIOWrite ?? 0
    }
    apply(server, d.server)
    apply(program, d.program)
    rtRealtime.server = d.serverRecent || []
    rtRealtime.program = d.programRecent || []
    renderGauges()
  } catch { /* 网络异常时保留上次数据 */ }
}

async function loadHistory() {
  try {
    const [rs, rp] = await Promise.all([
      ResourceApi.history('server', hiRange.value),
      ResourceApi.history('program', hiRange.value)
    ])
    lastHist.server = rs.data?.points || []
    lastHist.program = rp.data?.points || []
    rebuildSeries()
    drawAll()
    renderGauges()
  } catch { /* 保留上次数据 */ }
}

async function refreshAll() {
  loading.value = true
  try {
    await loadSnapshot()
    await loadHistory()
  } finally {
    loading.value = false
  }
}

function onRangeChange() {
  loadHistory()
}

// ============ 生命周期 ============
let pollTimer = null
const resizeHandler = () => {
  for (const c of [gaugeCpu, gaugeMem, chCpu, chMem, chDisk, chIo]) c.value?.resize()
}

onMounted(async () => {
  await nextTick()
  gaugeCpu.value = echarts.init(cpuGaugeEl.value)
  gaugeMem.value = echarts.init(memGaugeEl.value)
  chCpu.value = echarts.init(cpuChartEl.value)
  chMem.value = echarts.init(memChartEl.value)
  chDisk.value = echarts.init(diskChartEl.value)
  chIo.value = echarts.init(ioChartEl.value)
  renderGauges()

  try {
    const cfg = await ConfigApi.get()
    enabled.value = cfg.data?.resource?.enabled !== false
  } catch { /* 配置读取失败时默认已启用 */ }

  await loadSnapshot()
  await loadHistory()

  window.addEventListener('resize', resizeHandler)
  polling()
})

function polling() {
  clearTimeout(pollTimer)
  const delay = Math.max(1000, (samplingInterval.value || 5) * 1000)
  pollTimer = setTimeout(() => {
    loadSnapshot()
      .then(() => liveRefresh())
      .finally(polling)
  }, delay)
}

onUnmounted(() => {
  clearTimeout(pollTimer)
  window.removeEventListener('resize', resizeHandler)
  for (const c of [gaugeCpu, gaugeMem, chCpu, chMem, chDisk, chIo]) c.value?.dispose()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-resource {
  padding: @spacing-md @spacing-lg;

  .header-section {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 16px;

    h2 { margin: 0 0 4px; font-size: 20px; color: @text-color; }
    .description { margin: 0; color: @text-color-secondary; font-size: 13px; }
    .header-right { display: flex; align-items: center; gap: 8px; }
  }

  .enable-tip { margin-bottom: 16px; }

  .metric-grid {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 16px;
    margin-bottom: 20px;

    .metric-card {
      .metric-title { font-size: 13px; color: @text-color-secondary; margin-bottom: 8px; }

      .gauge-box { width: 100%; height: 180px; }
      .gauge-legend {
        display: flex;
        justify-content: center;
        gap: 16px;
        margin-top: 2px;
        font-size: 12px;
        color: @text-color;
        .legend-item { display: inline-flex; align-items: center; gap: 4px; }
        .dot { width: 8px; height: 8px; border-radius: 50%; display: inline-block; }
      }

      .disk-usage { padding: 12px 4px; }
      .disk-row {
        display: flex;
        align-items: center;
        gap: 8px;
        margin-bottom: 14px;
        .disk-label { width: 46px; flex: none; font-size: 12px; color: @text-color-secondary; }
        .disk-bar {
          flex: 1; height: 14px; border-radius: 7px;
          background: #f0f2f5; overflow: hidden;
        }
        .disk-fill { height: 100%; border-radius: 7px; transition: width .4s ease; }
        .disk-pct { width: 42px; text-align: right; font-size: 13px; font-weight: 600; color: @text-color; }
      }
      .disk-note { font-size: 11px; color: @text-color-placeholder; }

      .io-numeric { padding: 10px 2px; display: flex; flex-direction: column; gap: 14px; }
      .io-row { display: flex; align-items: baseline; gap: 10px; font-size: 13px; }
      .io-scope {
        flex: none; font-size: 12px; color: @text-color-secondary;
      }
      .io-scope.info { color: @text-color; }
      .io-val { font-variant-numeric: tabular-nums; color: @text-color; }
      .io-val b.read { color: #d03050; font-weight: 600; }
      .io-val b.write { color: #18a058; font-weight: 600; }
    }
  }

  .chart-section {
    margin-bottom: 20px;

    .chart-header {
      display: flex;
      align-items: center;
      justify-content: space-between;
      flex-wrap: wrap;
      gap: 10px;
      margin-bottom: 12px;

      h3 { margin: 0; font-size: 15px; color: @text-color; }
      .chart-actions { display: flex; align-items: center; gap: 10px; }
    }

    .chart-grid {
      display: grid;
      grid-template-columns: repeat(2, 1fr);
      gap: 16px;

      .chart-card { :deep(.el-card__body) { padding: 12px; } }
      .chart-box { width: 100%; height: 290px; }
    }
  }
}

@media (@tablet) {
  .admin-resource {
    .metric-grid { grid-template-columns: repeat(2, 1fr); }
    .chart-grid { grid-template-columns: 1fr; }
  }
}
</style>