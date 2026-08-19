<template>
  <div class="download-notification-tab">
    <n-spin :show="loading">
      <div class="toolbar">
        <n-button v-if="notifications.length > 0" size="tiny" quaternary @click="markAllRead">
          全部标记已读
        </n-button>
        <span class="count-info">共 {{ notifications.length }} 条通知</span>
      </div>
      <n-data-table
        :columns="columns"
        :data="notifications"
        :pagination="pagination"
        :bordered="false"
        :single-line="false"
        size="small"
        :max-height="360"
      />
      <div v-if="!loading && notifications.length === 0" class="empty-state">
        <n-empty description="暂无通知" />
      </div>
    </n-spin>
  </div>
</template>

<script setup>
import { ref, onMounted, h } from 'vue'
import { NButton, NTag, NSpin, NDataTable, NEmpty, useMessage } from 'naive-ui'
import { NotificationApi } from '@/api'
import store from '@/store'

const message = useMessage()
const loading = ref(false)
const notifications = ref([])
const pagination = ref({ page: 1, pageSize: 50, showSizePicker: true, pageSizes: [20, 50, 100] })

const columns = [
  { title: '文件名', key: 'fileName', width: 200, ellipsis: { tooltip: true } },
  {
    title: '状态', key: 'status', width: 100,
    render: (row) => {
      const statusMap = {
        'completed': { type: 'success', text: '已完成' },
        'failed': { type: 'error', text: '失败' },
      }
      const info = statusMap[row.status] || { type: 'default', text: row.status }
      return h(NTag, { type: info.type, size: 'small' }, { default: () => info.text })
    }
  },
  {
    title: '完成时间', key: 'completedAt', width: 170,
    render: (row) => formatTime(row.completedAt || row.updatedAt)
  },
  {
    title: '操作', key: 'actions', width: 80,
    render: (row) => {
      return h(NButton, {
        size: 'tiny',
        type: 'error',
        quaternary: true,
        onClick: () => dismiss(row.id)
      }, { default: () => '关闭' })
    }
  }
]

function formatTime(timeStr) {
  if (!timeStr) return ''
  const d = new Date(timeStr)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

async function loadNotifications() {
  loading.value = true
  try {
    const res = await NotificationApi.getNotifications()
    if (res.success && res.data) {
      notifications.value = res.data
    }
  } catch (e) {
    // 静默处理
  } finally {
    loading.value = false
  }
}

async function dismiss(id) {
  try {
    const res = await NotificationApi.markRead([id])
    if (res.success) {
      store.removeNotification(id)
      notifications.value = notifications.value.filter(n => n.id !== id)
    }
  } catch (e) {
    // 静默处理
  }
}

async function markAllRead() {
  const ids = notifications.value.map(n => n.id)
  if (ids.length === 0) return
  try {
    const res = await NotificationApi.markRead(ids)
    if (res.success) {
      store.clearNotifications()
      notifications.value = []
      message.success('已全部标记已读')
    }
  } catch (e) {
    // 静默处理
  }
}

onMounted(() => {
  loadNotifications()
})
</script>

<style scoped>
.download-notification-tab {
  min-height: 200px;
}

.toolbar {
  display: flex;
  justify-content: flex-end;
  align-items: center;
  gap: 12px;
  margin-bottom: 12px;
}

.count-info {
  font-size: 12px;
  color: rgba(255, 255, 255, 0.5);
}

.empty-state {
  display: flex;
  justify-content: center;
  padding: 40px 0;
}
</style>