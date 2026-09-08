<template>
  <div class="upload-task-table" v-loading="loading">
    <el-table
      :data="tasks"
      size="small"
      stripe
      max-height="400"
    >
      <el-table-column prop="fileName" label="文件名" width="200" show-overflow-tooltip />
      <el-table-column prop="_type" label="上传类型" width="100">
        <template #default="{ row }">
          <el-tag :type="elTagType(typeMap[row._type]?.type || 'default')" size="small">{{ typeMap[row._type]?.text || '-' }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="progress" label="进度" width="200">
        <template #default="{ row }">
          <el-progress
            v-if="getProgress(row) !== null"
            :percentage="getProgress(row)"
            :stroke-width="20"
            :text-inside="true"
            :status="row.status === 'completed' ? 'success' : undefined"
          />
          <span v-else>-</span>
        </template>
      </el-table-column>
      <el-table-column prop="status" label="状态" width="100">
        <template #default="{ row }">
          <el-tag :type="elTagType(statusMap[row.status]?.type || 'default')" size="small">{{ statusMap[row.status]?.text || row.status }}</el-tag>
        </template>
      </el-table-column>
      <el-table-column prop="fileSize" label="文件大小" width="100">
        <template #default="{ row }">{{ row.fileSize ? formatSize(row.fileSize) : '-' }}</template>
      </el-table-column>
      <el-table-column label="操作" width="180">
        <template #default="{ row }">
          <el-button v-if="['pending', 'downloading', 'uploading', 'in_progress'].includes(row.status)" size="small" type="warning" @click="handleCancel(row)">取消</el-button>
          <el-button v-if="['failed', 'cancelled'].includes(row.status)" size="small" type="primary" @click="handleRetry(row)">重试</el-button>
          <el-button v-if="['completed', 'cancelled', 'failed', 'expired'].includes(row.status)" size="small" type="danger" @click="handleDelete(row)">删除</el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="pagination.page"
      v-model:page-size="pagination.pageSize"
      :page-sizes="pagination.pageSizes"
      :total="tasks.length"
      layout="total, sizes, prev, pager, next, jumper"
      @current-change="loadData"
      @size-change="loadData"
    />
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { ElMessage } from 'element-plus'
import { UploadApi } from '../../api'

const props = defineProps({
  type: { type: String, required: true }
})

const loading = ref(false)
const tasks = ref([])
const pagination = ref({ page: 1, pageSize: 50, showSizePicker: true, pageSizes: [20, 50, 100] })

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

const typeMap = {
  session: { type: 'info', text: '本地上传' },
  url: { type: 'warning', text: 'URL 上传' },
}

const tagTypeMap = { info: 'info', warning: 'warning', success: 'success', error: 'danger', default: '' }
function elTagType(type) {
  return tagTypeMap[type] ?? ''
}

function getProgress(row) {
  const total = row.fileSize || 0
  const downloaded = row.downloadedBytes || row.uploadedBytes || 0
  if (total > 0) return Math.round(downloaded / total * 100)
  return null
}

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
    ElMessage.success('已取消')
    await loadData()
  } catch (e) {
    ElMessage.error('取消失败')
  }
}

async function handleRetry(row) {
  try {
    await UploadApi.retryURLTask(row.id)
    ElMessage.success('已重试')
    await loadData()
  } catch (e) {
    ElMessage.error('重试失败')
  }
}

async function handleDelete(row) {
  try {
    await UploadApi.deleteURLTask(row.id)
    ElMessage.success('已删除')
    await loadData()
  } catch (e) {
    ElMessage.error('删除失败')
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
    ElMessage.error('加载失败')
  } finally {
    loading.value = false
  }
  scheduleNextPoll()
}

// 是否有进行中的任务（上传/下载中、等待中）
function hasActiveTasks(list) {
  return list.some(row => ['pending', 'downloading', 'uploading', 'in_progress'].includes(row.status))
}

const POLL_INTERVAL = 60000 // 有进行中任务时 1 分钟轮询一次

let pollTimer = null
function stopPolling() {
  if (pollTimer) {
    clearTimeout(pollTimer)
    pollTimer = null
  }
}

// 仅当存在进行中的任务时，才安排下一次轮询；空闲时停止发请求
function scheduleNextPoll() {
  stopPolling()
  if (!hasActiveTasks(tasks.value)) return
  pollTimer = setTimeout(async () => {
    await loadData()
  }, POLL_INTERVAL)
}

onMounted(async () => {
  await loadData()
})

onUnmounted(() => {
  stopPolling()
})
</script>

<style scoped>
.upload-task-table {
  min-height: 200px;
}
</style>