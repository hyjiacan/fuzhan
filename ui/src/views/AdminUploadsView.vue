<template>
  <div class="admin-uploads">
    <div class="header-section">
      <h2>上传管理</h2>
      <p class="description">管理僵尸文件和 URL 上传任务</p>
    </div>

    <el-tabs v-model="activeTab">
      <!-- 僵尸文件标签页 -->
      <el-tab-pane name="zombie" label="僵尸文件">
        <!-- 统计卡片 -->
        <el-row :gutter="16" style="margin-bottom: 16px;">
          <el-col :span="8">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <el-icon :size="32" color="#faad14"><ZombieIcon /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ zombieStats.zombieSessions }}</div>
                  <div class="stat-label">过期会话</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <el-icon :size="32" color="#1890ff"><FileIcon /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ zombieStats.totalSessions }}</div>
                  <div class="stat-label">总会话</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="8">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <el-icon :size="32" color="#f5222d"><TrashIcon /></el-icon>
                <div class="stat-info">
                  <div class="stat-value">{{ formatSize(zombieStats.totalSize) }}</div>
                  <div class="stat-label">占用空间</div>
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <!-- 会话列表 -->
        <el-card header="过期会话列表" shadow="never">
          <template #header>
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span>过期会话列表</span>
              <el-space>
                <el-button size="small" type="danger" :disabled="zombieSelectedRowKeys.length === 0" @click="zombieCleanSelected" :loading="zombieCleaning">
                  清理选中 ({{ zombieSelectedRowKeys.length }})
                </el-button>
                <el-button size="small" type="warning" @click="zombieCleanAllExpired" :loading="zombieCleaning">
                  一键清理
                </el-button>
                <el-button size="small" @click="loadZombieSessions" :loading="zombieLoading">刷新</el-button>
              </el-space>
            </div>
          </template>

          <div ref="zombieWrapRef" class="table-v2-wrap" v-loading="zombieLoading">
            <el-table-v2
              :columns="zombieColumns"
              :data="zombieSessions"
              :width="zombieTableWidth"
              :height="zombieTableHeight"
              :row-height="32"
              row-key="id"
            />
          </div>
        </el-card>

        <!-- 确认清理对话框 -->
        <el-dialog v-model="zombieConfirmVisible" title="确认清理" width="400px">
          <el-alert type="warning" :closable="false">
            确定要清理这 {{ zombieSelectedSessions.length }} 个会话吗？此操作将删除相关的残留文件。
          </el-alert>
          <template #footer>
            <el-space>
              <el-button @click="zombieConfirmVisible = false">取消</el-button>
              <el-button type="danger" :loading="zombieCleaning" @click="zombieConfirmClean">确认清理</el-button>
            </el-space>
          </template>
        </el-dialog>
      </el-tab-pane>

      <!-- URL上传标签页 -->
      <el-tab-pane name="url-tasks" label="URL上传">
        <!-- 统计卡片 -->
        <el-row :gutter="16" style="margin-bottom: 16px;">
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value">{{ urlStats.total }}</div>
                  <div class="stat-label">全部任务</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #52c41a;">{{ urlStats.completed }}</div>
                  <div class="stat-label">已完成</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #ff4d4f;">{{ urlStats.failed }}</div>
                  <div class="stat-label">失败</div>
                </div>
              </div>
            </el-card>
          </el-col>
          <el-col :span="6">
            <el-card class="stat-card" shadow="never">
              <div class="stat-content">
                <div class="stat-info">
                  <div class="stat-value" style="color: #1890ff;">{{ urlStats.downloading }}</div>
                  <div class="stat-label">上传中/等待</div>
                </div>
              </div>
            </el-card>
          </el-col>
        </el-row>

        <!-- 过滤和操作栏 -->
        <el-card shadow="never">
          <template #header>
            <div style="display: flex; justify-content: space-between; align-items: center;">
              <span>URL 上传任务</span>
              <el-space>
                <el-button
                  size="small" type="danger"
                  :disabled="urlSelectedRowKeys.length === 0"
                  @click="batchDelete"
                >
                  删除选中 ({{ urlSelectedRowKeys.length }})
                </el-button>
                <el-button
                  size="small" type="warning"
                  :disabled="urlSelectedRowKeys.length === 0"
                  @click="batchRetry"
                >
                  重试选中 ({{ urlSelectedRowKeys.length }})
                </el-button>
                <el-button size="small" @click="loadURLTasks" :loading="urlLoading">刷新</el-button>
              </el-space>
            </div>
          </template>

          <!-- 状态过滤 -->
          <div style="margin-bottom: 12px;">
            <el-radio-group v-model="urlStatusFilter" @change="onURLStatusFilterChange">
              <el-radio-button value="">全部</el-radio-button>
              <el-radio-button value="pending">等待中</el-radio-button>
              <el-radio-button value="downloading">上传中</el-radio-button>
              <el-radio-button value="completed">已完成</el-radio-button>
              <el-radio-button value="failed">失败</el-radio-button>
            </el-radio-group>
          </div>

          <div ref="urlWrapRef" class="table-v2-wrap" v-loading="urlLoading">
            <el-table-v2
              :columns="urlColumns"
              :data="urlTasks"
              :width="urlTableWidth"
              :height="urlTableHeight"
              :row-height="32"
              row-key="id"
            />
          </div>
          <div class="pagination-wrap" v-if="urlTotal > urlPageSize">
            <el-pagination
              v-model:current-page="urlPage"
              v-model:page-size="urlPageSize"
              :total="urlTotal"
              :page-sizes="[10, 20, 50, 100]"
              layout="total, sizes, prev, pager, next"
              @current-change="loadURLTasks"
              @size-change="onURLPageSizeChange"
            />
          </div>
        </el-card>

        <!-- 确认删除对话框 -->
        <el-dialog v-model="deleteModalVisible" title="确认删除" width="400px">
          <el-alert type="warning" :closable="false">
            确定要删除选中的 {{ toDeleteTasks.length }} 个任务吗？相关文件也会被清理。
          </el-alert>
          <template #footer>
            <el-space>
              <el-button @click="deleteModalVisible = false">取消</el-button>
              <el-button type="danger" :loading="deleting" @click="confirmDelete">确认删除</el-button>
            </el-space>
          </template>
        </el-dialog>

        <!-- 确认重试对话框 -->
        <el-dialog v-model="retryModalVisible" title="确认重试" width="400px">
          <el-alert type="warning" :closable="false">
            确定要重试选中的 {{ toRetryTasks.length }} 个失败任务吗？
          </el-alert>
          <template #footer>
            <el-space>
              <el-button @click="retryModalVisible = false">取消</el-button>
              <el-button type="warning" :loading="retrying" @click="confirmRetry">确认重试</el-button>
            </el-space>
          </template>
        </el-dialog>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, reactive, computed, h, onMounted, onUnmounted, nextTick } from 'vue'
import { ElMessage, ElButton, ElTag, ElCheckbox } from 'element-plus'
import { AdminApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'
import store from '@/store'

const activeTab = ref('zombie')

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

// ============ 尺寸测量 ============
const zombieWrapRef = ref(null)
const urlWrapRef = ref(null)
const zombieTableWidth = ref(600)
const zombieTableHeight = ref(300)
const urlTableWidth = ref(600)
const urlTableHeight = ref(300)
let tableResizeObs = null
const updateTableSize = () => {
  const z = zombieWrapRef.value
  if (z && z.clientWidth > 0) {
    zombieTableWidth.value = z.clientWidth
    zombieTableHeight.value = z.clientHeight || 300
  }
  const u = urlWrapRef.value
  if (u && u.clientWidth > 0) {
    urlTableWidth.value = u.clientWidth
    urlTableHeight.value = u.clientHeight || 300
  }
}

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

// 勾选（el-table-v2 不内置选择列，手动实现）
const zombieIsAllSelected = computed(() => {
  return zombieSessions.value.length > 0 &&
    zombieSessions.value.every(s => zombieSelectedRowKeys.value.includes(s.id))
})
const zombieIsIndeterminate = computed(() => {
  if (zombieSessions.value.length === 0) return false
  const count = zombieSessions.value.filter(s => zombieSelectedRowKeys.value.includes(s.id)).length
  return count > 0 && count < zombieSessions.value.length
})
const zombieToggleSelectAll = (val) => {
  zombieSelectedRowKeys.value = val ? zombieSessions.value.map(s => s.id) : []
}
const zombieToggleSingle = (row, val) => {
  if (val) {
    if (!zombieSelectedRowKeys.value.includes(row.id)) {
      zombieSelectedRowKeys.value = zombieSelectedRowKeys.value.concat(row.id)
    }
  } else {
    zombieSelectedRowKeys.value = zombieSelectedRowKeys.value.filter(key => key !== row.id)
  }
}

const statusTypeMap = { default: 'info', info: 'info', success: 'success', warning: 'warning', error: 'danger' }

const zombieColumns = [
  {
    key: 'selection',
    width: 50,
    headerCellRenderer: () => h(ElCheckbox, {
      modelValue: zombieIsAllSelected.value,
      indeterminate: zombieIsIndeterminate.value,
      onChange: (val) => zombieToggleSelectAll(val)
    }),
    cellRenderer: ({ rowData: row }) => h(ElCheckbox, {
      modelValue: zombieSelectedRowKeys.value.includes(row.id),
      onChange: (val) => zombieToggleSingle(row, val)
    })
  },
  {
    title: '文件名',
    key: 'fileName',
    minWidth: 160,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => h('div', { class: 'file-name-cell', title: row.fileName || '' }, [
      h('span', { class: 'file-link' }, row.fileName || '-')
    ])
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    cellRenderer: ({ rowData: row }) => {
      const statusMap = {
        pending: { text: '等待中', type: 'default' },
        in_progress: { text: '上传中', type: 'info' },
        completed: { text: '已完成', type: 'success' },
        cancelled: { text: '已取消', type: 'warning' },
        expired: { text: '已过期', type: 'error' }
      }
      const status = statusMap[row.status] || { text: row.status, type: 'default' }
      return h(ElTag, { type: statusTypeMap[status.type], size: 'small' }, () => status.text)
    }
  },
  {
    title: '文件大小',
    key: 'fileSize',
    width: 120,
    cellRenderer: ({ rowData: row }) => formatSize(row.fileSize)
  },
  {
    title: '已上传',
    key: 'uploadedSize',
    width: 120,
    cellRenderer: ({ rowData: row }) => {
      const percent = row.fileSize > 0 ? Math.round((row.uploadedSize / row.fileSize) * 100) : 0
      return `${formatSize(row.uploadedSize)} (${percent}%)`
    }
  },
  {
    title: '创建时间',
    key: 'createdAt',
    width: 180,
    cellRenderer: ({ rowData: row }) => row.createdAt ? TimeUtils.formatDateTime(row.createdAt) : '-'
  },
  {
    title: '过期时间',
    key: 'expiredAt',
    width: 180,
    cellRenderer: ({ rowData: row }) => row.expiredAt ? TimeUtils.formatDateTime(row.expiredAt) : '-'
  },
  {
    title: '操作',
    key: 'actions',
    width: 80,
    cellRenderer: ({ rowData: row }) => {
      if (row.status === 'completed' || row.status === 'cancelled') {
        return null
      }
      return h(ElButton, {
        size: 'small',
        type: 'danger',
        link: true,
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
    nextTick(updateTableSize)
  }
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

const zombieCleanAllExpired = () => {
  const expiredSessions = zombieSessions.value.filter(s =>
    s.status === 'expired' || s.status === 'pending' || s.status === 'in_progress'
  )
  if (expiredSessions.length === 0) {
    ElMessage.info('没有需要清理的过期会话')
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
      ElMessage.success(`成功清理 ${sessionIds.length} 个会话`)
      zombieConfirmVisible.value = false
      zombieSelectedRowKeys.value = []
      zombieSelectedSessions.value = []
      loadZombieSessions()
    } else {
      ElMessage.error(data.message || '清理失败')
    }
  } catch (e) {
    ElMessage.error('清理失败')
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
const urlPage = ref(1)
const urlPageSize = ref(20)
const urlTotal = ref(0)

const urlStats = reactive({
  total: 0,
  completed: 0,
  failed: 0,
  downloading: 0
})

const urlTasks = ref([])

// 勾选（el-table-v2 不内置选择列，手动实现）
const urlIsAllSelected = computed(() => {
  return urlTasks.value.length > 0 &&
    urlTasks.value.every(t => urlSelectedRowKeys.value.includes(t.id))
})
const urlIsIndeterminate = computed(() => {
  if (urlTasks.value.length === 0) return false
  const count = urlTasks.value.filter(t => urlSelectedRowKeys.value.includes(t.id)).length
  return count > 0 && count < urlTasks.value.length
})
const urlToggleSelectAll = (val) => {
  urlSelectedRowKeys.value = val ? urlTasks.value.map(t => t.id) : []
}
const urlToggleSingle = (row, val) => {
  if (val) {
    if (!urlSelectedRowKeys.value.includes(row.id)) {
      urlSelectedRowKeys.value = urlSelectedRowKeys.value.concat(row.id)
    }
  } else {
    urlSelectedRowKeys.value = urlSelectedRowKeys.value.filter(key => key !== row.id)
  }
}

const statusTextOptions = {
  pending: { text: '等待中', type: 'default' },
  downloading: { text: '上传中', type: 'info' },
  completed: { text: '已完成', type: 'success' },
  failed: { text: '失败', type: 'error' }
}

const urlColumns = [
  {
    key: 'selection',
    width: 50,
    headerCellRenderer: () => h(ElCheckbox, {
      modelValue: urlIsAllSelected.value,
      indeterminate: urlIsIndeterminate.value,
      onChange: (val) => urlToggleSelectAll(val)
    }),
    cellRenderer: ({ rowData: row }) => h(ElCheckbox, {
      modelValue: urlSelectedRowKeys.value.includes(row.id),
      onChange: (val) => urlToggleSingle(row, val)
    })
  },
  {
    title: '文件名',
    key: 'fileName',
    minWidth: 160,
    flexGrow: 1,
    cellRenderer: ({ rowData: row }) => h('div', { class: 'file-name-cell', title: row.fileName || '' }, [
      h('span', { class: 'file-link' }, row.fileName || '-')
    ])
  },
  {
    title: 'URL',
    key: 'url',
    width: 200,
    flexShrink: 1,
    cellRenderer: ({ rowData: row }) => {
      return h('div', { class: 'file-name-cell', title: row.url }, [
        h('span', { class: 'file-link', style: 'color: rgba(0,0,0,0.6); font-size: 12px;' }, row.url || '-')
      ])
    }
  },
  {
    title: '状态',
    key: 'status',
    width: 100,
    cellRenderer: ({ rowData: row }) => {
      const s = statusTextOptions[row.status] || { text: row.status, type: 'default' }
      return h(ElTag, { type: statusTypeMap[s.type], size: 'small' }, () => s.text)
    }
  },
  {
    title: '存储类型',
    key: 'storageType',
    width: 90,
    cellRenderer: ({ rowData: row }) => {
      const map = { temp: '临时', private: '私有', regular: '普通' }
      return map[row.storageType] || row.storageType
    }
  },
  {
    title: '文件大小',
    key: 'fileSize',
    width: 100,
    cellRenderer: ({ rowData: row }) => formatSize(row.fileSize)
  },
  {
    title: '上传进度',
    key: 'downloadedBytes',
    width: 150,
    cellRenderer: ({ rowData: row }) => {
      if (row.status === 'completed') return '100%'
      if (row.fileSize <= 0) return '-'
      const pct = Math.round((row.downloadedBytes / row.fileSize) * 100)
      return h('div', { style: { display: 'flex', alignItems: 'center', gap: '6px' } }, [
        h('div', {
          style: {
            width: '80px', height: '6px', background: 'rgba(0,0,0,0.1)',
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
        h('span', { style: { fontSize: '12px', color: 'rgba(0,0,0,0.6)' } },
          `${formatSize(row.downloadedBytes)} / ${formatSize(row.fileSize)}`)
      ])
    }
  },
  {
    title: '错误信息',
    key: 'errorMessage',
    width: 180,
    cellRenderer: ({ rowData: row }) => {
      if (!row.errorMessage) return null
      return h('span', { style: { color: '#ff4d4f', fontSize: '12px' } }, row.errorMessage)
    }
  },
  {
    title: '创建时间',
    key: 'createdAt',
    width: 170,
    cellRenderer: ({ rowData: row }) => {
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
    cellRenderer: ({ rowData: row }) => row.completedAt ? TimeUtils.formatDateTime(row.completedAt) : '-'
  },
  {
    title: '操作',
    key: 'actions',
    width: 140,
    cellRenderer: ({ rowData: row }) => {
      const btns = []
      if (row.status === 'failed') {
        btns.push(h(ElButton, {
          size: 'small', type: 'warning', link: true,
          onClick: () => retrySingle(row)
        }, () => '重试'))
      }
      btns.push(h(ElButton, {
        size: 'small', type: 'danger', link: true,
        onClick: () => deleteSingle(row)
      }, () => '删除'))
      return h('div', { style: { display: 'flex', gap: '8px' } }, btns)
    }
  }
]

function loadURLTasks() {
  urlLoading.value = true
  AdminApi.getURLTasks(urlPage.value, urlPageSize.value, urlStatusFilter.value)
    .then(res => {
      if (res.success) {
        urlTasks.value = res.data.items || []
        urlPage.value = res.data.page
        urlPageSize.value = res.data.pageSize
        urlTotal.value = res.data.total
        updateURLStats(res.data.items || [])
      } else {
        ElMessage.error(res.message || '加载失败')
      }
    })
    .catch(() => ElMessage.error('加载任务列表失败'))
    .finally(() => {
      urlLoading.value = false
      nextTick(updateTableSize)
    })
}

function updateURLStats(items) {
  urlStats.total = items.length
  urlStats.completed = items.filter(t => t.status === 'completed').length
  urlStats.failed = items.filter(t => t.status === 'failed').length
  urlStats.downloading = items.filter(t => t.status === 'pending' || t.status === 'downloading').length
}

function onURLStatusFilterChange() {
  urlPage.value = 1
  urlSelectedRowKeys.value = []
  loadURLTasks()
}

function onURLPageSizeChange() {
  urlPage.value = 1
  loadURLTasks()
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
    ElMessage.info('请先选择任务')
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
    ElMessage.info('没有可重试的失败任务')
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
    ElMessage.success(`成功删除 ${successCount} 个任务`)
    deleteModalVisible.value = false
    urlSelectedRowKeys.value = []
    toDeleteTasks.value = []
    loadURLTasks()
  } catch (e) {
    ElMessage.error('删除失败')
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
    ElMessage.success(`成功重试 ${successCount} 个任务`)
    retryModalVisible.value = false
    urlSelectedRowKeys.value = []
    toRetryTasks.value = []
    loadURLTasks()
  } catch (e) {
    ElMessage.error('重试失败')
  } finally {
    retrying.value = false
  }
}

onMounted(() => {
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (zombieWrapRef.value) tableResizeObs.observe(zombieWrapRef.value)
  if (urlWrapRef.value) tableResizeObs.observe(urlWrapRef.value)
  loadZombieSessions()
  loadURLTasks()
})

onUnmounted(() => {
  tableResizeObs?.disconnect()
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

  .stat-card :deep(.el-card__body) {
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

  .table-v2-wrap {
    height: 420px;
  }

  .pagination-wrap {
    display: flex;
    justify-content: flex-end;
    padding: 16px 0 8px;
  }
}
</style>