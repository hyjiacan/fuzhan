<template>
  <div class="admin-records">
    <div class="header-section">
      <div class="header-left">
        <h2>记录管理</h2>
        <p class="description">查看公开文件的操作记录</p>
      </div>
      <div class="header-right">
        <n-button
          type="warning"
          secondary
          size="small"
          :loading="clearingRecord === 'search'"
          :disabled="clearingRecord !== null"
          @click="handleClearRecords('search')"
        >
          <template #icon>
            <n-icon><DeleteIcon /></n-icon>
          </template>
          清空搜索
        </n-button>
        <n-button
          type="error"
          secondary
          size="small"
          :loading="clearingRecord === 'upload'"
          :disabled="clearingRecord !== null"
          @click="handleClearRecords('upload')"
        >
          <template #icon>
            <n-icon><DeleteIcon /></n-icon>
          </template>
          清空上传
        </n-button>
        <n-button
          type="error"
          secondary
          size="small"
          :loading="clearingRecord === 'download'"
          :disabled="clearingRecord !== null"
          @click="handleClearRecords('download')"
        >
          <template #icon>
            <n-icon><DeleteIcon /></n-icon>
          </template>
          清空下载
        </n-button>
      </div>
    </div>

    <n-tabs
      type="line"
      :value="activeTab"
      @update:value="handleTabChange"
      class="records-tabs"
    >
      <n-tab-pane name="upload" tab="最近上传">
        <n-data-table
          :columns="uploadColumns"
          :data="uploadRecords"
          :loading="loading"
          :bordered="false"
          :single-line="true"
          striped
          size="small"
          :row-key="(row) => row.id"
          class="records-table"
        />
        <div class="pagination-wrap" v-if="uploadTotal > 0">
          <n-pagination
            :page="uploadPage"
            :page-size="pageSize"
            :item-count="uploadTotal"
            @update:page="(p) => { uploadPage = p; loadRecords('upload') }"
          />
        </div>
      </n-tab-pane>

      <n-tab-pane name="download" tab="最近下载">
        <n-data-table
          :columns="downloadColumns"
          :data="downloadRecords"
          :loading="loading"
          :bordered="false"
          :single-line="true"
          striped
          size="small"
          :row-key="(row) => row.id"
          class="records-table"
        />
        <div class="pagination-wrap" v-if="downloadTotal > 0">
          <n-pagination
            :page="downloadPage"
            :page-size="pageSize"
            :item-count="downloadTotal"
            @update:page="(p) => { downloadPage = p; loadRecords('download') }"
          />
        </div>
      </n-tab-pane>

      <n-tab-pane name="search" tab="最近搜索">
        <n-data-table
          :columns="searchColumns"
          :data="searchRecords"
          :loading="loading"
          :bordered="false"
          :single-line="true"
          striped
          size="small"
          :row-key="(row) => row.id"
          class="records-table"
        />
        <div class="pagination-wrap" v-if="searchTotal > 0">
          <n-pagination
            :page="searchPage"
            :page-size="pageSize"
            :item-count="searchTotal"
            @update:page="(p) => { searchPage = p; loadRecords('search') }"
          />
        </div>
      </n-tab-pane>
    </n-tabs>
  </div>
</template>

<script setup>
import { ref, h, onMounted } from 'vue'
import {
  NTabs, NTabPane, NDataTable, NPagination, NButton, NIcon, useMessage
} from 'naive-ui'
import { FileApi, AdminApi } from '@/api'
import { NumberUtils } from '@/utils'

const message = useMessage()

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

// ============ 图标 ============

const DeleteIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M6 19c0 1.1.9 2 2 2h8c1.1 0 2-.9 2-2V7H6v12zM19 4h-3.5l-1-1h-5l-1 1H5v2h14V4z' })
])

// ============ 表格列定义 ============

const formatSize = (bytes) => NumberUtils.formatFileSize(bytes || 0)

const formatTime = (time) => {
  if (!time) return ''
  const d = new Date(time)
  return d.toLocaleString('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit'
  })
}

const uploadColumns = [
  { title: '文件路径', key: 'fullPath', ellipsis: { tooltip: true }, width: 400 },
  { title: '大小', key: 'fileSize', width: 100,
    render: (row) => formatSize(row.fileSize)
  },
  { title: 'IP地址', key: 'clientIP', width: 140, ellipsis: { tooltip: true } },
  { title: '上传时间', key: 'uploadTime', width: 170,
    render: (row) => formatTime(row.uploadTime || row.createdAt)
  }
]

const downloadColumns = [
  { title: '文件路径', key: 'fullPath', ellipsis: { tooltip: true }, width: 400 },
  { title: '大小', key: 'fileSize', width: 100,
    render: (row) => formatSize(row.fileSize)
  },
  { title: 'IP地址', key: 'clientIP', width: 140, ellipsis: { tooltip: true } },
  { title: '下载时间', key: 'uploadTime', width: 170,
    render: (row) => formatTime(row.uploadTime || row.createdAt)
  }
]

const searchColumns = [
  { title: '搜索关键词', key: 'searchQuery', ellipsis: { tooltip: true }, width: 300 },
  { title: 'IP地址', key: 'clientIP', width: 140, ellipsis: { tooltip: true } },
  { title: '搜索时间', key: 'uploadTime', width: 170,
    render: (row) => formatTime(row.uploadTime || row.createdAt)
  }
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
      message.error('加载记录失败：' + (res.message || '未知错误'))
    }
  } catch (e) {
    message.error('加载记录失败：' + e.message)
  } finally {
    loading.value = false
  }
}

const handleTabChange = (tab) => {
  activeTab.value = tab
  loadRecords(tab)
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
      message.success(`已清空 ${res.data.count} 条${label}记录`)
      // 刷新当前标签页
      loadRecords(activeTab.value)
    } else {
      message.error('清空失败：' + (res.message || '未知错误'))
    }
  } catch (e) {
    message.error('清空失败：' + e.message)
  } finally {
    clearingRecord.value = null
  }
}

// ============ 初始化 ============

onMounted(() => {
  loadRecords('upload')
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-records {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    margin-bottom: 16px;
    gap: 16px;

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
    .records-table {
      margin-bottom: 16px;
    }

    .pagination-wrap {
      display: flex;
      justify-content: center;
      padding: 8px 0 16px;
    }
  }
}
</style>