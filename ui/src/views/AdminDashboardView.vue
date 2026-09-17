<template>
  <div class="admin-dashboard">
    <div class="header-section">
      <h2>系统看板</h2>
      <p class="description">系统资源和文件统计概览</p>
    </div>

    <!-- 统计卡片 -->
        <el-row :gutter="16" class="stat-grid">
          <el-col :xs="24" :sm="12" :md="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content stat-storage">
                <el-icon :size="32"><component :is="StorageIcon" /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ formatSize(storageStats.usedSpace) }}</div>
                  <div class="stat-label">已用存储</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content stat-files">
                <el-icon :size="32"><component :is="FileIcon" /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ indexStats.totalFiles.toLocaleString('zh-CN') }}</div>
                  <div class="stat-label">文件总数</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content stat-dups">
                <el-icon :size="32"><component :is="DuplicateIcon" /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ indexStats.duplicateGroups }}</div>
                  <div class="stat-label">重复文件组</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :xs="24" :sm="12" :md="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content stat-users">
                <el-icon :size="32"><component :is="UserIcon" /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ stats.activeUsers }}</div>
                  <div class="stat-label">活跃用户</div>
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <!-- 下方：左侧存储区域 / 右侧文件区域 -->
        <div class="dashboard-bottom">
          <div class="bottom-left">
            <el-card class="chart-card" shadow="never">
              <template #header>存储使用情况</template>
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
                    <el-icon :size="16"><component :is="FolderIcon" /></el-icon>
                    {{ root.name }}
                  </span>
                  <span class="storage-value">{{ root.quota > 0 ? formatSize(root.quota) : '无限制' }}</span>
                  <span class="storage-value">{{ formatSize(root.used) }}</span>
                  <span class="storage-value">{{ root.quota > 0 ? formatSize(root.free) : formatSize(root.total - root.used) }}</span>
                  <span class="storage-value" :class="{ 'usage-high': getUsagePercent(root) > 90 }">{{ getUsagePercent(root) }}%</span>
                </div>
              </div>
            </el-card>
            <el-card class="chart-card" shadow="never">
              <template #header>临时文件配额</template>
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
              <el-empty v-else description="暂无临时文件" />
            </el-card>
            <el-card class="chart-card" shadow="never">
              <template #header>私有存储配额</template>
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
              <el-empty v-else description="暂无私有文件" />
            </el-card>
          </div>
          <div class="bottom-right">
            <el-card class="chart-card" shadow="never">
              <template #header>
                <div class="card-header">
                  <span>热门搜索词</span>
                  <el-link type="primary" :underline="false" @click="goToRecords('search')">查看全部 &rsaquo;</el-link>
                </div>
              </template>
              <div v-if="keywords.length > 0" class="keyword-badges">
                <el-badge v-for="kw in keywords" :key="kw.word" :value="kw.count" :max="999" type="warning" class="keyword-badge">
                  <el-tag>{{ kw.word }}</el-tag>
                </el-badge>
              </div>
              <el-empty v-else description="暂无搜索记录" />
            </el-card>
            <el-card class="chart-card" shadow="never">
              <template #header>
                <div class="card-header">
                  <span>最近上传</span>
                  <el-link type="primary" :underline="false" @click="goToRecords('upload')">查看全部 &rsaquo;</el-link>
                </div>
              </template>
              <div v-if="rankings.recentUploads.length > 0" class="rank-list">
                <div v-for="(item, index) in rankings.recentUploads" :key="'u' + index" class="rank-item">
                  <el-icon :size="16" class="rank-icon upload"><component :is="UploadIcon" /></el-icon>
                  <span class="rank-filename">{{ item.filename }}</span>
                  <span class="rank-time">{{ formatTime(item.time) }}</span>
                </div>
              </div>
              <el-empty v-else description="暂无上传记录" />
            </el-card>
            <el-card class="chart-card" shadow="never">
              <template #header>
                <div class="card-header">
                  <span>最近下载</span>
                  <el-link type="primary" :underline="false" @click="goToRecords('download')">查看全部 &rsaquo;</el-link>
                </div>
              </template>
              <div v-if="rankings.recentDownloads.length > 0" class="rank-list">
                <div v-for="(item, index) in rankings.recentDownloads" :key="'d' + index" class="rank-item">
                  <el-icon :size="16" class="rank-icon download"><component :is="DownloadIcon" /></el-icon>
                  <span class="rank-filename">{{ item.filename }}</span>
                  <span class="rank-time">{{ formatTime(item.time) }}</span>
                </div>
              </div>
              <el-empty v-else description="暂无下载记录" />
            </el-card>
          </div>
        </div>
      </div>
</template>

<script setup>
import { ref, reactive, h, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { NumberUtils, TimeUtils } from '@/utils'
import { AdminApi, MonitorApi, IndexApi } from '@/api'

const router = useRouter()

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

const formatTime = (time) => (time ? TimeUtils.formatDateTime(time) : '')

// 跳转到记录管理页对应 tab
const goToRecords = (tab) => {
  router.push({ path: '/admin/records', query: { tab } })
}

// ============ 概览数据加载 ============

const loadStats = async () => {
  try {
    const [usersData, storageData, accessData, keywordsData, rankingsData, indexStatsData] = await Promise.all([
      AdminApi.getUsers().catch((e) => { console.error('获取用户数据失败:', e); return { success: false } }),
      MonitorApi.getStorage().catch((e) => { console.error('获取存储数据失败:', e); return { success: false } }),
      MonitorApi.getAccess().catch((e) => { console.error('获取访问数据失败:', e); return { success: false } }),
      MonitorApi.getKeywords(20).catch((e) => { console.error('获取关键词数据失败:', e); return { success: false } }),
      MonitorApi.getRankings(30).catch((e) => { console.error('获取排行数据失败:', e); return { success: false } }),
      IndexApi.getStats().catch((e) => { console.error('获取索引统计失败:', e); return { success: false } })
    ])

    const failedCount = [usersData, storageData, accessData, keywordsData, rankingsData, indexStatsData]
      .filter(d => !d.success).length
    if (failedCount > 0) {
      ElMessage.warning(`部分统计数据加载失败 (${failedCount}/6)，请刷新重试`)
    }

    if (usersData.success) {
      stats.value.totalUsers = usersData.data?.total ?? (usersData.data || []).length ?? 0
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
      keywords.value = (keywordsData.data || []).slice(0, 20)
    }

    if (rankingsData.success) {
      const seen = new Set()
      const dedup = (records) => (records || []).filter(r => {
        const key = r.filename || ''
        if (seen.has(key)) return false
        seen.add(key)
        return true
      })
      rankings.value.recentUploads = dedup(rankingsData.data?.recentUploads).slice(0, 10)
      rankings.value.recentDownloads = dedup(rankingsData.data?.recentDownloads).slice(0, 10)
    }

    if (indexStatsData.success) {
      indexStats.value = indexStatsData.data || { totalFiles: 0, totalSize: 0, duplicateGroups: 0 }
    }
  } catch (e) {
    console.error('获取统计数据失败', e)
  }
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
    ElMessage.info('请先选择要清理的会话')
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
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-dashboard {
  padding: @container-padding;

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

  .stat-grid :deep(.el-col) {
    display: flex;
  }

  .card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;

    &:deep(.el-link) {
      font-size: 12px;
    }
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

    :deep(.el-card__body) {
      padding: 16px;
    }
  }

  .stat-content {
    display: flex;
    align-items: center;
    gap: 16px;

    .el-icon {
      transition: transform @transition-bounce;
    }

    &:hover .el-icon {
      transform: scale(1.1);
    }

    // 卡片类型语义色：与下方对应状态色统一
    &.stat-storage .el-icon {
      color: @primary-accent;
    }

    &.stat-files .el-icon {
      color: @info-color;
    }

    &.stat-dups .el-icon {
      color: @warning-color;
    }

    &.stat-users .el-icon {
      color: @success-color;
    }
  }

  .stat-info {
    .stat-value {
      font-size: @font-size-display;
      font-weight: 600;
      color: @text-color;
      font-variant-numeric: tabular-nums;
      letter-spacing: -0.5px;
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
      background: fade(@primary-accent, 12%);
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