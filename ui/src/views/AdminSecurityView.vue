<template>
  <div class="admin-security">
    <!-- 头部 -->
    <div class="header-section">
      <div class="header-left">
        <h2>安全事件</h2>
        <p class="description">
          记录被识别为攻击 / 越权 / 异常请求的风险事件，支撑安全分析与处置
        </p>
      </div>
      <div class="header-right">
        <el-button v-if="unreadCount > 0" size="small" @click="markAllRead">
          <el-icon><Check /></el-icon>
          全部标记已读
        </el-button>
        <el-button type="primary" size="small" :loading="loading" @click="loadAll">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </div>

    <!-- 严重级别统计卡片 -->
    <div class="stat-cards">
      <div class="stat-card" v-for="c in statCards" :key="c.key" @click="applyStatFilter(c)">
        <div class="stat-label">{{ c.label }}</div>
        <div class="stat-value" :class="c.key.toLowerCase()">{{ c.value }}</div>
        <div class="stat-sub" v-if="c.sub">{{ c.sub }}</div>
      </div>
    </div>

    <!-- 基础统计 -->
    <div class="stats-section" v-loading="statsLoading">
      <div class="stats-panel trend-panel">
        <div class="panel-title">近 7 日事件趋势</div>
        <div class="trend-chart">
          <div v-for="p in stats.trend" :key="p.date" class="trend-col">
            <div class="trend-bars">
              <i class="bar high" :style="{ height: barH(p.high, maxDay) }" :title="'高危 ' + p.high"></i>
              <i class="bar medium" :style="{ height: barH(p.medium, maxDay) }" :title="'中危 ' + p.medium"></i>
              <i class="bar low" :style="{ height: barH(p.low, maxDay) }" :title="'低危 ' + p.low"></i>
            </div>
            <div class="trend-label">{{ shortDate(p.date) }}</div>
          </div>
        </div>
      </div>
      <div class="stats-panel">
        <div class="panel-title">TOP 来源 IP（近 7 日）</div>
        <ul class="top-list">
          <li v-for="(t, i) in stats.topIPs" :key="t.label">
            <span class="rank">{{ i + 1 }}</span>
            <span class="top-label mono">{{ t.label }}</span>
            <span class="top-count">{{ t.n }}</span>
          </li>
          <li v-if="!stats.topIPs.length" class="empty">暂无数据</li>
        </ul>
      </div>
      <div class="stats-panel">
        <div class="panel-title">TOP 事件类型（近 7 日）</div>
        <ul class="top-list">
          <li v-for="(t, i) in stats.topTypes" :key="t.label">
            <span class="rank">{{ i + 1 }}</span>
            <span class="top-label">{{ eventTypeLabel(t.label) }}</span>
            <span class="top-count">{{ t.n }}</span>
          </li>
          <li v-if="!stats.topTypes.length" class="empty">暂无数据</li>
        </ul>
      </div>
    </div>

    <!-- 近期锁定事件 -->
    <div class="lock-panel" v-if="stats.locks && stats.locks.length">
      <div class="panel-title">近期锁定事件（高危）</div>
      <el-table :data="stats.locks" size="small" :border="false" style="width: 100%">
        <el-table-column label="时间" width="190">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="事件" width="160">
          <template #default="{ row }">{{ eventTypeLabel(row.eventType) }}</template>
        </el-table-column>
        <el-table-column label="来源 IP" width="160">
          <template #default="{ row }"><span class="mono">{{ row.clientIP || '-' }}</span></template>
        </el-table-column>
        <el-table-column label="目标" min-width="180">
          <template #default="{ row }">{{ row.target || '-' }}</template>
        </el-table-column>
        <el-table-column label="说明" min-width="220">
          <template #default="{ row }">{{ row.reason || '-' }}</template>
        </el-table-column>
      </el-table>
    </div>

    <!-- 事件列表 -->
    <div class="filter-section">
      <el-select v-model="filters.severity" placeholder="严重级别" clearable size="small" style="width: 120px">
        <el-option v-for="(m, k) in severityMeta" :key="k" :label="m.label" :value="k" />
      </el-select>
      <el-select v-model="filters.eventType" placeholder="事件类型" clearable size="small" style="width: 180px">
        <el-option v-for="(m, k) in eventTypeMeta" :key="k" :label="m" :value="k" />
      </el-select>
      <el-input v-model="filters.clientIP" placeholder="来源 IP" clearable size="small" style="width: 150px" />
      <el-input v-model="filters.identity" placeholder="身份" clearable size="small" style="width: 130px" />
      <el-input v-model="filters.q" placeholder="目标/路径/请求ID 关键字" clearable size="small" style="width: 200px" @keyup.enter="onSearch" />
      <el-date-picker v-model="dateRange" type="daterange" size="small" range-separator="至"
        start-placeholder="开始" end-placeholder="结束" value-format="YYYY-MM-DD" style="width: 250px" />
      <el-checkbox v-model="filters.unread" size="small">仅未读</el-checkbox>
      <div class="filter-actions">
        <el-button size="small" type="primary" :loading="loading" @click="onSearch">查询</el-button>
        <el-button size="small" @click="onReset">重置</el-button>
      </div>
    </div>

    <div ref="wrapRef" class="table-wrap" v-loading="loading">
      <el-table :data="eventList" size="small" :border="false" stripe :max-height="tableHeight"
        @row-click="openDetail">
        <el-table-column label="时间" width="180">
          <template #default="{ row }">{{ formatTime(row.createdAt) }}</template>
        </el-table-column>
        <el-table-column label="严重级别" width="90">
          <template #default="{ row }">
            <el-tag :type="severityMeta[row.severity]?.type" size="small" effect="light">
              {{ severityMeta[row.severity]?.label || row.severity }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="事件类型" width="150">
          <template #default="{ row }">{{ eventTypeLabel(row.eventType) }}</template>
        </el-table-column>
        <el-table-column label="来源 IP" width="150">
          <template #default="{ row }"><span class="mono">{{ row.clientIP || '-' }}</span></template>
        </el-table-column>
        <el-table-column label="身份" width="110">
          <template #default="{ row }">{{ row.identity || row.identity === '' ? (row.identity || 'anonymous') : '-' }}</template>
        </el-table-column>
        <el-table-column label="目标" min-width="160">
          <template #default="{ row }">{{ row.target || '-' }}</template>
        </el-table-column>
        <el-table-column label="路径" min-width="220">
          <template #default="{ row }">
            <span class="path-cell" :title="row.path || ''">{{ row.path || '-' }}</span>
          </template>
        </el-table-column>
        <el-table-column label="状态" width="80">
          <template #default="{ row }">{{ row.statusCode || '-' }}</template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && eventList.length === 0" description="没有符合条件的风险事件" :image-size="80" />
      <div class="pagination-wrap" v-if="total > 0">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="loadEvents"
          @size-change="onPageSizeChange"
        />
      </div>
    </div>

    <!-- 事件详情 -->
    <el-dialog v-model="detailVisible" title="安全事件详情" width="640px" append-to-body>
      <el-descriptions v-if="current" :column="2" border size="small">
        <el-descriptions-item label="事件 ID">{{ current.id }}</el-descriptions-item>
        <el-descriptions-item label="严重级别">
          <el-tag :type="severityMeta[current.severity]?.type" size="small">
            {{ severityMeta[current.severity]?.label || current.severity }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="事件类型">{{ eventTypeLabel(current.eventType) }}</el-descriptions-item>
        <el-descriptions-item label="时间">{{ formatTime(current.createdAt) }}</el-descriptions-item>
        <el-descriptions-item label="来源 IP"><span class="mono">{{ current.clientIP }}</span></el-descriptions-item>
        <el-descriptions-item label="身份">{{ current.identity }}</el-descriptions-item>
        <el-descriptions-item label="方法">{{ current.httpMethod || '-' }}</el-descriptions-item>
        <el-descriptions-item label="状态码">{{ current.statusCode || '-' }}</el-descriptions-item>
        <el-descriptions-item label="防护机制">{{ guardLabel(current.guard) }}</el-descriptions-item>
        <el-descriptions-item label="结果">{{ current.result || 'failure' }}</el-descriptions-item>
        <el-descriptions-item label="操作">{{ current.operation || '-' }}</el-descriptions-item>
        <el-descriptions-item label="请求 ID"><span class="mono">{{ current.requestID || '-' }}</span></el-descriptions-item>
        <el-descriptions-item label="目标"><span class="wrap">{{ current.target || '-' }}</span></el-descriptions-item>
        <el-descriptions-item label="说明" :span="2"><span class="wrap">{{ current.reason || '-' }}</span></el-descriptions-item>
        <el-descriptions-item label="请求路径" :span="2"><span class="wrap break">{{ current.path || '-' }}</span></el-descriptions-item>
        <el-descriptions-item label="User-Agent" :span="2"><span class="wrap break">{{ current.userAgent || '-' }}</span></el-descriptions-item>
      </el-descriptions>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { Refresh, Check } from '@element-plus/icons-vue'
import { SecurityApi } from '@/api'

const severityMeta = {
  high: { label: '高危', type: 'danger' },
  medium: { label: '中危', type: 'warning' },
  low: { label: '低危', type: 'info' }
}
const eventTypeMeta = {
  'auth.failed_login': '登录/注册失败',
  'auth.locked': '登录被锁定',
  'auth.unauthorized': '认证失败(401)',
  'access.forbidden': '越权访问(403)',
  'rate.limited': '触发限流(429)',
  'code.bruteforce': '口令/分享码暴破'
}
const guardMeta = { auth: '认证/授权', accessguard: '访问防护', webdav: 'WebDAV', ftp: 'FTP' }

const eventTypeLabel = (t) => eventTypeMeta[t] || t || '-'
const guardLabel = (g) => guardMeta[g] || g || '-'

// ============ 统计卡片 ============
const unread = ref({ total: 0, high: 0 })
const dist = ref({ total: 0, high: 0, medium: 0, low: 0 })
const statCards = computed(() => [
  { key: 'high', label: '高危（未读）', value: unread.value.high, sub: `全部高危 ${dist.value.high}` },
  { key: 'medium', label: '中危', value: dist.value.medium, sub: '' },
  { key: 'low', label: '低危', value: dist.value.low, sub: '' },
  { key: 'total', label: '事件总数', value: dist.value.total, sub: `未读 ${unread.value.total}` }
])
const unreadCount = computed(() => unread.value.total)

// ============ 基础统计 ============
const statsLoading = ref(false)
const stats = ref({ trend: [], topIPs: [], topTypes: [], locks: [] })
const maxDay = computed(() => {
  let max = 0
  for (const p of stats.value.trend) {
    max = Math.max(max, p.high, p.medium, p.low)
  }
  return max || 1
})
const barH = (n, max) => `${Math.max(2, (n / max) * 100)}%`
const shortDate = (d) => (d || '').slice(5)

// ============ 事件列表 ============
const loading = ref(false)
const eventList = ref([])
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)
const dateRange = ref(null)
const filters = ref({ severity: '', eventType: '', clientIP: '', identity: '', q: '', unread: false })

const buildParams = () => {
  const p = { page: page.value, pageSize: pageSize.value }
  if (filters.value.severity) p.severity = filters.value.severity
  if (filters.value.eventType) p.eventType = filters.value.eventType
  if (filters.value.clientIP) p.clientIP = filters.value.clientIP
  if (filters.value.identity) p.identity = filters.value.identity
  if (filters.value.q) p.q = filters.value.q
  if (filters.value.unread) p.unread = '1'
  if (dateRange.value && dateRange.value.length === 2) {
    p.from = dateRange.value[0]
    p.to = dateRange.value[1]
  }
  return p
}

// ============ 详情 ============
const detailVisible = ref(false)
const current = ref(null)
const openDetail = (row) => {
  current.value = row
  detailVisible.value = true
}

// ============ 数据加载 ============
const loadStats = async () => {
  statsLoading.value = true
  try {
    const res = await SecurityApi.stats()
    if (res.success && res.data) {
      stats.value = {
        trend: res.data.trend || [],
        topIPs: res.data.topIPs || [],
        topTypes: res.data.topTypes || [],
        locks: res.data.locks || []
      }
      dist.value = res.data.severity || dist.value
    }
  } catch (e) {
    // 忽略统计失败
  } finally {
    statsLoading.value = false
  }
}

const loadUnread = async () => {
  try {
    const res = await SecurityApi.unreadSummary()
    if (res.success && res.data) {
      unread.value = { total: res.data.total || 0, high: res.data.high || 0 }
    }
  } catch (e) {
    // 忽略
  }
}

const loadEvents = async () => {
  loading.value = true
  try {
    const res = await SecurityApi.list(buildParams())
    if (res.success && res.data) {
      eventList.value = res.data.list || []
      total.value = res.data.total || 0
    }
  } catch (e) {
    eventList.value = []
    total.value = 0
  } finally {
    loading.value = false
  }
}

const loadAll = () => {
  loadStats()
  loadUnread()
  loadEvents()
}

const onSearch = () => {
  page.value = 1
  loadEvents()
}

const onReset = () => {
  filters.value = { severity: '', eventType: '', clientIP: '', identity: '', q: '', unread: false }
  dateRange.value = null
  page.value = 1
  loadEvents()
}

const onPageSizeChange = () => {
  page.value = 1
  loadEvents()
}

const applyStatFilter = (c) => {
  if (c.key === 'high') {
    filters.value.severity = 'high'
    filters.value.unread = true
  } else if (c.key !== 'total') {
    filters.value.severity = c.key
    filters.value.unread = false
  }
  onSearch()
}

const markAllRead = async () => {
  await SecurityApi.markAllRead()
  unread.value = { total: 0, high: 0 }
}

const formatTime = (iso) => {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

// ============ 表格高度 ============
const wrapRef = ref(null)
const tableHeight = ref(400)
let tableResizeObs = null
const updateTableSize = () => {
  const el = wrapRef.value
  if (el) {
    const height = el.clientHeight || 400
    tableHeight.value = height > 60 ? height - 61 : height
  }
}

onMounted(() => {
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (wrapRef.value) tableResizeObs.observe(wrapRef.value)
  loadAll()
})

onUnmounted(() => {
  tableResizeObs?.disconnect()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-security {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: @container-padding;
  overflow: auto;

  .header-section {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;

    .header-left {
      h2 { margin: 0; font-size: @font-size-lg; }
      .description {
        margin: 4px 0 0;
        color: @text-color-placeholder;
        font-size: @font-size-sm;
      }
    }
    .header-right { display: flex; align-items: center; gap: 12px; }
  }

  .stat-cards {
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 12px;
    margin-bottom: 16px;

    .stat-card {
      background: @bg-color;
      border-radius: @content-radius;
      box-shadow: @shadow-sm;
      padding: 14px 16px;
      cursor: pointer;
      transition: box-shadow @transition-fast;

      &:hover { box-shadow: @shadow-md; }

      .stat-label {
        font-size: @font-size-sm;
        color: @text-color-secondary;
      }
      .stat-value {
        margin-top: 6px;
        font-size: 24px;
        font-weight: 600;
        &.high { color: @error-color; }
        &.medium { color: @warning-color; }
        &.low { color: @info-color; }
        &.total { color: @primary-color; }
      }
      .stat-sub {
        margin-top: 2px;
        font-size: @font-size-xs;
        color: @text-color-placeholder;
      }
    }
  }

  .stats-section {
    display: grid;
    grid-template-columns: 1.6fr 1fr 1fr;
    gap: 12px;
    margin-bottom: 16px;

    .stats-panel {
      background: @bg-color;
      border-radius: @content-radius;
      box-shadow: @shadow-sm;
      padding: 14px 16px;

      .panel-title {
        font-weight: 500;
        font-size: @font-size-sm;
        margin-bottom: 12px;
        color: @text-color;
      }
    }

    .trend-chart {
      display: flex;
      align-items: flex-end;
      gap: 8px;
      height: 140px;

      .trend-col {
        flex: 1;
        display: flex;
        flex-direction: column;
        align-items: center;
        height: 100%;

        .trend-bars {
          flex: 1;
          width: 100%;
          display: flex;
          align-items: flex-end;
          gap: 3px;

          .bar {
            flex: 1;
            border-radius: 2px 2px 0 0;
            min-height: 2px;
            &.high { background: @error-color; }
            &.medium { background: @warning-color; }
            &.low { background: ~'var(--el-color-info, #909399)'; }
          }
        }
        .trend-label {
          margin-top: 6px;
          font-size: @font-size-xs;
          color: @text-color-placeholder;
        }
      }
    }

    .top-list {
      list-style: none;
      margin: 0;
      padding: 0;
      height: 140px;
      overflow: auto;

      li {
        display: flex;
        align-items: center;
        gap: 8px;
        padding: 3px 0;
        font-size: @font-size-sm;

        .rank {
          width: 18px;
          height: 18px;
          border-radius: 4px;
          background: var(--el-fill-color-light);
          color: @text-color-secondary;
          text-align: center;
          line-height: 18px;
          font-size: 12px;
          flex-shrink: 0;
        }
        .top-label {
          flex: 1;
          overflow: hidden;
          text-overflow: ellipsis;
          white-space: nowrap;
        }
        .top-count {
          color: @text-color-placeholder;
          font-size: @font-size-xs;
        }
        &.empty { color: @text-color-placeholder; justify-content: center; height: 100%; }
      }
    }
  }

  .lock-panel {
    background: @bg-color;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    padding: 14px 16px;
    margin-bottom: 16px;

    .panel-title {
      font-weight: 500;
      font-size: @font-size-sm;
      margin-bottom: 12px;
    }
  }

  .filter-section {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    padding: 12px;
    margin-bottom: 12px;
    background: @bg-color;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;

    .filter-actions {
      margin-left: auto;
      display: flex;
      gap: 8px;
    }
  }

  .table-wrap {
    flex: 1 1 auto;
    min-height: 320px;
    display: flex;
    flex-direction: column;
    background: @bg-color;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    overflow: hidden;
    position: relative;

    :deep(.el-table) { flex: 1 1 auto; min-height: 0; cursor: pointer; }

    .path-cell {
      display: block;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .pagination-wrap {
      flex-shrink: 0;
      display: flex;
      justify-content: flex-end;
      padding: 12px 16px;
      border-top: 1px solid @border-color-light;
    }
  }

  .mono { font-family: monospace; }
  .wrap { white-space: normal; word-break: break-word; }
  .break { word-break: break-all; }
}

@media (max-width: 1200px) {
  .admin-security .stats-section {
    grid-template-columns: 1fr 1fr;
  }
  .admin-security .stat-cards {
    grid-template-columns: repeat(2, 1fr);
  }
}
</style>