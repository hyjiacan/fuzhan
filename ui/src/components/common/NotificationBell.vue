<template>
  <div class="notification-bell" ref="bellRef">
    <n-badge :value="unreadCount" :max="99" :show="unreadCount > 0">
      <n-button
        quaternary
        size="small"
        class="bell-btn"
        :class="{ 'bell-has-new': unreadCount > 0 }"
        @click="toggleDropdown"
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
          <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"></path>
          <path d="M13.73 21a2 2 0 0 1-3.46 0"></path>
        </svg>
      </n-button>
    </n-badge>

    <transition name="dropdown">
      <div v-if="showDropdown" class="notification-dropdown" @click.stop>
        <div class="dropdown-header">
          <span class="dropdown-title">下载通知</span>
          <n-button
            v-if="unreadCount > 0"
            text
            size="tiny"
            @click="markAllRead"
          >
            全部标记已读
          </n-button>
        </div>

        <div class="dropdown-body">
          <div v-if="notifications.length === 0" class="empty-state">
            暂无通知
          </div>

          <div
            v-for="item in notifications"
            :key="item.id"
            class="notification-item"
            :class="{ 'is-failed': item.status === 'failed' }"
          >
            <div class="item-main" @click="item.status === 'failed' && toggleExpand(item.id)">
              <span class="status-icon" :class="item.status === 'completed' ? 'success' : 'failed'">
                {{ item.status === 'completed' ? '✓' : '✗' }}
              </span>
              <div class="item-info">
                <span class="item-filename" :title="item.fileName">{{ item.fileName }}</span>
                <span class="item-time">{{ formatTime(item.completedAt || item.updatedAt) }}</span>
              </div>
              <span v-if="item.status === 'failed'" class="expand-icon">
                {{ expandedId === item.id ? '▲' : '▼' }}
              </span>
              <button class="item-dismiss" @click.stop="dismiss(item.id)" title="关闭通知">×</button>
            </div>

            <transition name="slide">
              <div v-if="expandedId === item.id && item.errorMessage" class="error-detail">
                {{ item.errorMessage }}
              </div>
            </transition>
          </div>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { NBadge, NButton, useMessage } from 'naive-ui'
import { NotificationApi } from '@/api'
import store from '@/store'

const message = useMessage()
const bellRef = ref(null)
const showDropdown = ref(false)
const expandedId = ref(null)

const notifications = computed(() => store.state.notifications || [])
const unreadCount = computed(() => notifications.value.length)

// 新通知到达时提示（下拉打开时不打扰）
watch(unreadCount, (newCount, oldCount) => {
  if (oldCount > 0 && newCount > oldCount && !showDropdown.value) {
    const added = newCount - oldCount
    const newItems = notifications.value.slice(-added)
    for (const item of newItems) {
      if (item.status === 'completed') {
        message.success(`${item.fileName} 下载完成`)
      } else if (item.status === 'failed') {
        message.warning(`${item.fileName} 下载失败`)
      }
    }
  }
})

function toggleDropdown() {
  showDropdown.value = !showDropdown.value
  if (showDropdown.value) {
    refreshNotifications()
  } else {
    expandedId.value = null
  }
}

function toggleExpand(id) {
  expandedId.value = expandedId.value === id ? null : id
}

async function dismiss(id) {
  try {
    const res = await NotificationApi.markRead([id])
    if (res.success) {
      store.removeNotification(id)
      if (notifications.value.length === 0) {
        showDropdown.value = false
      }
    }
  } catch (e) {
    // 静默处理
  }
}

async function refreshNotifications() {
  try {
    const res = await NotificationApi.getNotifications()
    if (res.success && res.data) {
      store.setNotifications(res.data)
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
      showDropdown.value = false
      expandedId.value = null
    }
  } catch (e) {
    // 静默处理
  }
}

function formatTime(timeStr) {
  if (!timeStr) return ''
  const d = new Date(timeStr)
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function handleClickOutside(e) {
  if (bellRef.value && !bellRef.value.contains(e.target)) {
    showDropdown.value = false
    expandedId.value = null
  }
}

onMounted(() => {
  document.addEventListener('click', handleClickOutside)
})

onUnmounted(() => {
  document.removeEventListener('click', handleClickOutside)
})
</script>

<style scoped>
.notification-bell {
  position: relative;
  display: inline-flex;
  align-items: center;
}

.bell-btn {
  color: rgba(255, 255, 255, 0.7);
}
.bell-btn:hover {
  color: #fff;
}
.bell-has-new {
  animation: bellPulse 2s ease-in-out infinite;
}

@keyframes bellPulse {
  0%, 100% { transform: scale(1); }
  50% { transform: scale(1.15); }
}

.notification-dropdown {
  position: absolute;
  top: calc(100% + 8px);
  right: 0;
  width: 340px;
  max-height: 420px;
  background: #252525;
  border: 1px solid rgba(255, 255, 255, 0.1);
  border-radius: 8px;
  box-shadow: 0 4px 20px rgba(0, 0, 0, 0.4);
  z-index: 2000;
  display: flex;
  flex-direction: column;
}

.dropdown-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.08);
}
.dropdown-title {
  font-size: 14px;
  font-weight: 600;
  color: #fff;
}

.dropdown-body {
  overflow-y: auto;
  max-height: 360px;
}

.empty-state {
  padding: 40px 16px;
  text-align: center;
  color: rgba(255, 255, 255, 0.4);
  font-size: 13px;
}

.notification-item {
  border-bottom: 1px solid rgba(255, 255, 255, 0.05);
}
.notification-item:last-child {
  border-bottom: none;
}

.item-main {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 16px;
  cursor: default;
}
.is-failed .item-main {
  cursor: pointer;
}
.item-main:hover {
  background: rgba(255, 255, 255, 0.05);
}

.status-icon {
  flex-shrink: 0;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: bold;
}
.status-icon.success {
  background: rgba(82, 196, 26, 0.2);
  color: #52c41a;
}
.status-icon.failed {
  background: rgba(255, 77, 79, 0.2);
  color: #ff4d4f;
}

.item-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.item-filename {
  font-size: 13px;
  color: rgba(255, 255, 255, 0.85);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.item-time {
  font-size: 11px;
  color: rgba(255, 255, 255, 0.4);
}

.expand-icon {
  font-size: 10px;
  color: rgba(255, 255, 255, 0.3);
  flex-shrink: 0;
}

.item-dismiss {
  background: none;
  border: none;
  color: rgba(255, 255, 255, 0.25);
  font-size: 14px;
  cursor: pointer;
  padding: 0 2px;
  line-height: 1;
  flex-shrink: 0;
}
.item-dismiss:hover {
  color: rgba(255, 255, 255, 0.6);
}

.error-detail {
  padding: 0 16px 10px 46px;
  font-size: 12px;
  color: #ff7875;
  line-height: 1.5;
  word-break: break-all;
}

/* Transitions */
.dropdown-enter-active,
.dropdown-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.dropdown-enter-from,
.dropdown-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}

.slide-enter-active,
.slide-leave-active {
  transition: all 0.2s ease;
}
.slide-enter-from,
.slide-leave-to {
  opacity: 0;
  max-height: 0;
}
</style>
