<template>
  <div class="upload-task-table">
    <n-spin :show="loading">
      <n-data-table
        :columns="columns"
        :data="tasks"
        :pagination="pagination"
        :bordered="false"
        :single-line="false"
        size="small"
        :max-height="400"
      />
    </n-spin>
  </div>
</template>

<script setup>
import { h, ref, onMounted, onUnmounted } from 'vue'
import { NButton, NTag, NProgress, NSpace, useMessage } from 'naive-ui'
import { UploadApi } from '../../api'

const props = defineProps({
  type: { type: String, required: true }
})

const message = useMessage()
const loading = ref(false)
const tasks = ref([])
const pagination = ref({ page: 1, pageSize: 50, showSizePicker: true, pageSizes: [20, 50, 100] })
let pollTimer = null

const statusMap = {
  'in_progress': { type: 'info', text: '上传中' },
  'uploading': { type: 'info', text: '上传中' },
  'pending': { type: 'warning', text: '等待中' },
  'downloading': { type: 'info', text: '下载中' },
  'completed': { type: 'success', text: '已完成' },
  'failed': { type: 'error', text: '失败' },
  'cancelled': { type: 'default', text: '已取消' },
  'expired': { type: 'default', text: '已过期' },
}

const columns = [
  { title: '文件名', key: 'fileName', width: 200, ellipsis: { tooltip: true } },
  { title: '进度', key: 'progress', width: 200,
    render: (row) => {
      const total = row.fileSize || 0
      const downloaded = row.downloadedBytes || row.uploadedBytes || 0
      if (total > 0) {
        return h(NProgress, {
          type: 'line',
          status: row.status === 'completed' ? 'success' : 'info',
          percentage: Math.round(downloaded / total * 100),
          indicatorPlacement: 'inside',
          height: 20
        })
      }
      return '-'
    }
  },
  { title: '状态', key: 'status', width: 100,
    render: (row) => {
      const info = statusMap[row.status] || { type: 'default', text: row.status }
      return h(NTag, { type: info.type, size: 'small' }, { default: () => info.text })
    }
  },
  { title: '文件大小', key: 'fileSize', width: 100,
    render: (row) => row.fileSize ? formatSize(row.fileSize) : '-'
  },
  { title: '操作', key: 'actions', width: 180,
    render: (row) => {
      const btns = []
      if (['pending', 'downloading', 'uploading', 'in_progress'].includes(row.status)) {
        btns.push(h(NButton, { size: 'tiny', type: 'warning', onClick: () => handleCancel(row) }, { default: () => '取消' }))
      }
      if (['failed', 'cancelled'].includes(row.status)) {
        btns.push(h(NButton, { size: 'tiny', type: 'primary', onClick: () => handleRetry(row) }, { default: () => '重试' }))
      }
      if (['completed', 'cancelled', 'failed', 'expired'].includes(row.status)) {
        btns.push(h(NButton, { size: 'tiny', type: 'error', onClick: () => handleDelete(row) }, { default: () => '删除' }))
      }
      return h(NSpace, {}, { default: () => btns })
    }
  }
]

function formatSize(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let size = bytes
  while (size >= 1024 && i < units.length - 1) { size /= 1024; i++ }
  return `${size.toFixed(1)} ${units[i]}`
}

// 判断是否为 session（id 为数字的是 session，UUID 字符串的是 url task）
function isSession(row) {
  return typeof row.id === 'number'
}

async function handleCancel(row) {
  try {
    if (isSession(row)) {
      await UploadApi.session.cancel(row.id)
    } else {
      await UploadApi.cancelURLTask(row.id)
    }
    message.success('已取消')
    await loadData()
  } catch (e) {
    message.error('取消失败')
  }
}

async function handleRetry(row) {
  try {
    await UploadApi.retryURLTask(row.id)
    message.success('已重试')
    await loadData()
  } catch (e) {
    message.error('重试失败')
  }
}

async function handleDelete(row) {
  try {
    await UploadApi.deleteURLTask(row.id)
    message.success('已删除')
    await loadData()
  } catch (e) {
    message.error('删除失败')
  }
}

async function loadData() {
  loading.value = true
  try {
    const [sessRes, urlRes] = await Promise.all([
      UploadApi.listSessions(props.type, pagination.value.page, pagination.value.pageSize),
      UploadApi.listURLTasks(props.type, pagination.value.page, pagination.value.pageSize)
    ])
    const sessions = (sessRes?.data?.sessions || []).map(s => ({ ...s, _type: 'session' }))
    const urlTasks = (urlRes?.data?.tasks || []).map(t => ({ ...t, _type: 'url' }))
    tasks.value = [...sessions, ...urlTasks].sort((a, b) => new Date(b.createdAt) - new Date(a.createdAt))
  } catch (e) {
    message.error('加载失败')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  loadData()
  pollTimer = setInterval(loadData, 2000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
.upload-task-table {
  min-height: 200px;
}
</style>