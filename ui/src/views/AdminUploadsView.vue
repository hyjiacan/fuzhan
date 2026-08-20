<template>
  <div class="admin-uploads">
    <div class="header-section">
      <h2>上传管理</h2>
      <p class="description">管理僵尸文件和 URL 上传任务</p>
    </div>

    <n-tabs type="line" animated default-value="zombie">
      <!-- 僵尸文件标签页 -->
      <n-tab-pane name="zombie" tab="僵尸文件">
        <!-- 统计卡片 -->
        <n-grid :cols="3" :x-gap="16" :y-gap="16" style="margin-bottom: 16px;">
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="ZombieIcon" size="32" color="#faad14" />
                <div class="stat-info">
                  <div class="stat-value">{{ zombieStats.zombieSessions }}</div>
                  <div class="stat-label">过期会话</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="FileIcon" size="32" color="#1890ff" />
                <div class="stat-info">
                  <div class="stat-value">{{ zombieStats.totalSessions }}</div>
                  <div class="stat-label">总会话</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <n-icon :component="TrashIcon" size="32" color="#f5222d" />
                <div class="stat-info">
                  <div class="stat-value">{{ formatSize(zombieStats.totalSize) }}</div>
                  <div class="stat-label">占用空间</div>
                </div>
              </div>
            </n-card>
          </n-gi>
        </n-grid>

        <!-- 会话列表 -->
        <n-card title="过期会话列表">
          <template #header-extra>
            <n-space>
              <n-button size="small" type="error" :disabled="zombieSelectedRowKeys.length === 0" @click="zombieCleanSelected" :loading="zombieCleaning">
                清理选中 ({{ zombieSelectedRowKeys.length }})
              </n-button>
              <n-button size="small" type="warning" @click="zombieCleanAllExpired" :loading="zombieCleaning">
                一键清理
              </n-button>
              <n-button size="small" @click="loadZombieSessions" :loading="zombieLoading">刷新</n-button>
            </n-space>
          </template>

          <n-data-table
            :columns="zombieColumns"
            :data="zombieSessions"
            :loading="zombieLoading"
            :pagination="zombiePagination"
            :row-key="row => row.id"
            :checked-row-keys="zombieSelectedRowKeys"
            @update:checked-row-keys="zombieHandleSelectionChange"
          />
        </n-card>

        <!-- 确认清理对话框 -->
        <n-modal v-model:show="zombieConfirmVisible" preset="card" title="确认清理" style="width: 400px">
          <n-alert type="warning">
            确定要清理这 {{ zombieSelectedSessions.length }} 个会话吗？此操作将删除相关的残留文件。
          </n-alert>
          <template #footer>
            <n-space justify="end">
              <n-button @click="zombieConfirmVisible = false">取消</n-button>
              <n-button type="error" :loading="zombieCleaning" @click="zombieConfirmClean">确认清理</n-button>
            </n-space>
          </template>
        </n-modal>
      </n-tab-pane>

      <!-- URL上传标签页 -->
      <n-tab-pane name="url-tasks" tab="URL上传">
        <!-- 统计卡片 -->
        <n-grid :cols="4" :x-gap="16" :y-gap="16" style="margin-bottom: 16px;">
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value">{{ urlStats.total }}</div>
                  <div class="stat-label">全部任务</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #52c41a;">{{ urlStats.completed }}</div>
                  <div class="stat-label">已完成</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #ff4d4f;">{{ urlStats.failed }}</div>
                  <div class="stat-label">失败</div>
                </div>
              </div>
            </n-card>
          </n-gi>
          <n-gi>
            <n-card class="stat-card">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #1890ff;">{{ urlStats.downloading }}</div>
                  <div class="stat-label">上传中/等待</div>
                </div>
              </div>
            </n-card>
          </n-gi>
        </n-grid>

        <!-- 过滤和操作栏 -->
        <n-card>
          <template #header-extra>
            <n-space>
              <n-button
                size="small" type="error"
                :disabled="urlSelectedRowKeys.length === 0"
                @click="batchDelete"
              >
                删除选中 ({{ urlSelectedRowKeys.length }})
              </n-button>
              <n-button
                size="small" type="warning"
                :disabled="urlSelectedRowKeys.length === 0"
                @click="batchRetry"
              >
                重试选中 ({{ urlSelectedRowKeys.length }})
              </n-button>
              <n-button size="small" @click="loadURLTasks" :loading="urlLoading">刷新</n-button>
            </n-space>
          </template>

          <!-- 状态过滤 -->
          <div style="margin-bottom: 12px;">
            <n-radio-group v-model:value="urlStatusFilter" size="small" @update-value="onURLStatusFilterChange">
              <n-radio-button value="">全部</n-radio-button>
              <n-radio-button value="pending">等待中</n-radio-button>
              <n-radio-button value="downloading">上传中</n-radio-button>
              <n-radio-button value="completed">已完成</n-radio-button>
              <n-radio-button value="failed">失败</n-radio-button>
            </n-radio-group>
          </div>

          <n-data-table
            :columns="urlColumns"
            :data="urlTasks"
            :loading="urlLoading"
            :pagination="urlPagination"
            :row-key="row => row.id"
            :checked-row-keys="urlSelectedRowKeys"
            @update:checked-row-keys="handleURLSelectionChange"
          />
        </n-card>

        <!-- 确认删除对话框 -->
        <n-modal v-model:show="deleteModalVisible" preset="card" title="确认删除" style="width: 400px">
          <n-alert type="warning">
            确定要删除选中的 {{ toDeleteTasks.length }} 个任务吗？相关文件也会被清理。
          </n-alert>
          <template #footer>
            <n-space justify="end">
              <n-button @click="deleteModalVisible = false">取消</n-button>
              <n-button type="error" :loading="deleting" @click="confirmDelete">确认删除</n-button>
            </n-space>
          </template>
        </n-modal>

        <!-- 确认重试对话框 -->
        <n-modal v-model:show="retryModalVisible" preset="card" title="确认重试" style="width: 400px">
          <n-alert type="warning">
            确定要重试选中的 {{ toRetryTasks.length }} 个失败任务吗？
          </n-alert>
          <template #footer>
            <n-space justify="end">
              <n-button @click="retryModalVisible = false">取消</n-button>
              <n-button type="warning" :loading="retrying" @click="confirmRetry">确认重试</n-button>
            </n-space>
          </template>
        </n-modal>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup>
import { ref, reactive, h, onMounted } from 'vue'
import {
  NGrid, NGi, NCard, NButton, NSpace, NDataTable,
  NRadioGroup, NRadioButton, NAlert, NModal, NIcon, NTabs, NTabPane,
  useMessage
} from 'naive-ui'
import { AdminApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'
import store from '@/store'

const message = useMessage()

// ============ 格式工具 ============
const formatSize = (bytes) => NumberUtils.formatFileSize(bytes || 0)

// ============ Icons ============
const FileIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M14 2H6c-1.1 0-2 .9-2 2v16c0 1.1.89 2 1.99 2H18c1.1 0 2-.9 2-2V8l-6-6zm4 18H6V4h7v5h5v11z' })
])
const ZombieIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-2h2v2zm0-4h-2V7h2v6z' })
])
const TrashIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z' })
])

// ============ 僵尸文件部分 ============
const zombieLoading = ref(false)
const zombieCleaning = ref(false)
const zombieConfirmVisible = ref(false)
const zombieSelectedRowKeys = ref([])
const zombieSelectedSessions = ref([])

const zombieStats = reactive({
  zombieSessions: 0,
  totalSessions: 0,
  totalSize: 0
})

const zombieSessions = ref([])

const zombiePagination = {
  pageSize: 20
}

const zombieColumns = [
  {
    title: '文件名',
    key: 'fileName',
    ellipsis: { tooltip: true }
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      const statusMap = {
        pending: { text: '等待中', type: 'default' },
        in_progress: { text: '上传中', type: 'info' },
        completed: { text: '已完成', type: 'success' },
        cancelled: { text: '已取消', type: 'warning' },
        expired: { text: '已过期', type: 'error' }
      }
      const status = statusMap[row.status] || { text: row.status, type: 'default' }
      return h('n-tag', { type: status.type, size: 'small' }, () => status.text)
    }
  },
  {
    title: '文件大小',
    key: 'fileSize',
    width: 120,
    render(row) {
      return formatSize(row.fileSize)
    }
  },
  {
    title: '已上传',
    key: 'uploadedSize',
    width: 120,
    render(row) {
      const percent = row.fileSize > 0 ? Math.round((row.uploadedSize / row.fileSize) * 100) : 0
      return `${formatSize(row.uploadedSize)} (${percent}%)`
    }
  },
  {
    title: '创建时间',
    key: 'createdAt',
    width: 180,
    render(row) {
      return row.createdAt || '-'
    }
  },
  {
    title: '过期时间',
    key: 'expiredAt',
    width: 180,
    render(row) {
      return row.expiredAt || '-'
    }
  },
  {
    type: 'selection',
    width: 50
  },
  {
    title: '操作',
    key: 'actions',
    width: 80,
    render(row) {
      if (row.status === 'completed' || row.status === 'cancelled') {
        return null
      }
      return h(NButton, {
        size: 'small',
        type: 'error',
        quaternary: true,
        onClick: () => zombieCleanSession(row)
      }, () => '清理')
    }
  }
]

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

const zombieCleanAllExpired = () => {
  const expiredSessions = zombieSessions.value.filter(s =>
    s.status === 'expired' || s.status === 'pending' || s.status === 'in_progress'
  )
  if (expiredSessions.length === 0) {
    message.info('没有需要清理的过期会话')
    return
  }
  zombieSelectedRowKeys.value = expiredSessions.map(s => s.id)
  zombieSelectedSessions.value = expiredSessions
  zombieConfirmVisible.value = true
}

const zombieConfirmClean = async () => {
  zombieCleaning.value = true
  try {
    const sessionIds = zombieSelectedSessions.value.map(s => s.id)
    const data = await AdminApi.cleanupSessions(sessionIds)
    if (data.success) {
      message.success(`成功清理 ${sessionIds.length} 个会话`)
      zombieConfirmVisible.value = false
      zombieSelectedRowKeys.value = []
      zombieSelectedSessions.value = []
      loadZombieSessions()
    } else {
      message.error(data.message || '清理失败')
    }
  } catch (e) {
    message.error('清理失败')
  } finally {
    zombieCleaning.value = false
  }
}

// ============ URL上传部分 ============
const urlLoading = ref(false)
const deleting = ref(false)
const retrying = ref(false)
const deleteModalVisible = ref(false)
const retryModalVisible = ref(false)
const toDeleteTasks = ref([])
const toRetryTasks = ref([])
const urlSelectedRowKeys = ref([])
const urlStatusFilter = ref('')

const urlStats = reactive({
  total: 0,
  completed: 0,
  failed: 0,
  downloading: 0
})

const urlTasks = ref([])

const urlPagination = reactive({
  page: 1,
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  onChange: (page) => {
    urlPagination.page = page
    loadURLTasks()
  },
  onPageSizeChange: (pageSize) => {
    urlPagination.pageSize = pageSize
    urlPagination.page = 1
    loadURLTasks()
  }
})

const statusOptions = {
  pending: { text: '等待中', type: 'default' },
  downloading: { text: '上传中', type: 'info' },
  completed: { text: '已完成', type: 'success' },
  failed: { text: '失败', type: 'error' }
}

const urlColumns = [
  {
    type: 'selection',
    width: 50
  },
  {
    title: '文件名',
    key: 'fileName',
    width: 200,
    ellipsis: { tooltip: true }
  },
  {
    title: 'URL',
    key: 'url',
    width: 250,
    ellipsis: { tooltip: true },
    render(row) {
      return h('span', { style: { color: 'rgba(255,255,255,0.6)', fontSize: '12px' } }, row.url)
    }
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    render(row) {
      const s = statusOptions[row.status] || { text: row.status, type: 'default' }
      return h('n-tag', { type: s.type, size: 'small' }, () => s.text)
    }
  },
  {
    title: '存储类型',
    key: 'storageType',
    width: 90,
    render(row) {
      const map = { temp: '临时', private: '私有', regular: '普通' }
      return map[row.storageType] || row.storageType
    }
  },
  {
    title: '文件大小',
    key: 'fileSize',
    width: 100,
    render(row) {
      return formatSize(row.fileSize)
    }
  },
  {
    title: '上传进度',
    key: 'downloadedBytes',
    width: 150,
    render(row) {
      if (row.status === 'completed') return '100%'
      if (row.fileSize <= 0) return '-'
      const pct = Math.round((row.downloadedBytes / row.fileSize) * 100)
      return h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } }, [
        h('div', {
          style: {
            width: '80px', height: '6px', background: 'rgba(255,255,255,0.1)',
            borderRadius: '3px', overflow: 'hidden'
          }
        }, [
          h('div', {
            style: {
              width: `${Math.min(pct, 100)}%`, height: '100%',
              background: row.status === 'failed' ? '#ff4d4f' : '#1890ff',
              borderRadius: '3px', transition: 'width 0.3s'
            }
          })
        ]),
        h('span', { style: { fontSize: '12px', color: 'rgba(255,255,255,0.6)' } },
          `${formatSize(row.downloadedBytes)} / ${formatSize(row.fileSize)}`)
      ])
    }
  },
  {
    title: '错误信息',
    key: 'errorMessage',
    width: 180,
    ellipsis: { tooltip: true },
    render(row) {
      if (!row.errorMessage) return null
      return h('span', { style: { color: '#ff4d4f', fontSize: '12px' } }, row.errorMessage)
    }
  },
  {
    title: '创建时间',
    key: 'createdAt',
    width: 170,
    render(row) {
      const text = TimeUtils.formatDateTime(row.createdAt)
      if (TimeUtils.isRecent24h(row.createdAt)) {
        return h('span', { style: 'color: #18a058' }, text)
      }
      return text
    }
  },
  {
    title: '完成时间',
    key: 'completedAt',
    width: 170,
    render(row) {
      return row.completedAt ? TimeUtils.formatDateTime(row.completedAt) : '-'
    }
  },
  {
    title: '操作',
    key: 'actions',
    width: 140,
    render(row) {
      const btns = []
      if (row.status === 'failed') {
        btns.push(h(NButton, {
          size: 'small', type: 'warning', quaternary: true,
          onClick: () => retrySingle(row)
        }, () => '重试'))
      }
      btns.push(h(NButton, {
        size: 'small', type: 'error', quaternary: true,
        onClick: () => deleteSingle(row)
      }, () => '删除'))
      return h('div', { style: { display: 'flex', gap: '4px' } }, btns)
    }
  }
]

function loadURLTasks() {
  urlLoading.value = true
  AdminApi.getURLTasks(urlPagination.page, urlPagination.pageSize, urlStatusFilter.value)
    .then(res => {
      if (res.success) {
        urlTasks.value = res.data.items || []
        urlPagination.page = res.data.page
        urlPagination.pageSize = res.data.pageSize
        urlPagination.itemCount = res.data.total
        updateURLStats(res.data.items || [])
      } else {
        message.error(res.message || '加载失败')
      }
    })
    .catch(() => message.error('加载任务列表失败'))
    .finally(() => { urlLoading.value = false })
}

function updateURLStats(items) {
  urlStats.total = items.length
  urlStats.completed = items.filter(t => t.status === 'completed').length
  urlStats.failed = items.filter(t => t.status === 'failed').length
  urlStats.downloading = items.filter(t => t.status === 'pending' || t.status === 'downloading').length
}

function onURLStatusFilterChange() {
  urlPagination.page = 1
  urlSelectedRowKeys.value = []
  loadURLTasks()
}

function handleURLSelectionChange(keys) {
  urlSelectedRowKeys.value = keys
}

function deleteSingle(row) {
  toDeleteTasks.value = [row]
  deleteModalVisible.value = true
}

function retrySingle(row) {
  toRetryTasks.value = [row]
  retryModalVisible.value = true
}

function batchDelete() {
  const selected = urlTasks.value.filter(t => urlSelectedRowKeys.value.includes(t.id))
  if (selected.length === 0) {
    message.info('请先选择任务')
    return
  }
  toDeleteTasks.value = selected
  deleteModalVisible.value = true
}

function batchRetry() {
  const selected = urlTasks.value.filter(t =>
    urlSelectedRowKeys.value.includes(t.id) && t.status === 'failed'
  )
  if (selected.length === 0) {
    message.info('没有可重试的失败任务')
    return
  }
  toRetryTasks.value = selected
  retryModalVisible.value = true
}

async function confirmDelete() {
  deleting.value = true
  try {
    let successCount = 0
    for (const task of toDeleteTasks.value) {
      try {
        const res = await AdminApi.deleteURLTask(task.id)
        if (res.success) successCount++
      } catch (e) {
        console.error('删除任务失败:', task.id, e)
      }
    }
    message.success(`成功删除 ${successCount} 个任务`)
    deleteModalVisible.value = false
    urlSelectedRowKeys.value = []
    toDeleteTasks.value = []
    loadURLTasks()
  } catch (e) {
    message.error('删除失败')
  } finally {
    deleting.value = false
  }
}

async function confirmRetry() {
  retrying.value = true
  store.setActiveUrlTasks(true)
  try {
    let successCount = 0
    for (const task of toRetryTasks.value) {
      try {
        const res = await AdminApi.retryURLTask(task.id)
        if (res.success) successCount++
      } catch (e) {
        console.error('重试任务失败:', task.id, e)
      }
    }
    message.success(`成功重试 ${successCount} 个任务`)
    retryModalVisible.value = false
    urlSelectedRowKeys.value = []
    toRetryTasks.value = []
    loadURLTasks()
  } catch (e) {
    message.error('重试失败')
  } finally {
    retrying.value = false
  }
}

onMounted(() => {
  loadZombieSessions()
  loadURLTasks()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-uploads {
  display: flex;
  flex-direction: column;
  gap: 0;
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

  .stat-card .n-card__content {
    padding: 16px;
  }

  .stat-content {
    display: flex;
    align-items: center;
    gap: 16px;
  }

  .stat-info {
    .stat-value {
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .stat-label {
      font-size: @font-size-base;
      color: @text-color-secondary;
    }
  }
}
</style>
