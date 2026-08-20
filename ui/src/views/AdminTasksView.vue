<template>
  <div class="admin-tasks">
    <div class="header-section">
      <h2>任务管理</h2>
      <p class="description">查看系统任务执行历史和当前状态</p>
    </div>

    <!-- 活跃任务 -->
    <n-card title="当前执行中的任务" style="margin-bottom: 16px;">
      <n-data-table
        :columns="activeColumns"
        :data="activeTasks"
        :loading="loading"
        :bordered="false"
        size="small"
        :max-height="400"
      />
    </n-card>

    <!-- 任务历史 -->
    <n-card title="任务历史">
      <template #header-extra>
        <n-space>
          <n-select
            v-model:value="filterType"
            :options="typeOptions"
            style="width: 150px"
            clearable
            placeholder="全部类型"
            size="small"
            @update:value="loadHistory"
          />
          <n-button size="small" @click="loadHistory">刷新</n-button>
        </n-space>
      </template>
      <n-data-table
        :columns="historyColumns"
        :data="historyTasks"
        :loading="loadingHistory"
        :bordered="false"
        size="small"
        :max-height="500"
      />
      <template #footer>
        <n-space justify="end">
          <n-pagination
            v-if="totalPages > 1"
            :page="currentPage"
            :page-count="totalPages"
            :page-size="pageSize"
            @update:page="onPageChange"
          />
        </n-space>
      </template>
    </n-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted, h } from 'vue'
import { AdminApi } from '@/api'
import { TimeUtils } from '@/utils'
import { useMessage } from 'naive-ui'
import { NTag, NButton, NProgress, NSpace, NSelect, NPagination, NIcon, NSpin } from 'naive-ui'

const message = useMessage()
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

const taskTypeLabels = {
  scan: { label: '扫描', type: 'info' },
  hash: { label: '哈希计算', type: 'warning' },
  consistency_check: { label: '一致性检查', type: 'success' },
  cleanup: { label: '清理任务', type: 'default' },
  url_download: { label: 'URL 下载', type: 'primary' }
}

const statusTypeMap = {
  pending: { type: 'default', text: '等待中' },
  running: { type: 'info', text: '运行中' },
  completed: { type: 'success', text: '已完成' },
  failed: { type: 'error', text: '失败' },
  cancelled: { type: 'warning', text: '已取消' }
}

// 活跃任务列
const activeColumns = [
  { title: '任务名称', key: 'taskName', ellipsis: { tooltip: true }, width: 200 },
  {
    title: '类型',
    key: 'taskType',
    width: 100,
    render: (row) => {
      const info = taskTypeLabels[row.taskType] || { label: row.taskType, type: 'default' }
      return h(NTag, { size: 'small', type: info.type }, () => info.label)
    }
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => {
      const info = statusTypeMap[row.status] || { type: 'default', text: row.status }
      return h(NTag, { size: 'small', type: info.type }, () => info.text)
    }
  },
  {
    title: '进度',
    key: 'progress',
    width: 200,
    render: (row) => {
      return h(NSpace, { align: 'center' }, {
        default: () => [
          h(NProgress, {
            type: 'line',
            percentage: row.progress,
            height: 16,
            indicatorPlacement: 'inside',
            processing: row.status === 'running',
            color: row.status === 'failed' ? '#d03050' : undefined,
            railColor: 'rgba(0,0,0,0.08)'
          }),
          row.totalItems > 0 ? `(${row.doneItems}/${row.totalItems})` : null
        ]
      })
    }
  },
  {
    title: '开始时间',
    key: 'startedAt',
    width: 180,
    render: (row) => row.startedAt ? TimeUtils.formatDateTime(row.startedAt) : '-'
  },
  {
    title: '错误信息',
    key: 'errorMessage',
    ellipsis: { tooltip: true },
    width: 200,
    render: (row) => {
      if (!row.errorMessage) return '-'
      return h('span', { style: 'color: #d03050;' }, row.errorMessage)
    }
  }
]

// 历史任务列
const historyColumns = [
  { title: '任务名称', key: 'taskName', ellipsis: { tooltip: true }, width: 200 },
  {
    title: '类型',
    key: 'taskType',
    width: 100,
    render: (row) => {
      const info = taskTypeLabels[row.taskType] || { label: row.taskType, type: 'default' }
      return h(NTag, { size: 'small', type: info.type }, () => info.label)
    }
  },
  {
    title: '状态',
    key: 'status',
    width: 90,
    render: (row) => {
      const info = statusTypeMap[row.status] || { type: 'default', text: row.status }
      return h(NTag, { size: 'small', type: info.type }, () => info.text)
    }
  },
  {
    title: '进度',
    key: 'progress',
    width: 160,
    render: (row) => {
      return h(NProgress, {
        type: 'line',
        percentage: row.progress,
        height: 16,
        indicatorPlacement: 'inside',
        railColor: 'rgba(0,0,0,0.08)',
        color: row.status === 'failed' ? '#d03050' : undefined
      })
    }
  },
  {
    title: '开始时间',
    key: 'startedAt',
    width: 180,
    render: (row) => row.startedAt ? TimeUtils.formatDateTime(row.startedAt) : '-'
  },
  {
    title: '完成时间',
    key: 'endedAt',
    width: 180,
    render: (row) => row.endedAt ? TimeUtils.formatDateTime(row.endedAt) : '-'
  },
  {
    title: '错误信息',
    key: 'errorMessage',
    ellipsis: { tooltip: true },
    width: 200,
    render: (row) => {
      if (!row.errorMessage) return '-'
      return h('span', { style: 'color: #d03050;' }, row.errorMessage)
    }
  }
]

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