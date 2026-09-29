<template>
  <div class="admin-resource">
    <div class="header-section">
      <div class="header-left">
        <h2>服务器资源监控</h2>
        <p class="description">服务器整体与本程序的 CPU、内存、磁盘占用、磁盘 IO 与网络流量（实时 + 历史）</p>
      </div>
      <div class="header-right">
        <el-tag v-if="!enabled" type="warning">资源监控未启用</el-tag>
        <el-tag v-else :type="collectorRunning ? 'success' : 'info'" effect="light">
          {{ collectorRunning ? '采集运行中' : '等待首帧' }}
        </el-tag>
        <el-tag v-if="enabled && realtimeMode" type="success" effect="plain" size="small">
          {{ realtimeMode === 'sse' ? '实时推送' : '轮询' }}
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

    <!-- ============ 服务器配置 ============ -->
    <section class="hardware-section">
      <el-card shadow="never" class="hardware-card">
        <div class="hardware-head">
          <span class="hw-hostname">{{ hardware.hostname || '服务器配置' }}</span>
          <span class="hw-meta">
            <template v-if="hardware.os">{{ hardware.os }}</template>
            <template v-if="hardware.arch"> · {{ hardware.arch }}</template>
            <template v-if="hardware.kernel"> · 内核 {{ hardware.kernel }}</template>
          </span>
        </div>
        <div class="hardware-grid">
          <div class="hw-item">
            <div class="hw-label">CPU</div>
            <div class="hw-value" :title="hardware.cpu?.model || ''">{{ hardware.cpu?.model || '-' }}</div>
            <div class="hw-sub">
              <template v-if="hardware.cpu?.cores || hardware.cpu?.threads">
                {{ hardware.cpu?.cores || '-' }} 核 / {{ hardware.cpu?.threads || '-' }} 线程
              </template>
              <template v-if="hardware.cpu?.freqMhz"> · {{ fmtGHz(hardware.cpu.freqMhz) }}</template>
              <template v-if="hardware.cpu?.cacheSize"> · 缓存 {{ fmtCache(hardware.cpu.cacheSize) }}</template>
            </div>
          </div>
          <div class="hw-item">
            <div class="hw-label">内存</div>
            <div class="hw-value">{{ fmtCapacity(hardware.memory?.total) }}</div>
            <div class="hw-sub">
              <template v-if="hardware.memory?.available">可用 {{ fmtCapacity(hardware.memory.available) }}</template>
              <template v-if="hardware.memory?.swapTotal"> · Swap {{ fmtCapacity(hardware.memory.swapTotal) }}</template>
            </div>
          </div>
          <div class="hw-item">
            <div class="hw-label">磁盘</div>
            <div class="hw-value">{{ (hardware.disks?.length || 0) }} 块 · {{ fmtCapacity(hardware.diskTotal) }}</div>
            <div class="hw-sub">{{ diskSummary }}</div>
          </div>
          <div class="hw-item">
            <div class="hw-label">网卡</div>
            <div class="hw-value">{{ (hardware.nics?.length || 0) }} 块</div>
            <div class="hw-sub">{{ nicSummary }}</div>
          </div>
        </div>
      </el-card>
    </section>

    <!-- ============ 顶部指标块 ============ -->
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
        <div class="mem-detail">
          <div class="mem-line">
            <span class="mem-scope">服务器</span>
            已用 <b>{{ fmtGB(server.memory) }}</b> / 总 <b>{{ fmtGB(server.memoryTotal) }}</b>
          </div>
          <div class="mem-line">
            <span class="mem-scope">本程序</span>
            已用 <b>{{ fmtGB(program.memory) }}</b>
          </div>
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
          <div class="disk-note">服务器 {{ fmtBytes(server.diskUsed) }} / {{ fmtBytes(server.diskTotal) }} · 本程序占整个存储 {{ fmtBytes(program.diskUsed) }} / {{ fmtBytes(program.diskTotal) }}</div>
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

      <!-- 网络流量：合计 + 各网卡明细 -->
      <el-card shadow="never" class="metric-card">
        <div class="metric-title">网络流量</div>
        <div class="net-usage">
          <div class="io-row info">
            <span class="io-scope">服务器</span>
            <span class="io-val"><b class="recv">收 ↓</b> {{ fmtRate(server.netRecv) }}</span>
            <span class="io-val"><b class="sent">发 ↑</b> {{ fmtRate(server.netSent) }}</span>
          </div>
          <div class="net-nics">
            <div v-for="nic in topNICs" :key="nic.name" class="net-nic-row">
              <span class="net-nic-name" :title="nic.name">{{ nic.name }}</span>
              <span class="net-nic-val">↓ {{ fmtRate(nic.recv) }}</span>
              <span class="net-nic-val">↑ {{ fmtRate(nic.sent) }}</span>
            </div>
            <div v-if="!topNICs.length" class="net-empty">暂无网卡速率数据</div>
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
        <el-card shadow="never" class="chart-card wide"><div ref="netChartEl" class="chart-box"></div></el-card>
      </div>
    </section>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, nextTick, watch } from 'vue'
import * as echarts from 'echarts/lib/echarts'
import 'echarts/lib/chart/gauge'
import 'echarts/lib/chart/line'
import 'echarts/lib/component/grid'
import 'echarts/lib/component/tooltip'
import 'echarts/lib/component/title'
import 'echarts/lib/component/legend'
import { Refresh } from '@element-plus/icons-vue'
import { ConfigApi, ResourceApi } from '@/api'
import { useTheme } from '@/composables/useTheme'

// ============ 状态 ============
const loading = ref(false)
const enabled = ref(true)
const collectorRunning = ref(false)
const samplingInterval = ref(5)
const hiRange = ref('1h')
// 实时数据来源：sse（服务端推送）/ polling（轮询兜底），未启用资源监控时为空
const realtimeMode = ref('')
const netNICs = ref([])

const emptyPoint = {
  cpu: 0, memory: 0, memoryTotal: 0,
  diskUsed: 0, diskTotal: 0, diskIoRead: 0, diskIOWrite: 0,
  netRecv: 0, netSent: 0
}
const server = reactive({ ...emptyPoint })
const program = reactive({ ...emptyPoint })

const hardware = reactive({
  hostname: '', os: '', arch: '', kernel: '',
  cpu: {}, memory: {}, disks: [], diskTotal: 0, nics: []
})

// ============ 格式化 ============
const fmtPercent = (v) => (v == null ? '-' : `${Number(v).toFixed(1)}%`)
const memPercent = ({ memory, memoryTotal }) =>
  memoryTotal > 0 ? Math.min(100, Math.round((memory / memoryTotal) * 1000) / 10) : 0
const diskPercent = ({ diskUsed, diskTotal }) =>
  diskTotal > 0 ? Math.min(100, Math.round((diskUsed / diskTotal) * 1000) / 10) : 0

// 容量展示：统一以 G 为主单位（内存等小容量场景更直观），超过 1024G 折算为 T
function fmtCapacity(bytes) {
  const n = Number(bytes) || 0
  if (n <= 0) return '0 G'
  const g = n / 1024 ** 3
  if (g >= 1024) return `${(g / 1024).toFixed(2)} T`
  return `${g >= 100 ? g.toFixed(0) : g.toFixed(1)} G`
}
// 内存具体值：固定 G 单位，保留两位小数
const fmtGB = (bytes) => `${((Number(bytes) || 0) / 1024 ** 3).toFixed(2)} G`
const fmtGHz = (mhz) => `${(Number(mhz) / 1000).toFixed(2)} GHz`
const fmtCache = (kb) => (kb >= 1024 ? `${(kb / 1024).toFixed(1)} MB` : `${kb} KB`)

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

const topNICs = computed(() => netNICs.value.slice(0, 3))
const diskSummary = computed(() => {
  const d = hardware.disks || []
  if (!d.length) return '未采集到磁盘信息'
  const head = d.slice(0, 3).map((x) => `${x.mountpoint || x.device} ${fmtCapacity(x.total)}`).join(' · ')
  return d.length > 3 ? `${head} 等 ${d.length} 项` : head
})
const nicSummary = computed(() => {
  const n = hardware.nics || []
  if (!n.length) return '未采集到网卡信息'
  const head = n.slice(0, 3).map((x) => `${x.name} ${x.speedMbps ? x.speedMbps + ' Mbps' : '速率未知'}`).join(' · ')
  return n.length > 3 ? `${head} 等 ${n.length} 块` : head
})

const ORANGE = '#FFA500'
const BLUE = '#2080f0'
// 占用率 label 的 rich 文本样式：名称用灰色小字，数值用对应 scope 的颜色加粗
const labelRich = {
  n: { fontSize: 11, color: '#909399' },
  sv: { fontSize: 12, fontWeight: 'bold', color: ORANGE, padding: [0, 8, 0, 2] },
  pv: { fontSize: 12, fontWeight: 'bold', color: BLUE, padding: [0, 0, 0, 2] }
}
const { isDark } = useTheme()

// ============ ECharts 实例 ============
const cpuGaugeEl = ref(null)
const memGaugeEl = ref(null)
const cpuChartEl = ref(null)
const memChartEl = ref(null)
const diskChartEl = ref(null)
const ioChartEl = ref(null)
const netChartEl = ref(null)

const gaugeCpu = ref(null)
const gaugeMem = ref(null)
const chCpu = ref(null)
const chMem = ref(null)
const chDisk = ref(null)
const chIo = ref(null)
const chNet = ref(null)

// 实时缓冲（内存 5 秒采样）与最近一次历史数据，用于整合渲染
const rtRealtime = { server: [], program: [] }
const lastHist = { server: [], program: [] }

// ============ 顶部仪表盘 ============
// 单仪表盘承载服务器/本程序两条指针；当前 ECharts 为 v4，gauge 的 title/detail 仅支持
// 系列级配置，故用一条 rich 文本同时展示两个 scope 的占用率 label。
function gaugeBaseOption() {
  return {
    series: [{
      type: 'gauge', min: 0, max: 100, startAngle: 210, endAngle: -30,
      radius: '82%', center: ['50%', '50%'],
      axisLine: { lineStyle: { width: 10 } },
      axisTick: { show: false }, splitLine: { show: false },
      axisLabel: { show: false },
      pointer: { show: true, length: '55%', width: 5 },
      title: { show: false },
      detail: {
        show: true, width: 'auto', offsetCenter: ['0%', '58%'],
        rich: labelRich, padding: [0, 0]
      },
      data: []
    }]
  }
}

function renderGauges() {
  const cpuData = [
    { value: +Number(server.cpu || 0).toFixed(1), name: '服务器', itemStyle: { color: ORANGE } },
    { value: +Number(program.cpu || 0).toFixed(1), name: '本程序', itemStyle: { color: BLUE } }
  ]
  gaugeCpu.value?.setOption({
    series: [{
      ...gaugeBaseOption().series[0],
      detail: {
        ...gaugeBaseOption().series[0].detail,
        formatter: () => `{n|服务器} {sv|${fmtPercent(server.cpu)}}\n{n|本程序} {pv|${fmtPercent(program.cpu)}}`
      },
      data: cpuData
    }]
  }, true)

  const memData = [
    { value: memPercent(server), name: '服务器', itemStyle: { color: ORANGE } },
    { value: memPercent(program), name: '本程序', itemStyle: { color: BLUE } }
  ]
  gaugeMem.value?.setOption({
    series: [{
      ...gaugeBaseOption().series[0],
      detail: {
        ...gaugeBaseOption().series[0].detail,
        formatter: () => `{n|服务器} {sv|${fmtPercent(memPercent(server))}}\n{n|本程序} {pv|${fmtPercent(memPercent(program))}}`
      },
      data: memData
    }]
  }, true)
}

// ============ 曲线（实时+历史整合，服务器与程序同图，动态追加刷新） ============
// 数据点统一存储为 [timestamp, value...]；内存序列额外携带使用量/总量字节用于 tooltip。
const seriesData = {}
const seriesKeys = [
  'cpu-s', 'cpu-p', 'mem-s', 'mem-p',
  'disk-s', 'disk-p', 'ioR-s', 'ioR-p', 'ioW-s', 'ioW-p',
  'netR-s', 'netS-s'
]
for (const k of seriesKeys) seriesData[k] = []

const seriesVal = {
  'cpu-s': (pt) => [+Number(pt.cpu || 0).toFixed(2)],
  'cpu-p': (pt) => [+Number(pt.cpu || 0).toFixed(2)],
  'mem-s': (pt) => [memPercent(pt), pt.memory || 0, pt.memoryTotal || 0],
  'mem-p': (pt) => [memPercent(pt), pt.memory || 0, pt.memoryTotal || 0],
  'disk-s': (pt) => [pt.diskUsed || 0],
  'disk-p': (pt) => [pt.diskUsed || 0],
  'ioR-s': (pt) => [pt.diskIoRead || 0],
  'ioR-p': (pt) => [pt.diskIoRead || 0],
  'ioW-s': (pt) => [pt.diskIOWrite || 0],
  'ioW-p': (pt) => [pt.diskIOWrite || 0],
  'netR-s': (pt) => [pt.netRecv || 0],
  'netS-s': (pt) => [pt.netSent || 0]
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

function axisTheme() {
  return isDark.value
    ? { tick: '#8a8f98', line: '#3a3f45', text: '#c8ccd2' }
    : { tick: '#909399', line: '#e4e7ed', text: '#606266' }
}

function mkOption({ title, yMax, yName, yFormatter, valueFormatter, customTooltip, series, withLabels = false }) {
  const t = axisTheme()
  return {
    title: {
      text: title,
      subtext: '',
      textStyle: { fontSize: 13, fontWeight: 600, color: t.text },
      subtextStyle: { rich: labelRich },
      left: 4, top: 2
    },
    legend: { top: 2, right: 8, itemWidth: 12, itemHeight: 8, textStyle: { fontSize: 11, color: t.text } },
    grid: { left: 52, right: 16, top: withLabels ? 48 : 36, bottom: 28 },
    xAxis: { type: 'time', min: () => Date.now() - rangeMs(hiRange.value), max: () => Date.now(), axisLine: { lineStyle: { color: t.line } }, axisLabel: { fontSize: 10, color: t.tick } },
    yAxis: { type: 'value', min: 0, max: yMax, name: yName, axisLine: { lineStyle: { color: t.line } }, axisLabel: { fontSize: 10, color: t.tick, formatter: yFormatter }, splitLine: { lineStyle: { color: t.line } } },
    tooltip: {
      trigger: 'axis', confine: true,
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

// 内存 tooltip：显示占用率 + 已用/总量（G）；本程序仅展示已用（RSS）
function memTooltip(params) {
  if (!params || !params.length) return ''
  const p = params[0]
  const d = new Date(p.value[0])
  const pad = (x) => String(x).padStart(2, '0')
  let html = `${d.getMonth() + 1}/${d.getDate()} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  for (const item of params) {
    const pct = Number(item.value[1] || 0)
    const used = Number(item.value[2] || 0)
    const total = Number(item.value[3] || 0)
    const detail = item.seriesName === '本程序'
      ? `(${fmtGB(used)})`
      : `(${fmtGB(used)} / ${fmtGB(total)})`
    html += `<br/>${item.marker}${item.seriesName}: <b>${pct.toFixed(1)}%</b> <span style="color:#a0a4ab">${detail}</span>`
  }
  return html
}

// ============ 动态刷新（官方 BP：持有一个 option，往数据数组 push，merge setOption） ============
// chartOpt 对象持久持有，其 series.data 引用 seriesData 数组；
// xAxis.min/max 为函数，每次渲染按 now 重新求值，实现窗口自动滚动。
const chartOpt = { cpu: null, mem: null, disk: null, io: null, net: null }
const chartRefs = { cpu: chCpu, mem: chMem, disk: chDisk, io: chIo, net: chNet }
const chartNames = ['cpu', 'mem', 'disk', 'io', 'net']

function buildChartOpt() {
  chartOpt.cpu = mkOption({
    title: 'CPU 占用率', yName: '%', yMax: 100, withLabels: true,
    valueFormatter: (v) => `${Number(v).toFixed(1)}%`,
    series: [
      lineSeries('服务器', seriesData['cpu-s'], ORANGE),
      lineSeries('本程序', seriesData['cpu-p'], BLUE)
    ]
  })

  chartOpt.mem = mkOption({
    title: '内存占用（%）', yName: '%', yMax: 100, withLabels: true,
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

  chartOpt.net = mkOption({
    title: '网络流量（服务器合计）',
    valueFormatter: (v) => fmtRate(v), yFormatter: (v) => fmtRate(v),
    series: [
      lineSeries('接收', seriesData['netR-s'], '#2080f0'),
      lineSeries('发送', seriesData['netS-s'], '#e6a23c')
    ]
  })
}

// 当前 ECharts 为 v4，折线无 endLabel；用标题副文本（rich）把最新占用率直接标在图上
function syncChartLabels() {
  if (chartOpt.cpu) {
    chartOpt.cpu.title.subtext = `{n|服务器} {sv|${fmtPercent(server.cpu)}}   {n|本程序} {pv|${fmtPercent(program.cpu)}}`
  }
  if (chartOpt.mem) {
    chartOpt.mem.title.subtext = `{n|服务器} {sv|${fmtPercent(memPercent(server))}}   {n|本程序} {pv|${fmtPercent(memPercent(program))}}`
  }
}

// 全量绘制（初始加载 / 切换时间范围）
function drawAll() {
  buildChartOpt()
  syncChartLabels()
  for (const name of chartNames) chartRefs[name].value?.setOption(chartOpt[name])
}

// 主题切换时重建 option（series.data 仍引用 seriesData 数组，不中断动态推流）
watch(isDark, () => drawAll())

function lastTs(arr) {
  let m = -Infinity
  for (const it of arr) if (it[0] > m) m = it[0]
  return m
}

// 追加式刷新：把新到达的实时点 push 进 seriesData，再 merge setOption（不整图重建）
function liveRefresh() {
  if (!chartOpt.cpu) return
  syncChartLabels()
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
  for (const name of chartNames) {
    chartRefs[name].value?.setOption(chartOpt[name])
  }
}

// ============ 数据获取 ============
function applyPoint(target, p) {
  target.cpu = p?.cpu ?? 0
  target.memory = p?.memory ?? 0
  target.memoryTotal = p?.memoryTotal ?? 0
  target.diskUsed = p?.diskUsed ?? 0
  target.diskTotal = p?.diskTotal ?? 0
  target.diskIoRead = p?.diskIoRead ?? 0
  target.diskIOWrite = p?.diskIOWrite ?? 0
  target.netRecv = p?.netRecv ?? 0
  target.netSent = p?.netSent ?? 0
}

function applyHardware(hw) {
  if (!hw) return
  hardware.hostname = hw.hostname || ''
  hardware.os = hw.os || ''
  hardware.arch = hw.arch || ''
  hardware.kernel = hw.kernel || ''
  hardware.cpu = hw.cpu || {}
  hardware.memory = hw.memory || {}
  hardware.disks = hw.disks || []
  hardware.diskTotal = hw.diskTotal || 0
  hardware.nics = hw.nics || []
}

// 应用一帧实时数据（SSE 帧与轮询快照结构一致）
function applyRealtime(d) {
  if (!d) return
  collectorRunning.value = !!d.running
  if (d.samplingInterval) samplingInterval.value = d.samplingInterval
  applyHardware(d.hardware)
  applyPoint(server, d.server)
  applyPoint(program, d.program)
  netNICs.value = Array.isArray(d.netNICs) ? d.netNICs : []
  rtRealtime.server = d.serverRecent || []
  rtRealtime.program = d.programRecent || []
  renderGauges()
  liveRefresh()
}

async function loadSnapshot() {
  try {
    const res = await ResourceApi.snapshot()
    applyRealtime(res.data || {})
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

// ============ 实时通道：SSE 优先，失败/不支持时降级为轮询 ============
let sse = null
let sseErrors = 0
let pollTimer = null

function startSSE() {
  if (typeof window === 'undefined' || !window.EventSource) { startPolling(); return }
  if (!localStorage.getItem('token')) { startPolling(); return }
  stopPolling()
  try {
    sse = new EventSource(ResourceApi.streamUrl())
  } catch {
    sse = null
    startPolling()
    return
  }
  realtimeMode.value = 'sse'
  sse.onopen = () => { sseErrors = 0 }
  sse.onmessage = (ev) => {
    sseErrors = 0
    try { applyRealtime(JSON.parse(ev.data)) } catch { /* 忽略异常帧 */ }
  }
  sse.onerror = () => {
    // EventSource 断线会自动重连；连续失败则降级为轮询，避免无谓重连
    sseErrors += 1
    if (sseErrors >= 2) {
      stopSSE()
      startPolling()
    }
  }
}

function stopSSE() {
  if (sse) { sse.close(); sse = null }
  sseErrors = 0
}

function startPolling() {
  stopPolling()
  realtimeMode.value = 'polling'
  const delay = Math.max(1000, (samplingInterval.value || 5) * 1000)
  pollTimer = setTimeout(() => {
    loadSnapshot().finally(() => {
      // 页面隐藏时不再续期，待重新可见时由 onVisibilityChange 恢复
      if (!document.hidden) startPolling()
    })
  }, delay)
}

function stopPolling() {
  if (pollTimer) { clearTimeout(pollTimer); pollTimer = null }
}

function startRealtime() {
  if (!enabled.value) return
  startSSE()
}

function stopRealtime() {
  stopSSE()
  stopPolling()
  realtimeMode.value = ''
}

// ============ 生命周期 ============
const resizeHandler = () => {
  for (const c of [gaugeCpu, gaugeMem, chCpu, chMem, chDisk, chIo, chNet]) c.value?.resize()
}

onMounted(async () => {
  await nextTick()
  gaugeCpu.value = echarts.init(cpuGaugeEl.value)
  gaugeMem.value = echarts.init(memGaugeEl.value)
  chCpu.value = echarts.init(cpuChartEl.value)
  chMem.value = echarts.init(memChartEl.value)
  chDisk.value = echarts.init(diskChartEl.value)
  chIo.value = echarts.init(ioChartEl.value)
  chNet.value = echarts.init(netChartEl.value)
  renderGauges()

  try {
    const cfg = await ConfigApi.get()
    enabled.value = cfg.data?.resource?.enabled !== false
  } catch { /* 配置读取失败时默认已启用 */ }

  await loadSnapshot()
  await loadHistory()

  window.addEventListener('resize', resizeHandler)
  document.addEventListener('visibilitychange', onVisibilityChange)
  startRealtime()
})

// 页面切换到后台时断开实时通道（不发起请求）；重新可见时立即同步一次并恢复
function onVisibilityChange() {
  if (document.hidden) {
    stopRealtime()
  } else {
    loadSnapshot().finally(() => startRealtime())
  }
}

onUnmounted(() => {
  stopRealtime()
  document.removeEventListener('visibilitychange', onVisibilityChange)
  window.removeEventListener('resize', resizeHandler)
  for (const c of [gaugeCpu, gaugeMem, chCpu, chMem, chDisk, chIo, chNet]) c.value?.dispose()
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

  .hardware-section {
    margin-bottom: 16px;

    .hardware-card {
      :deep(.el-card__body) { padding: 14px 18px; }

      .hardware-head {
        display: flex;
        align-items: baseline;
        flex-wrap: wrap;
        gap: 10px;
        margin-bottom: 12px;
        padding-bottom: 10px;
        border-bottom: 1px solid var(--el-border-color-lighter);

        .hw-hostname { font-size: 15px; font-weight: 600; color: @text-color; }
        .hw-meta { font-size: 12px; color: @text-color-secondary; }
      }

      .hardware-grid {
        display: grid;
        grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
        gap: 14px 24px;
      }

      .hw-item {
        min-width: 0;

        .hw-label {
          font-size: 12px;
          color: @text-color-secondary;
          margin-bottom: 4px;
        }
        .hw-value {
          font-size: 14px;
          font-weight: 600;
          color: @text-color;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }
        .hw-sub {
          margin-top: 2px;
          font-size: 12px;
          color: @text-color-placeholder;
          line-height: 1.5;
          word-break: break-all;
        }
      }
    }
  }

  .metric-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: 16px;
    margin-bottom: 20px;

    .metric-card {
      .metric-title { font-size: 13px; color: @text-color-secondary; margin-bottom: 8px; }

      .gauge-box { width: 100%; height: 200px; }
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
      .mem-detail {
        margin-top: 6px;
        padding-top: 6px;
        border-top: 1px dashed var(--el-border-color-lighter);
        font-size: 12px;
        color: @text-color-secondary;
        line-height: 1.7;
        .mem-scope { display: inline-block; width: 46px; color: @text-color-placeholder; }
        b { color: @text-color; font-variant-numeric: tabular-nums; }
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
          background: var(--el-fill-color-light); overflow: hidden;
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
      .io-val b.recv { color: #2080f0; font-weight: 600; }
      .io-val b.sent { color: #e6a23c; font-weight: 600; }

      .net-usage { padding: 10px 2px; display: flex; flex-direction: column; gap: 10px; }
      .net-nics {
        display: flex;
        flex-direction: column;
        gap: 6px;
        padding-top: 8px;
        border-top: 1px dashed var(--el-border-color-lighter);
      }
      .net-nic-row {
        display: flex;
        align-items: baseline;
        gap: 8px;
        font-size: 12px;
        color: @text-color-secondary;
      }
      .net-nic-name {
        flex: 1;
        min-width: 0;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
      }
      .net-nic-val { flex: none; font-variant-numeric: tabular-nums; color: @text-color; }
      .net-empty { font-size: 12px; color: @text-color-placeholder; }
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
      .chart-card.wide { grid-column: 1 / -1; }
      .chart-box { width: 100%; height: 290px; }
    }
  }
}

@media (@tablet) {
  .admin-resource {
    .metric-grid { grid-template-columns: repeat(2, 1fr); }
    .chart-grid { grid-template-columns: 1fr; }
    .chart-section .chart-grid .chart-card.wide { grid-column: auto; }
  }
}
</style>