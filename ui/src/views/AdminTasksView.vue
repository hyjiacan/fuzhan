<template>
  <div class="admin-tasks page-container">
    <div class="header-section">
      <div class="header-left">
        <h2>系统作业</h2>
        <p class="description">查看系统任务执行历史和当前状态</p>
      </div>
      <div class="header-right">
        <el-button type="primary" @click="openScheduleModal">
          <el-icon><Setting /></el-icon>
          作业配置
        </el-button>
      </div>
    </div>

    <!-- 活跃任务 -->
    <el-card shadow="never" style="margin-bottom: 16px;">
      <template #header>当前执行中的任务</template>
      <div class="table-v2-wrap">
        <!-- 表内嵌于流式卡片中，使用普通 el-table（支持 max-height）即可满足固定高度滚动，虚拟滚动无法稳定测量高度，故未采用 el-table-v2 -->
        <el-table :data="visibleActiveTasks" size="small" :border="false" :max-height="400">
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
      <div class="table-v2-wrap">
        <el-table :data="historyTasks" size="small" :border="false" :max-height="500">
          <el-table-column label="任务名称" prop="taskName" min-width="200" show-overflow-tooltip />
          <el-table-column label="类型" width="110">
            <template #default="{ row }">
              <el-tag size="small" :type="taskTypeTag(row).type">{{ taskTypeTag(row).label }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="100">
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

    <el-dialog v-model="showScheduleModal" title="作业调度配置" width="620px" :close-on-click-modal="false">
      <el-form v-loading="scheduleLoading" label-width="140px">
        <el-form-item label="扫描启动延迟">
          <div class="inline-number">
            <el-input-number v-model="scheduleDraft.scanStartDelaySeconds" :min="1" :max="3600" />
            <span>秒</span>
          </div>
          <div class="field-hint">服务启动后延迟执行首次全量扫描，默认 10 秒</div>
        </el-form-item>
        <el-form-item label="定时扫描间隔">
          <cron-editor ref="scanCronEditorRef" v-model="scheduleDraft.scanCronExpression" />
          <div class="field-hint">控制定时全量扫描，默认 <code>0 1 * * *</code>（每天 01:00）</div>
        </el-form-item>
        <el-form-item label="检索索引对齐间隔">
          <cron-editor ref="reconcileCronEditorRef" v-model="scheduleDraft.searchReconcileCronExpression" />
          <div class="field-hint">定时校正检索索引与文件表的对齐，默认 <code>0 5 * * *</code>（每天 05:00）</div>
        </el-form-item>
      </el-form>

      <el-divider content-position="left">其它系统作业</el-divider>
      <el-table :data="otherSystemJobs" size="small" class="job-table">
        <el-table-column prop="name" label="作业" min-width="130" />
        <el-table-column prop="schedule" label="调度" min-width="180" />
        <el-table-column prop="remark" label="说明" min-width="150" />
      </el-table>
      <div class="field-hint job-hint">
        以上作业按固定周期或事件触发，不支持在界面调整；此处仅用于展示系统作业全貌。
      </div>
      <template #footer>
        <el-space>
          <el-button @click="showScheduleModal = false">取消</el-button>
          <el-button type="primary" :loading="scheduleSaving" @click="saveSchedule">保存</el-button>
        </el-space>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { AdminApi, ConfigApi } from '@/api'
import { TimeUtils } from '@/utils'
import { Loading, Setting } from '@element-plus/icons-vue'
import CronEditor from '@/components/common/CronEditor.vue'

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

// 活跃任务渲染上限：避免并发/堆积任务过多时一次性渲染大量行（表格本身固定高度滚动）。
const ACTIVE_SLICE = 100
const visibleActiveTasks = computed(() => activeTasks.value.slice(0, ACTIVE_SLICE))

const typeOptions = [
  { label: '扫描', value: 'scan' },
  { label: '哈希计算', value: 'hash' },
  { label: '一致性检查', value: 'consistency_check' },
  { label: '清理任务', value: 'cleanup' },
  { label: 'URL 上传', value: 'url_download' }
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
  url_download: { label: 'URL 上传', type: 'primary' },
  search_reconcile: { label: '检索索引对齐', type: 'info' }
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

// 作业调度配置：从系统配置读取定时扫描 / 检索索引对齐的 cron 与启动延迟
const showScheduleModal = ref(false)
const scheduleLoading = ref(false)
const scheduleSaving = ref(false)
const scheduleLoaded = ref(false)
const schedule = ref({
  scanStartDelaySeconds: 10,
  scanCronExpression: '0 1 * * *',
  searchReconcileCronExpression: '0 5 * * *'
})
const scheduleDraft = ref({ ...schedule.value })
const scanCronEditorRef = ref(null)
const reconcileCronEditorRef = ref(null)

// 其它系统作业：调度由后端固定周期或事件触发，界面不提供调整入口，仅展示全貌
const otherSystemJobs = [
  { name: '临时存储清理', schedule: '启动时 + 每 1 小时', remark: '清理过期上传会话、分片与临时文件' },
  { name: '一致性检查', schedule: '每 24 小时', remark: '首轮在启动 24 小时后，扫描进行中跳过' },
  { name: '实时索引监听', schedule: '常驻监听', remark: '文件变动增量写入索引' },
  { name: '资源监控采集', schedule: '采样 5 秒 / 采集 60 秒', remark: '采样间隔与应用配置有关' },
  { name: '存储使用率告警', schedule: '每 1 分钟', remark: '达到阈值时发送通知' },
  { name: 'LDAP 目录同步', schedule: '启动时 + 同步间隔', remark: '间隔取 LDAP 配置，未启用则跳过' },
  { name: '回收站/记录落库', schedule: '事件驱动', remark: '请求产生的操作记录与下载计数' }
]

const loadSchedule = async () => {
  scheduleLoading.value = true
  try {
    const res = await ConfigApi.get()
    if (res.success && res.data) {
      const idx = res.data.index || {}
      schedule.value = {
        scanStartDelaySeconds: idx.scanStartDelaySeconds || 10,
        scanCronExpression: idx.scanCronExpression || '0 1 * * *',
        searchReconcileCronExpression: idx.searchReconcileCronExpression || '0 5 * * *'
      }
      scheduleLoaded.value = true
    }
  } catch (e) {
    console.error('加载作业配置失败', e)
  } finally {
    scheduleLoading.value = false
  }
}

const openScheduleModal = async () => {
  if (!scheduleLoaded.value) await loadSchedule()
  scheduleDraft.value = { ...schedule.value }
  showScheduleModal.value = true
}

const saveSchedule = async () => {
  const draft = scheduleDraft.value
  const scanValid = scanCronEditorRef.value
    ? scanCronEditorRef.value.validate()
    : /^(\S+\s+){4}\S+$/.test((draft.scanCronExpression || '').trim())
  const reconcileValid = reconcileCronEditorRef.value
    ? reconcileCronEditorRef.value.validate()
    : /^(\S+\s+){4}\S+$/.test((draft.searchReconcileCronExpression || '').trim())
  if (!scanValid || !reconcileValid) {
    ElMessage.error('请填写有效的 cron 表达式')
    return
  }

  scheduleSaving.value = true
  try {
    const res = await ConfigApi.save({
      index: {
        scanStartDelaySeconds: draft.scanStartDelaySeconds,
        scanCronExpression: draft.scanCronExpression.trim(),
        searchReconcileCronExpression: draft.searchReconcileCronExpression.trim()
      }
    })
    if (res.success) {
      schedule.value = { ...draft }
      showScheduleModal.value = false
      ElMessage.success('作业配置已保存')
    } else {
      ElMessage.error(res.message || '保存失败')
    }
  } catch (e) {
    console.error('保存作业配置失败', e)
    ElMessage.error('保存失败')
  } finally {
    scheduleSaving.value = false
  }
}

onMounted(() => {
  loadActiveTasks()
  loadHistory()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-tasks {
  .history-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
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

  .job-table {
    margin-bottom: 4px;
  }

  .job-hint {
    margin-top: 8px;
  }
}
</style>
