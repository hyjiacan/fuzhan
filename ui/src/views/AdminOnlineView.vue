<template>
  <div class="admin-online">
    <div class="header-section">
      <div class="header-left">
        <h2>在线IP</h2>
        <p class="description">
          最近 {{ idleTimeoutSeconds }} 秒内有请求的客户端 IP（与登录状态无关）
        </p>
      </div>
      <div class="header-right">
        <span class="online-summary">
          当前在线 <b>{{ onlineList.length }}</b> 个 IP
        </span>
        <el-button type="primary" size="small" :loading="loading" @click="loadData">
          <el-icon><Refresh /></el-icon>
          刷新
        </el-button>
      </div>
    </div>

    <el-alert
      type="info"
      :closable="false"
      class="idle-tip"
      title="判定规则：某 IP 在空闲超时内发出过任一元请求即视为在线，距上次请求超过该时长自动离线。"
    />

    <div ref="wrapRef" class="table-v2-wrap" v-loading="loading">
      <el-table-v2
        v-if="onlineList.length > 0"
        :columns="columns"
        :data="onlineList"
        :width="tableWidth"
        :height="tableHeight"
        :row-height="32"
        row-key="ip"
      />
      <el-empty v-else-if="!loading" description="当前没有在线 IP" :image-size="80" />
    </div>
  </div>
</template>

<script setup>
import { ref, h, onMounted, onUnmounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { AdminApi } from '@/api'

// ============ 状态 ============
const loading = ref(false)
const onlineList = ref([])
const idleTimeoutSeconds = ref(180)

// ============ 列 ============
const formatDuration = (seconds) => {
  if (!seconds && seconds !== 0) return '-'
  const s = Math.max(0, seconds)
  if (s < 60) return `${s} 秒`
  const m = Math.floor(s / 60)
  const rs = s % 60
  if (m < 60) return `${m} 分 ${rs} 秒`
  const hh = Math.floor(m / 60)
  return `${hh} 小时 ${m % 60} 分`
}

const formatTime = (iso) => {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

const columns = [
  {
    title: 'IP 地址', key: 'ip', width: 200,
    cellRenderer: ({ rowData: row }) => h('span', { style: 'font-family: monospace; font-weight: 500;' }, row.ip)
  },
  {
    title: '在线时长', key: 'onlineSeconds', width: 140,
    sortable: true, sortBy: 'onlineSeconds',
    cellRenderer: ({ rowData: row }) => formatDuration(row.onlineSeconds)
  },
  {
    title: '上次请求', key: 'lastSeen', width: 220,
    cellRenderer: ({ rowData: row }) => formatTime(row.lastSeen)
  },
  {
    title: '首次请求', key: 'firstSeen', width: 220,
    cellRenderer: ({ rowData: row }) => formatTime(row.firstSeen)
  },
  {
    title: '请求次数', key: 'requestCount', width: 110,
    sortable: true, sortBy: 'requestCount',
    cellRenderer: ({ rowData: row }) => row.requestCount || 0
  },
  {
    title: 'User-Agent', key: 'userAgent', width: 320,
    cellRenderer: ({ rowData: row }) => h('span', {
      style: 'display: block; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;',
      title: row.userAgent || ''
    }, row.userAgent || '-')
  }
]

// ============ el-table-v2 尺寸测量 ============
const wrapRef = ref(null)
const tableWidth = ref(800)
const tableHeight = ref(400)
let tableResizeObs = null
const updateTableSize = () => {
  const el = wrapRef.value
  if (el) {
    tableWidth.value = el.clientWidth || 800
    tableHeight.value = el.clientHeight || 400
  }
}

// ============ 数据加载 ============
const loadData = async () => {
  loading.value = true
  try {
    const res = await AdminApi.getOnlineIps()
    if (res.success && res.data) {
      onlineList.value = Array.isArray(res.data.online) ? res.data.online : []
      if (res.data.idleTimeoutSeconds > 0) {
        idleTimeoutSeconds.value = res.data.idleTimeoutSeconds
      }
    }
  } catch (e) {
    onlineList.value = []
  } finally {
    loading.value = false
  }
}

// 手动刷新：进入页面时加载一次，之后由用户点击「刷新」按钮触发（不再自动轮询）
onMounted(async () => {
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (wrapRef.value) {
    tableResizeObs.observe(wrapRef.value)
  }
  await loadData()
})

onUnmounted(() => {
  tableResizeObs?.disconnect()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-online {
  display: flex;
  flex-direction: column;
  height: 100%;
  padding: @container-padding;

  .header-section {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 16px;

    .header-left {
      h2 {
        margin: 0;
        font-size: @font-size-lg;
      }

      .description {
        margin: 4px 0 0;
        color: #999;
        font-size: @font-size-sm;
      }
    }

    .header-right {
      display: flex;
      align-items: center;
      gap: 12px;

      .online-summary {
        font-size: 14px;
        color: #666;

        b {
          color: @primary-color;
          font-size: 16px;
        }
      }
    }
  }

  .idle-tip {
    margin-bottom: 12px;
  }

  .table-v2-wrap {
    flex: 1 1 auto;
    min-height: 0;
    background: #fff;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    overflow: hidden;
    position: relative;
  }
}
</style>