<template>
  <div class="upload-manager-bar" v-if="activeCount > 0">
    <n-button text @click="showDialog = true" class="manager-btn">
      <template #icon>
        <n-icon><UploadIcon /></n-icon>
      </template>
      上传管理
      <n-badge :value="activeCount" :max="99" class="badge" />
    </n-button>
    <UploadManagerDialog v-model:show="showDialog" />
  </div>
</template>

<script setup>
import { ref, h, onMounted, onUnmounted } from 'vue'
import { UploadApi } from '../../api'
import UploadManagerDialog from './UploadManagerDialog.vue'

const showDialog = ref(false)
const activeCount = ref(0)

let pollTimer = null

const UploadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 16, height: 16 }, [
  h('path', { d: 'M9 16h6v-6h4l-7-7-7 7h4zm-4 2h14v2H5z' })
])

async function refreshCount() {
  try {
    const [sessRes, urlRes] = await Promise.all([
      UploadApi.listSessions('public', 1, 1000),
      UploadApi.listURLTasks('public', 1, 1000)
    ])
    const sessions = sessRes?.data?.sessions || []
    const tasks = urlRes?.data?.tasks || []
    const activeSessions = sessions.filter(s => s.status === 'in_progress' || s.status === 'uploading').length
    const activeTasks = tasks.filter(t => t.status === 'pending' || t.status === 'downloading').length
    activeCount.value = activeSessions + activeTasks
  } catch {
    // 静默失败
  }
}

onMounted(() => {
  refreshCount()
  pollTimer = setInterval(refreshCount, 5000)
})

onUnmounted(() => {
  if (pollTimer) clearInterval(pollTimer)
})
</script>

<style scoped>
.upload-manager-bar {
  position: fixed;
  bottom: 0;
  right: 20px;
  z-index: 1000;
  background: var(--bar-bg, #fff);
  border: 1px solid var(--border-color, #e0e0e0);
  border-bottom: none;
  border-radius: 8px 8px 0 0;
  padding: 8px 16px;
  box-shadow: 0 -2px 8px rgba(0,0,0,0.1);
}
.manager-btn {
  display: flex;
  align-items: center;
  gap: 8px;
}
.badge {
  margin-left: 4px;
}
</style>