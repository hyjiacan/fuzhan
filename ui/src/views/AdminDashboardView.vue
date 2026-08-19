<template>
  <div class="admin-dashboard">
    <div class="header-section">
      <h2>系统看板</h2>
      <p class="description">系统资源和文件统计概览</p>
    </div>

    <!-- 统计卡片 -->
        <n-grid :cols="6" :x-gap="16" :y-gap="16" class="stat-grid">
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="StorageIcon" size="32" />
                <div class="stat-info">
                  <div class="stat-value">{{ formatSize(storageStats.usedSpace) }}</div>
                  <div class="stat-label">已用存储</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="FileIcon" size="32" />
                <div class="stat-info">
                  <div class="stat-value">{{ indexStats.totalFiles.toLocaleString('zh-CN') }}</div>
                  <div class="stat-label">文件总数</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="FileIcon" size="32" />
                <div class="stat-info">
                  <div class="stat-value">{{ formatSize(indexStats.totalSize) }}</div>
                  <div class="stat-label">总大小</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="DuplicateIcon" size="32" />
                <div class="stat-info">
                  <div class="stat-value">{{ indexStats.duplicateGroups }}</div>
                  <div class="stat-label">重复文件组</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="AccessIcon" size="32" />
                <div class="stat-info">
                  <div class="stat-value">{{ stats.activeUsers }}</div>
                  <div class="stat-label">活跃用户</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="GlobeIcon" size="32" />
                <div class="stat-info">
                  <div class="stat-value">{{ openApiStats.todayCalls }}</div>
                  <div class="stat-label">API 调用(今日)</div>
                </div>
              </div>
            </n-card>
          </n-gi>
        </n-grid>

        <!-- 系统信息：运行状态 + 版本 -->
        <div class="system-info-row" v-if="systemInfoLoaded">
          <n-card title="系统信息" class="chart-card system-info-card">
            <n-descriptions label-placement="left" :column="4" size="small">
              <n-descriptions-item label="服务状态">
                <n-tag :type="healthStatus === 'healthy' ? 'success' : 'error'" size="small">
                  {{ healthStatus === 'healthy' ? '正常运行' : '异常' }}
                </n-tag>
              </n-descriptions-item>
              <n-descriptions-item label="应用名称">
                {{ systemConfig.appName || '-' }}
              </n-descriptions-item>
              <n-descriptions-item label="版本号">
                {{ systemConfig.version || '-' }}
              </n-descriptions-item>
              <n-descriptions-item label="组件状态">
                <n-space size="small">
                  <n-tag v-for="check in healthChecks" :key="check.name" :type="check.status === 'ok' ? 'success' : 'error'" size="small">
                    {{ check.label }}: {{ check.status === 'ok' ? '正常' : '异常' }}
                  </n-tag>
                </n-space>
              </n-descriptions-item>
            </n-descriptions>
          </n-card>
        </div>

        <!-- 下方：左侧存储区域 / 右侧文件区域 -->
        <div class="dashboard-bottom">
          <div class="bottom-left">
            <n-card title="存储使用情况" class="chart-card">
              <div class="storage-list">
                <div class="storage-header">
                  <span>分区</span>
                  <span>配额</span>
                  <span>已用</span>
                  <span>剩余</span>
                  <span>使用率</span>
                </div>
                <div v-for="root in storageStats.roots" :key="root.name" class="storage-item">
                  <div class="storage-progress-bar" :style="{ width: getUsagePercent(root) + '%' }"></div>
                  <span class="storage-name">
                    <n-icon :component="FolderIcon" size="16" />
                    {{ root.name }}
                  </span>
                  <span class="storage-value">{{ root.quota > 0 ? formatSize(root.quota) : '无限制' }}</span>
                  <span class="storage-value">{{ formatSize(root.used) }}</span>
                  <span class="storage-value">{{ root.quota > 0 ? formatSize(root.free) : formatSize(root.total - root.used) }}</span>
                  <span class="storage-value" :class="{ 'usage-high': getUsagePercent(root) > 90 }">{{ getUsagePercent(root) }}%</span>
                </div>
              </div>
            </n-card>
            <n-card title="临时文件配额" class="chart-card">
              <div v-if="Object.keys(storageStats.temp?.usedByIP || {}).length > 0" class="quota-list">
                <div class="quota-header">
                  <span>IP地址</span>
                  <span>使用量</span>
                  <span>配额</span>
                </div>
                <div v-for="(used, ip) in storageStats.temp?.usedByIP" :key="ip" class="quota-item">
                  <span class="quota-ip">{{ ip }}</span>
                  <span class="quota-value">{{ formatSize(used) }}</span>
                  <span class="quota-value">{{ formatSize(storageStats.temp?.quotaPerIP || 0) }}</span>
                </div>
              </div>
              <n-empty v-else description="暂无临时文件" />
            </n-card>
            <n-card title="私有存储配额" class="chart-card">
              <div v-if="Object.keys(storageStats.private?.usedByUser || {}).length > 0" class="quota-list">
                <div class="quota-header cols-2">
                  <span>用户ID</span>
                  <span>使用量</span>
                </div>
                <div v-for="(used, userId) in storageStats.private?.usedByUser" :key="userId" class="quota-item cols-2">
                  <span class="quota-ip">{{ userId.substring(0, 8) }}...</span>
                  <span class="quota-value">{{ formatSize(used) }}</span>
                </div>
              </div>
              <n-empty v-else description="暂无私有文件" />
            </n-card>
          </div>
          <div class="bottom-right">
            <n-card title="热门搜索词" class="chart-card">
              <div v-if="keywords.length > 0" class="keyword-badges">
                <n-badge v-for="kw in keywords" :key="kw.word" :value="kw.count" :max="999" type="warning" class="keyword-badge">
                  <n-tag>{{ kw.word }}</n-tag>
                </n-badge>
              </div>
              <n-empty v-else description="暂无搜索记录" />
            </n-card>
            <n-card title="最近上传" class="chart-card">
              <div v-if="rankings.recentUploads.length > 0" class="rank-list">
                <div v-for="(item, index) in rankings.recentUploads" :key="'u' + index" class="rank-item">
                  <n-icon :component="UploadIcon" size="16" class="rank-icon upload" />
                  <span class="rank-filename">{{ item.filename }}</span>
                  <span class="rank-time">{{ formatTime(item.time) }}</span>
                </div>
              </div>
              <n-empty v-else description="暂无上传记录" />
            </n-card>
            <n-card title="最近下载" class="chart-card">
              <div v-if="rankings.recentDownloads.length > 0" class="rank-list">
                <div v-for="(item, index) in rankings.recentDownloads" :key="'d' + index" class="rank-item">
                  <n-icon :component="DownloadIcon" size="16" class="rank-icon download" />
                  <span class="rank-filename">{{ item.filename }}</span>
                  <span class="rank-time">{{ formatTime(item.time) }}</span>
                </div>
              </div>
              <n-empty v-else description="暂无下载记录" />
            </n-card>
          </div>
        </div>
      </div>
</template>

<script setup>
import { ref, reactive, h, onMounted } from 'vue'
import {
  NGrid, NGi, NCard, NIcon, NEmpty, NButton, NBadge, NTag,
  NSpace, NDescriptions, NDescriptionsItem, useMessage
} from 'naive-ui'
import { NumberUtils } from '@/utils'
import { AdminApi, MonitorApi, SystemApi, IndexApi } from '@/api'
import store from '@/store'

const message = useMessage()

// ============ 概览部分 ============

// 统计数据
const stats = ref({
  totalUsers: 0,
  activeUsers: 0,
  onlineIPs: 0
})

// 存储统计
const storageStats = ref({
  totalSpace: 0,
  usedSpace: 0,
  freeSpace: 0,
  roots: [],
  temp: {
    quotaPerIP: 0,
    usedByIP: {}
  },
  private: {
    usedByUser: {}
  }
})

// 热门搜索词
const keywords = ref([])

// 排行榜
const rankings = ref({
  recentUploads: [],
  recentDownloads: []
})

// 索引统计
const indexStats = ref({
  totalFiles: 0,
  totalSize: 0,
  duplicateGroups: 0
})

// OpenAPI 统计
const openApiStats = ref({
  todayCalls: 0,
  weekCalls: 0,
  totalCalls: 0
})

// ============ 系统信息 ============
const systemInfoLoaded = ref(false)
const healthStatus = ref('healthy')
const healthChecks = ref([])
const systemConfig = ref({
  appName: '',
  version: '1.0.0'
})

// ============ Icons ============
const UserIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z' })
])
const FileIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm4 18H6V4h7v5h5v11z' })
])
const StorageIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M2 20h20v-4H2v4zm2-3h2v2H4v-2zM2 4v4h20V4H2zm4 3H4V5h2v2zm-4 7h20v-4H2v4zm2-3h2v2H4v-2z' })
])
const AccessIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-2 15l-5-5 1.41-1.41L10 14.17l7.59-7.59L19 8l-9 9z' })
])
const GlobeIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z' })
])
const DuplicateIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z' })
])
const FolderIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z' })
])
const DownloadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M19 9h-4V3H9v6H5l7 7 7-7zM5 18v2h14v-2H5z' })
])
const UploadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M9 16h6v-6h4l-7-7-7 7h4zm-4 2h14v2H5z' })
])

// ============ 方法 ============

const formatSize = (bytes) => NumberUtils.formatFileSize(bytes || 0)

const getUsagePercent = (root) => {
  if (root.quota > 0) {
    return Math.min(100, Math.round((root.used / root.quota) * 100))
  }
  if (root.total > 0) {
    return Math.min(100, Math.round((root.used / root.total) * 100))
  }
  return 0
}

const formatTime = (time) => {
  if (!time) return ''
  const d = new Date(time)
  return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

// ============ 概览数据加载 ============

const loadStats = async () => {
  try {
    const [usersData, storageData, accessData, keywordsData, rankingsData, indexStatsData, openApiData] = await Promise.all([
      AdminApi.getUsers().catch((e) => { console.error('获取用户数据失败:', e); return { success: false } }),
      MonitorApi.getStorage().catch((e) => { console.error('获取存储数据失败:', e); return { success: false } }),
      MonitorApi.getAccess().catch((e) => { console.error('获取访问数据失败:', e); return { success: false } }),
      MonitorApi.getKeywords(30).catch((e) => { console.error('获取关键词数据失败:', e); return { success: false } }),
      MonitorApi.getRankings(30).catch((e) => { console.error('获取排行数据失败:', e); return { success: false } }),
      IndexApi.getStats().catch((e) => { console.error('获取索引统计失败:', e); return { success: false } }),
      SystemApi.getOpenAPIStats().catch((e) => { console.error('获取OpenAPI统计失败:', e); return { success: false } })
    ])

    const failedCount = [usersData, storageData, accessData, keywordsData, rankingsData, indexStatsData]
      .filter(d => !d.success).length
    if (failedCount > 0) {
      message.warning(`部分统计数据加载失败 (${failedCount}/6)，请刷新重试`)
    }

    if (usersData.success) {
      stats.value.totalUsers = (usersData.data || []).length
    }

    if (storageData.success) {
      storageStats.value = storageData.data || {
        totalSpace: 0,
        usedSpace: 0,
        roots: [],
        temp: { quotaPerIP: 0, usedByIP: {} },
        private: { usedByUser: {} }
      }
    }

    if (accessData.success) {
      stats.value.activeUsers = accessData.data?.activeUsers || 0
      stats.value.onlineIPs = accessData.data?.onlineIPs || 0
    }

    if (keywordsData.success) {
      keywords.value = keywordsData.data || []
    }

    if (rankingsData.success) {
      const seen = new Set()
      const dedup = (records) => (records || []).filter(r => {
        const key = r.filename || ''
        if (seen.has(key)) return false
        seen.add(key)
        return true
      })
      rankings.value.recentUploads = dedup(rankingsData.data?.recentUploads)
      rankings.value.recentDownloads = dedup(rankingsData.data?.recentDownloads)
    }

    if (indexStatsData.success) {
      indexStats.value = indexStatsData.data || { totalFiles: 0, totalSize: 0, duplicateGroups: 0 }
    }

    if (openApiData.success) {
      openApiStats.value = openApiData.data || { todayCalls: 0, weekCalls: 0, totalCalls: 0 }
    }
  } catch (e) {
    console.error('获取统计数据失败', e)
  }
}

// ============ 系统信息加载 ============

const loadSystemInfo = async () => {
  try {
    const [healthRes] = await Promise.all([
      SystemApi.health().catch((e) => { console.error('获取系统信息失败:', e); return { success: false } })
    ])
    if (healthRes.success && healthRes.data) {
      healthStatus.value = healthRes.data.status || 'healthy'
      const checks = healthRes.data.checks || {}
      healthChecks.value = Object.entries(checks).map(([name, info]) => ({
        name,
        label: getCheckLabel(name),
        status: info.status || 'unknown',
        message: info.message || ''
      }))
    }
    systemConfig.value = {
      appName: store.state.config.appName || '',
      version: store.state.config.version || '1.0.0'
    }
    systemInfoLoaded.value = true
  } catch (e) {
    console.error('加载系统信息失败', e)
  }
}

const getCheckLabel = (name) => {
  const labels = { database: '数据库', storage: '存储', cache: '缓存', network: '网络' }
  return labels[name] || name
}

// ============ 僵尸文件数据加载 ============

const loadZombieSessions = async () => {
  zombieLoading.value = true
  try {
    const data = await AdminApi.getSessions()
    if (data.success) {
      const sessionList = (data.data && data.data.sessions) || []
      zombieSessions.value = sessionList
      zombieStats.totalSessions = sessionList.length
      zombieStats.zombieSessions = sessionList.filter(s =>
        s.status === 'expired' || s.status === 'pending' || s.status === 'in_progress'
      ).length
      zombieStats.totalSize = sessionList.reduce((sum, s) => sum + (s.uploadedSize || 0), 0)
    }
  } catch (e) {
    console.error('加载会话列表失败', e)
  } finally {
    zombieLoading.value = false
  }
}

const zombieHandleSelectionChange = (keys) => {
  zombieSelectedRowKeys.value = keys
}

const zombieCleanSelected = () => {
  if (zombieSelectedRowKeys.value.length === 0) {
    message.info('请先选择要清理的会话')
    return
  }
  const selected = zombieSessions.value.filter(s => zombieSelectedRowKeys.value.includes(s.id))
  zombieSelectedSessions.value = selected
  zombieConfirmVisible.value = true
}

const zombieCleanSession = (session) => {
  zombieSelectedSessions.value = [session]
  zombieConfirmVisible.value = true
}

onMounted(() => {
  loadStats()
  loadSystemInfo()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-dashboard {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 16px;

    h2 {
      margin: 0 0 8px 0;
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .description {
      margin: 0;
      color: @text-color-secondary;
      font-size: @font-size-base;
    }
  }

  .stat-grid .n-gi {
    display: flex;
  }

  .stat-grid {
    margin-bottom: 16px;
  }

  .dashboard-bottom {
    display: flex;
    gap: 16px;
    margin-bottom: 16px;
  }

  .bottom-left {
    flex: 5;
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
  }

  .bottom-right {
    flex: 4;
    display: flex;
    flex-direction: column;
    gap: 16px;
    min-width: 0;
  }

  .stat-card,
  .chart-card {
    transition: transform @transition-smooth, box-shadow @transition-smooth;
    flex: 1;

    &:hover {
      transform: translateY(-2px);
      box-shadow: @card-hover-shadow;
    }

    .n-card__content {
      padding: 16px;
    }
  }

  .stat-content {
    display: flex;
    align-items: center;
    gap: 16px;

    .n-icon {
      color: @primary-color;
      transition: transform @transition-bounce;
    }

    &:hover .n-icon {
      transform: scale(1.1);
    }
  }

  .stat-info {
    .stat-value {
      font-size: @font-size-xxl;
      font-weight: 600;
      color: @text-color;
    }

    .stat-label {
      font-size: @font-size-base;
      color: @text-color-secondary;
    }
  }

  .storage-list {
    border-top: 1px solid @border-color-light;
  }

  .storage-header,
  .storage-item {
    display: grid;
    grid-template-columns: 2fr 1fr 1fr 1fr 1fr;
    gap: 8px;
    padding: 8px 12px;
    font-size: @font-size-sm;
    text-align: right;
  }

  .storage-header {
    font-weight: 500;
    color: @text-color-secondary;
    border-bottom: 1px solid @border-color-light;

    & > span:first-child {
      text-align: left;
    }
  }

  .storage-item {
    position: relative;
    overflow: hidden;
    border-radius: @border-radius-sm;
    transition: background-color @transition-fast;

    &:hover {
      background-color: @bg-color-secondary;
    }

    .storage-progress-bar {
      position: absolute;
      top: 0;
      left: 0;
      height: 100%;
      background: fade(@primary-color, 10%);
      pointer-events: none;
      transition: width @transition-smooth;
      z-index: 0;
    }

    & > span {
      position: relative;
      z-index: 1;
    }

    .storage-name {
      display: flex;
      align-items: center;
      gap: 4px;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      text-align: left;
    }

    .storage-value {
      color: @text-color-secondary;
    }

    .usage-high {
      color: @error-color;
      font-weight: 600;
    }
  }

  .quota-header.cols-2,
  .quota-item.cols-2 {
    grid-template-columns: 2fr 1fr;
  }

  .quota-list {
    border-top: 1px solid @border-color-light;
  }

  .quota-header,
  .quota-item {
    display: grid;
    grid-template-columns: 2fr 1fr 1fr;
    gap: 8px;
    padding: 8px 12px;
    font-size: @font-size-sm;
    text-align: right;
  }

  .quota-header {
    font-weight: 500;
    color: @text-color-secondary;
    border-bottom: 1px solid @border-color-light;

    & > span:first-child {
      text-align: left;
    }
  }

  .quota-item {
    &:hover {
      background-color: @bg-color-secondary;
    }

    .quota-ip {
      font-family: monospace;
      font-size: @font-size-xs;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      text-align: left;
    }

    .quota-value {
      color: @text-color-secondary;
    }
  }

  .zombie-section {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
  }

  .zombie-count {
    display: flex;
    align-items: center;
    gap: 16px;

    .warning-icon {
      color: @warning-color;
    }

    .zombie-info {
      text-align: left;
    }

    .zombie-number {
      font-size: 36px;
      font-weight: 700;
      color: @warning-color;
    }

    .zombie-label {
      font-size: @font-size-sm;
      color: @text-color-secondary;
    }
  }

  .rank-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .rank-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    border-bottom: 1px solid @border-color-light;
    transition: transform @transition-smooth, background-color @transition-fast;

    &:last-child {
      border-bottom: none;
    }

    &:hover {
      transform: translateX(4px);
      background-color: @bg-color-secondary;
      padding-left: 8px;
      margin-left: -8px;
      margin-right: -8px;
      padding-right: 8px;
    }

    .rank-icon {
      &.download {
        color: @info-color;
      }
      &.upload {
        color: @success-color;
      }
    }

    .rank-filename {
      flex: 1;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
      font-size: @font-size-sm;
    }

    .rank-time {
      color: @text-color-placeholder;
      font-size: @font-size-xs;
    }
  }
}

/* 系统信息栏 */
.system-info-row {
  margin-bottom: 16px;
}

.system-info-card {
  .n-descriptions {
    padding: 4px 0;
  }
}

/* 响应式：看板 */
@media @tablet {
  .admin-dashboard {
    padding: 12px;

    .dashboard-bottom {
      flex-direction: column;
    }
  }

  .storage-header,
  .storage-item {
    grid-template-columns: 2fr 1fr 1fr !important;

    & > span:nth-child(3),
    & > span:nth-child(4) {
      display: none;
    }
  }
}

@media @mobile {
  .admin-dashboard {
    padding: 8px;
  }

  .storage-header,
  .storage-item {
    grid-template-columns: 2fr 1fr !important;

    & > span:nth-child(3),
    & > span:nth-child(4) {
      display: none;
    }
  }

  .quota-header,
  .quota-item {
    grid-template-columns: 1fr 1fr !important;

    & > span:nth-child(3) {
      display: none;
    }
  }

  .stat-content {
    flex-direction: column;
    gap: 8px;
    text-align: center;

    .stat-info {
      .stat-value {
        font-size: @font-size-xl;
      }
    }
  }
}
</style>
