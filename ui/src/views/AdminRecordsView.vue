<template>
  <div class="admin-records">
    <div class="header-section">
      <div class="header-left">
        <h2>记录管理</h2>
        <p class="description">查看公开文件的操作记录</p>
      </div>
      <div class="header-right">
        <el-button
          type="warning"
          size="small"
          :loading="clearingRecord === 'search'"
          :disabled="clearingRecord !== null"
          @click="handleClearRecords('search')"
        >
          <el-icon><DeleteIcon /></el-icon>
          清空搜索
        </el-button>
        <el-button
          type="danger"
          size="small"
          :loading="clearingRecord === 'upload'"
          :disabled="clearingRecord !== null"
          @click="handleClearRecords('upload')"
        >
          <el-icon><DeleteIcon /></el-icon>
          清空上传
        </el-button>
        <el-button
          type="danger"
          size="small"
          :loading="clearingRecord === 'download'"
          :disabled="clearingRecord !== null"
          @click="handleClearRecords('download')"
        >
          <el-icon><DeleteIcon /></el-icon>
          清空下载
        </el-button>
      </div>
    </div>

    <el-tabs
      v-model="activeTab"
      @tab-change="handleTabChange"
      class="records-tabs"
    >
      <el-tab-pane name="upload" label="最近上传">
        <div ref="uploadWrapRef" class="table-v2-wrap" v-loading="loading">
          <el-table-v2
            :columns="uploadColumns"
            :data="uploadRecords"
            :width="uploadWidth"
            :height="uploadHeight"
            :row-height="32"
            row-key="id"
          />
        </div>
        <div class="pagination-wrap" v-if="uploadTotal > 0">
          <el-pagination
            v-model:current-page="uploadPage"
            :page-size="pageSize"
            :total="uploadTotal"
            layout="prev, pager, next"
            @current-change="() => loadRecords('upload')"
          />
        </div>
      </el-tab-pane>

      <el-tab-pane name="download" label="最近下载">
        <div ref="downloadWrapRef" class="table-v2-wrap" v-loading="loading">
          <el-table-v2
            :columns="downloadColumns"
            :data="downloadRecords"
            :width="downloadWidth"
            :height="downloadHeight"
            :row-height="32"
            row-key="id"
          />
        </div>
        <div class="pagination-wrap" v-if="downloadTotal > 0">
          <el-pagination
            v-model:current-page="downloadPage"
            :page-size="pageSize"
            :total="downloadTotal"
            layout="prev, pager, next"
            @current-change="() => loadRecords('download')"
          />
        </div>
      </el-tab-pane>

      <el-tab-pane name="search" label="最近搜索">
        <div ref="searchWrapRef" class="table-v2-wrap" v-loading="loading">
          <el-table-v2
            :columns="searchColumns"
            :data="searchRecords"
            :width="searchWidth"
            :height="searchHeight"
            :row-height="32"
            row-key="id"
          />
        </div>
        <div class="pagination-wrap" v-if="searchTotal > 0">
          <el-pagination
            v-model:current-page="searchPage"
            :page-size="pageSize"
            :total="searchTotal"
            layout="prev, pager, next"
            @current-change="() => loadRecords('search')"
          />
        </div>
      </el-tab-pane>
    </el-tabs>
  </div>
</template>

<script setup>
import { ref, h, onMounted, onUnmounted, nextTick, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElButton } from 'element-plus'
import { FileApi, AdminApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'

const route = useRoute()
const router = useRouter()

// ============ 状态 ============

const activeTab = ref('upload')
const loading = ref(false)
const pageSize = 20

// 上传
const uploadRecords = ref([])
const uploadTotal = ref(0)
const uploadPage = ref(1)

// 下载
const downloadRecords = ref([])
const downloadTotal = ref(0)
const downloadPage = ref(1)

// 搜索
const searchRecords = ref([])
const searchTotal = ref(0)
const searchPage = ref(1)

// 清空
const clearingRecord = ref(null)

// 单条删除
const deletingRecordId = ref(null)

// ============ 图标 ============

const DeleteIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z' })
])

// ============ el-table-v2 尺寸测量 ============
const uploadWrapRef = ref(null)
const downloadWrapRef = ref(null)
const searchWrapRef = ref(null)
const uploadWidth = ref(600)
const uploadHeight = ref(300)
const downloadWidth = ref(600)
const downloadHeight = ref(300)
const searchWidth = ref(600)
const searchHeight = ref(300)
let tableResizeObs = null

const updateTableSize = () => {
  const u = uploadWrapRef.value
  if (u && u.clientWidth > 0) {
    uploadWidth.value = u.clientWidth
    uploadHeight.value = u.clientHeight || 300
  }
  const d = downloadWrapRef.value
  if (d && d.clientWidth > 0) {
    downloadWidth.value = d.clientWidth
    downloadHeight.value = d.clientHeight || 300
  }
  const s = searchWrapRef.value
  if (s && s.clientWidth > 0) {
    searchWidth.value = s.clientWidth
    searchHeight.value = s.clientHeight || 300
  }
}

// ============ 表格列定义 ============

const formatSize = (bytes) => NumberUtils.formatFileSize(bytes || 0)

const formatTime = (time) => (time ? TimeUtils.formatDateTime(time) : '')

// 最近 24h 内的新记录显示为绿色（仅上传时间列应用；下载时间固定显示，不标绿）
const renderRecentTime = (time) => {
  if (!time) return h('span', { style: 'color:var(--el-text-color-placeholder);' }, '-')
  const text = TimeUtils.formatDateTime(time)
  if (TimeUtils.isRecent24h(time)) {
    return h('span', { style: 'color: #18a058' }, text)
  }
  return text
}

// 长文本列渲染：超出列宽时省略号截断，并保留完整内容 title 提示
const formatTypeText = (text) => h('div', { class: 'file-name-cell', title: text || '' }, [
  h('span', { class: 'file-link' }, text || '-')
])

// 依据文件名末位扩展名取文件类型图标类名
const getFileIconClass = (fullPath) => {
  const name = fullPath.split('/').filter(Boolean).pop() || ''
  const ext = name.split('.').pop()?.toLowerCase() || ''
  return `icon-filetype-${ext}`
}

// 记录管理页文件路径列：与"最近"页一致，把路径段拆分渲染，目录段可点击，
// 但跳转目标是「文件管理」页（/admin/files/<目录>）而非公共文件页。
const renderRecordPathCell = (row) => {
  const fullPath = row.fullPath || ''
  const segments = fullPath.split('/').filter(Boolean)
  const fileName = segments[segments.length - 1] || row.fileName || '未知文件'
  const dirSegments = segments.slice(0, -1)
  const iconClass = `icon-filetype ${getFileIconClass(fullPath)}`
  const navigateToFileDir = (dirPath) => {
    router.push('/admin/files/' + dirPath.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/'))
  }

  return h('div', {
    class: 'file-name-cell',
    title: fileName
  }, [
    h('div', { class: 'file-icon-wrapper' }, [
      h('span', { class: iconClass })
    ]),
    h('span', { class: 'file-path-content' }, [
      ...dirSegments.map((seg, idx) => {
        const dirPath = '/' + dirSegments.slice(0, idx + 1).join('/')
        return [
          h('a', {
            class: 'path-segment',
            onClick: (e) => {
              e.preventDefault()
              e.stopPropagation()
              navigateToFileDir(dirPath)
            }
          }, seg),
          '/'
        ]
      }).flat(),
      h('span', { class: 'file-link', style: 'cursor:default;' }, fileName)
    ])
  ])
}

// 每条记录的操作列（单条删除）
const deleteRecordColumn = (action) => ({
  title: '操作',
  key: 'actions',
  dataKey: 'id',
  width: 76,
  fixed: 'right',
  align: 'center',
  cellRenderer: ({ rowData: row }) => {
    if (row.id === undefined || row.id === null) return null
    return h(ElButton, {
      type: 'danger',
      link: true,
      size: 'small',
      loading: deletingRecordId.value === row.id,
      disabled: deletingRecordId.value !== null && deletingRecordId.value !== row.id,
      onClick: () => handleDeleteRecord(row, action)
    }, { default: () => '删除' })
  }
})

const uploadColumns = [
  { title: '文件路径', key: 'fullPath', minWidth: 260, flexGrow: 1, cellRenderer: ({ rowData: row }) => renderRecordPathCell(row) },
  { title: '大小', key: 'fileSize', width: 100,
    cellRenderer: ({ rowData: row }) => formatSize(row.fileSize)
  },
  { title: 'IP地址', key: 'clientIP', dataKey: 'clientIP', width: 140, cellRenderer: ({ rowData: row }) => row.clientIP || '-' },
  { title: '上传时间', key: 'createdAt', dataKey: 'createdAt', width: 170,
    cellRenderer: ({ rowData: row }) => renderRecentTime(row.createdAt)
  },
  deleteRecordColumn('upload')
]

const downloadColumns = [
  { title: '文件路径', key: 'fullPath', minWidth: 260, flexGrow: 1, cellRenderer: ({ rowData: row }) => renderRecordPathCell(row) },
  { title: '大小', key: 'fileSize', width: 100,
    cellRenderer: ({ rowData: row }) => formatSize(row.fileSize)
  },
  { title: 'IP地址', key: 'clientIP', dataKey: 'clientIP', width: 140, cellRenderer: ({ rowData: row }) => row.clientIP || '-' },
  { title: '下载时间', key: 'createdAt', dataKey: 'createdAt', width: 170,
    cellRenderer: ({ rowData: row }) => formatTime(row.createdAt)
  },
  { title: '上传时间', key: 'uploadTime', dataKey: 'uploadTime', width: 170,
    cellRenderer: ({ rowData: row }) => (row.uploadTime && !String(row.uploadTime).startsWith('0001'))
      ? renderRecentTime(row.uploadTime)
      : h('span', { style: 'color:var(--el-text-color-placeholder);' }, '-')
  },
  deleteRecordColumn('download')
]

const searchColumns = [
  { title: '搜索关键词', key: 'searchQuery', minWidth: 200, flexGrow: 1, cellRenderer: ({ rowData: row }) => formatTypeText(row.searchQuery) },
  { title: 'IP地址', key: 'clientIP', dataKey: 'clientIP', width: 140, cellRenderer: ({ rowData: row }) => row.clientIP || '-' },
  { title: '搜索时间', key: 'createdAt', dataKey: 'createdAt', width: 170,
    cellRenderer: ({ rowData: row }) => formatTime(row.createdAt)
  },
  deleteRecordColumn('search')
]

// ============ 数据加载 ============

const loadRecords = async (action) => {
  loading.value = true
  try {
    let page = 1
    if (action === 'upload') page = uploadPage.value
    else if (action === 'download') page = downloadPage.value
    else page = searchPage.value

    const res = await FileApi.recent(page, pageSize, action)
    if (res.success) {
      const records = res.data.records || []
      const total = res.data.total || 0

      if (action === 'upload') {
        uploadRecords.value = records
        uploadTotal.value = total
      } else if (action === 'download') {
        downloadRecords.value = records
        downloadTotal.value = total
      } else {
        searchRecords.value = records
        searchTotal.value = total
      }
    } else {
      ElMessage.error('加载记录失败：' + (res.message || '未知错误'))
    }
  } catch (e) {
    ElMessage.error('加载记录失败：' + e.message)
  } finally {
    loading.value = false
    nextTick(updateTableSize)
  }
}

const handleTabChange = (tab) => {
  activeTab.value = tab
  loadRecords(tab)
}

// ============ 单条删除 ============

const handleDeleteRecord = async (row, action) => {
  const actionLabels = { search: '搜索', upload: '上传', download: '下载' }
  const label = actionLabels[action] || action
  if (!window.confirm(`确定要删除该条${label}记录吗？此操作不可恢复。`)) {
    return
  }

  deletingRecordId.value = row.id
  try {
    const res = await AdminApi.deleteRecord(row.id, action)
    if (res.success) {
      ElMessage.success('记录已删除')
      loadRecords(action)
    } else {
      ElMessage.error('删除失败：' + (res.message || '未知错误'))
    }
  } catch (e) {
    ElMessage.error('删除失败：' + e.message)
  } finally {
    deletingRecordId.value = null
  }
}

// ============ 清空记录 ============

const handleClearRecords = async (action) => {
  const actionLabels = { search: '搜索', upload: '上传', download: '下载' }
  const label = actionLabels[action] || action
  if (!window.confirm(`确定要清空所有${label}记录吗？此操作不可恢复。`)) {
    return
  }

  clearingRecord.value = action
  try {
    const res = await AdminApi.clearRecords(action)
    if (res.success) {
      ElMessage.success(`已清空 ${res.data.count} 条${label}记录`)
      // 刷新当前标签页
      loadRecords(activeTab.value)
    } else {
      ElMessage.error('清空失败：' + (res.message || '未知错误'))
    }
  } catch (e) {
    ElMessage.error('清空失败：' + e.message)
  } finally {
    clearingRecord.value = null
  }
}

// ============ 初始化 ============

onMounted(() => {
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (uploadWrapRef.value) tableResizeObs.observe(uploadWrapRef.value)
  if (downloadWrapRef.value) tableResizeObs.observe(downloadWrapRef.value)
  if (searchWrapRef.value) tableResizeObs.observe(searchWrapRef.value)
  // 支持从看板等入口通过 ?tab=xxx 直接定位到对应 tab
  const tabQuery = route.query.tab
  const initialTab = ['upload', 'download', 'search'].includes(tabQuery) ? tabQuery : 'upload'
  activeTab.value = initialTab
  loadRecords(initialTab)
})

onUnmounted(() => {
  tableResizeObs?.disconnect()
})

watch(activeTab, () => {
  nextTick(updateTableSize)
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-records {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 16px;
    gap: 16px;
    flex-shrink: 0;

    .header-left {
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

    .header-right {
      display: flex;
      gap: 8px;
      flex-shrink: 0;
      padding-top: 4px;
    }
  }

  .records-tabs {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;

    // el-tabs 内部结构拉伸，使 body 高度撑满容器
    :deep(.el-tabs__header) {
      flex-shrink: 0;
      margin-bottom: 8px;
    }

    :deep(.el-tabs__content) {
      flex: 1 1 auto;
      min-height: 0;
    }

    :deep(.el-tab-pane) {
      height: 100%;
      display: flex;
      flex-direction: column;
    }

    .table-v2-wrap {
      flex: 1 1 auto;
      min-height: 0;
      position: relative;
      background: @bg-color;
      border-radius: @content-radius;
      box-shadow: @shadow-sm;
      overflow: hidden;
    }

    .pagination-wrap {
      flex-shrink: 0;
      display: flex;
      justify-content: center;
      padding: 8px 0 0;
    }
  }
}
</style>