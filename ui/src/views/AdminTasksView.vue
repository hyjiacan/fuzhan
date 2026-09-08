<template>
  <div class="admin-tasks">
    <div class="header-section">
      <h2>任务管理</h2>
      <p class="description">查看系统任务执行历史和当前状态</p>
    </div>

    <!-- 活跃任务 -->
    <el-card shadow="never" style="margin-bottom: 16px;">
      <template #header>当前执行中的任务</template>
      <div class="tasks-table-wrap">
        <!-- 表内嵌于流式卡片中，使用普通 el-table（支持 max-height）即可满足固定高度滚动，虚拟滚动无法稳定测量高度，故未采用 el-table-v2 -->
        <el-table :data="activeTasks" size="small" :border="false" :max-height="400">
          <el-table-column label="任务名称" prop="taskName" min-width="200" show-overflow-tooltip />
          <el-table-column label="类型" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="taskTypeTag(row).type">{{ taskTypeTag(row).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="statusTag(row).type">{{ statusTag(row).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="触发原因" width="110">
            <template #default="{ row }">{{ triggerLabel(row.trigger) }}</template>
          </el-table-column>
          <el-table-column label="进度" width="220">
            <template #default="{ row }">
              <div style="display: flex; align-items: center; gap: 8px;">
                <el-progress
                  :percentage="Math.round(row.progress || 0)"
                  :stroke-width="16"
                  :text-inside="true"
                  :status="row.status === 'failed' ? 'exception' : undefined"
                  style="flex: 1;"
                />
                <span v-if="row.totalItems > 0" class="progress-count">({{ row.doneItems }}/{{ row.totalItems }})</span>
              </div>
            </template>
          </el-table-column>
          <el-table-column label="开始时间" width="180">
            <template #default="{ row }">{{ row.startedAt ? TimeUtils.formatDateTime(row.startedAt) : '-' }}</template>
          </el-table-column>
          <el-table-column label="已运行" width="100">
            <template #default="{ row }">{{ row.startedAt ? formatDuration(row.startedAt, new Date().toISOString()) : '-' }}</template>
          </el-table-column>
          <el-table-column label="错误信息" width="240" prop="errorMessage">
            <template #default="{ row }">
              <span v-if="row.errorMessage" style="color: #d03050;">{{ row.errorMessage }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column label="明细" prop="details" min-width="220" show-overflow-tooltip>
            <template #default="{ row }">
              <span v-if="row.details">{{ row.details }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="loading" class="table-loading-mask">
          <el-icon class="is-loading" :size="22"><Loading /></el-icon>
        </div>
      </div>
    </el-card>

    <!-- 任务历史 -->
    <el-card shadow="never">
      <template #header>
        <div class="history-header">
          <span>任务历史</span>
          <el-space>
            <el-select
              v-model="filterType"
              style="width: 150px"
              clearable
              placeholder="全部类型"
              size="small"
              @change="onFilterTypeChange"
            >
              <el-option
                v-for="opt in typeOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </el-select>
            <el-button size="small" @click="loadHistory">刷新</el-button>
          </el-space>
        </div>
      </template>
      <div class="tasks-table-wrap">
        <el-table :data="historyTasks" size="small" :border="false" :max-height="500">
          <el-table-column label="任务名称" prop="taskName" min-width="200" show-overflow-tooltip />
          <el-table-column label="类型" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="taskTypeTag(row).type">{{ taskTypeTag(row).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag size="small" :type="statusTag(row).type">{{ statusTag(row).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="触发原因" width="110">
            <template #default="{ row }">{{ triggerLabel(row.trigger) }}</template>
          </el-table-column>
          <el-table-column label="进度" width="180">
            <template #default="{ row }">
              <el-progress
                :percentage="Math.round(row.progress || 0)"
                :stroke-width="16"
                :text-inside="true"
                :status="row.status === 'failed' ? 'exception' : undefined"
              />
            </template>
          </el-table-column>
          <el-table-column label="开始时间" width="180">
            <template #default="{ row }">{{ row.startedAt ? TimeUtils.formatDateTime(row.startedAt) : '-' }}</template>
          </el-table-column>
          <el-table-column label="完成时间" width="180">
            <template #default="{ row }">{{ row.endedAt ? TimeUtils.formatDateTime(row.endedAt) : '-' }}</template>
          </el-table-column>
          <el-table-column label="耗时" width="90">
            <template #default="{ row }">{{ formatDuration(row.startedAt, row.endedAt) }}</template>
          </el-table-column>
          <el-table-column label="错误信息" width="240" prop="errorMessage">
            <template #default="{ row }">
              <span v-if="row.errorMessage" style="color: #d03050;">{{ row.errorMessage }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
          <el-table-column label="明细" prop="details" min-width="220" show-overflow-tooltip>
            <template #default="{ row }">
              <span v-if="row.details">{{ row.details }}</span>
              <span v-else>-</span>
            </template>
          </el-table-column>
        </el-table>
        <div v-if="loadingHistory" class="table-loading-mask">
          <el-icon class="is-loading" :size="22"><Loading /></el-icon>
        </div>
      </div>
      <template #footer>
        <div style="display: flex; justify-content: flex-end;">
          <el-pagination
            v-if="totalPages > 1"
            v-model:current-page="currentPage"
            :total="totalCount"
            :page-size="pageSize"
            layout="prev, pager, next"
            @current-change="onPageChange"
          />
        </div>
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { AdminApi } from '@/api'
import { TimeUtils } from '@/utils'
import { Loading } from '@element-plus/icons-vue'

// 计算耗时（startedAt 到 endedAt 的差值，返回可读字符串）
const formatDuration = (startedAt, endedAt) => {
  if (!startedAt || !endedAt) return '-'
  const start = new Date(startedAt).getTime()
  const end = new Date(endedAt).getTime()
  const diff = Math.max(0, end - start)
  if (diff < 1000) return diff + 'ms'
  if (diff < 60000) return (diff / 1000).toFixed(1) + 's'
  if (diff < 3600000) return Math.floor(diff / 60000) + 'm' + Math.floor((diff % 60000) / 1000) + 's'
  return Math.floor(diff / 3600000) + 'h' + Math.floor((diff % 3600000) / 60000) + 'm'
}

const loading = ref(false)
const loadingHistory = ref(false)
const activeTasks = ref([])
const historyTasks = ref([])
const filterType = ref(null)
const currentPage = ref(1)
const pageSize = ref(20)
const totalCount = ref(0)

const typeOptions = [
  { label: '扫描', value: 'scan' },
  { label: '哈希计算', value: 'hash' },
  { label: '一致性检查', value: 'consistency_check' },
  { label: '清理任务', value: 'cleanup' },
  { label: 'URL 下载', value: 'url_download' }
]

const totalPages = computed(() => Math.ceil(totalCount.value / pageSize.value))

// naive 的标签类型 -> element-plus 的标签类型
const naiveToElTag = (type) => {
  const map = {
    default: 'info',
    error: 'danger',
    info: 'info',
    success: 'success',
    warning: 'warning',
    primary: 'primary'
  }
  return map[type] || 'info'
}

const taskTypeLabels = {
  scan: { label: '扫描', type: 'info' },
  hash: { label: '哈希计算', type: 'warning' },
  consistency_check: { label: '一致性检查', type: 'success' },
  cleanup: { label: '清理任务', type: 'default' },
  url_download: { label: 'URL 下载', type: 'primary' }
}

const taskTypeTag = (row) => {
  const info = taskTypeLabels[row.taskType] || { label: row.taskType, type: 'default' }
  return { label: info.label, type: naiveToElTag(info.type) }
}

const statusTypeMap = {
  pending: { type: 'default', text: '等待中' },
  running: { type: 'info', text: '运行中' },
  completed: { type: 'success', text: '已完成' },
  failed: { type: 'error', text: '失败' },
  cancelled: { type: 'warning', text: '已取消' }
}

const statusTag = (row) => {
  const info = statusTypeMap[row.status] || { type: 'default', text: row.status }
  return { label: info.text, type: naiveToElTag(info.type) }
}

// 触发原因编码 -> 中文标签
const triggerLabels = {
  timer: '定时器',
  startup: '启动',
  manual: '用户手动',
  user: '用户操作',
  auto: '自动',
}

const triggerLabel = (code) => {
  if (!code) return '-'
  return triggerLabels[code] || code
}

const loadActiveTasks = async () => {
  loading.value = true
  try {
    const data = await AdminApi.getTasks()
    if (data.success) {
      activeTasks.value = data.data?.active || []
    }
  } catch (e) {
    console.error('加载活跃任务失败', e)
  } finally {
    loading.value = false
  }
}

const loadHistory = async () => {
  loadingHistory.value = true
  try {
    const data = await AdminApi.getTaskHistory(filterType.value, currentPage.value, pageSize.value)
    if (data.success) {
      historyTasks.value = data.data?.items || []
      totalCount.value = data.data?.total || 0
    }
  } catch (e) {
    console.error('加载任务历史失败', e)
  } finally {
    loadingHistory.value = false
  }
}

const onPageChange = (page) => {
  currentPage.value = page
  loadHistory()
}

// 切换类型过滤时回到第一页，避免停留在无数据的旧页码
const onFilterTypeChange = () => {
  currentPage.value = 1
  loadHistory()
}

onMounted(() => {
  loadActiveTasks()
  loadHistory()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-tasks {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 24px;

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

  .history-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .tasks-table-wrap {
    position: relative;
    width: 100%;
  }

  .table-loading-mask {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(255, 255, 255, 0.6);
    z-index: 5;
  }

  .progress-count {
    color: @text-color-placeholder;
    font-size: @font-size-xs;
    white-space: nowrap;
  }

  .empty-hint {
    display: flex;
    justify-content: center;
    padding: 40px 0;
  }
}

@media @tablet {
  .admin-tasks {
    padding: 12px;
  }
}

@media @mobile {
  .admin-tasks {
    padding: 8px;
  }
}
</style>