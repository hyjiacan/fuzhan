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
          当前在线 <b>{{ total }}</b> 个 IP
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

    <div ref="wrapRef" class="table-wrap" v-loading="loading">
      <el-table
        :data="onlineList"
        size="small"
        :border="false"
        stripe
        :max-height="tableHeight"
      >
        <el-table-column label="IP 地址" min-width="180">
          <template #default="{ row }">
            <span style="font-family: monospace; font-weight: 500;">{{ row.ip }}</span>
          </template>
        </el-table-column>
        <el-table-column label="在线时长" width="150" sortable prop="onlineSeconds">
          <template #default="{ row }">{{ formatDuration(row.onlineSeconds) }}</template>
        </el-table-column>
        <el-table-column label="上次请求" width="220">
          <template #default="{ row }">{{ formatTime(row.lastSeen) }}</template>
        </el-table-column>
        <el-table-column label="首次请求" width="220">
          <template #default="{ row }">{{ formatTime(row.firstSeen) }}</template>
        </el-table-column>
        <el-table-column label="请求次数" width="120" sortable prop="requestCount">
          <template #default="{ row }">{{ row.requestCount || 0 }}</template>
        </el-table-column>
        <el-table-column label="User-Agent" min-width="300">
          <template #default="{ row }">
            <span class="ua-cell" :title="row.userAgent || ''">{{ row.userAgent || '-' }}</span>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-if="!loading && onlineList.length === 0" description="当前没有在线 IP" :image-size="80" />
      <div class="pagination-wrap" v-if="total > 0">
        <el-pagination
          v-model:current-page="page"
          v-model:page-size="pageSize"
          :total="total"
          :page-sizes="[10, 20, 50, 100]"
          layout="total, sizes, prev, pager, next"
          @current-change="loadData"
          @size-change="onPageSizeChange"
        />
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { Refresh } from '@element-plus/icons-vue'
import { AdminApi } from '@/api'

// ============ 状态 ============
const loading = ref(false)
const onlineList = ref([])
const idleTimeoutSeconds = ref(180)
const page = ref(1)
const pageSize = ref(20)
const total = ref(0)

// 普通 el-table 需要通过容器测量提供 max-height，实现受控的高度内滚动
const wrapRef = ref(null)
const tableHeight = ref(400)
let tableResizeObs = null
const updateTableSize = () => {
  const el = wrapRef.value
  if (el) {
    const height = el.clientHeight || 400
    // 预留分页高度，避免表格把分页挤出可视区
    tableHeight.value = height > 60 ? height - 61 : height
  }
}

// ============ 格式化 ============
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

// ============ 数据加载 ============
const loadData = async () => {
  loading.value = true
  try {
    const res = await AdminApi.getOnlineIps(page.value, pageSize.value)
    if (res.success && res.data) {
      onlineList.value = Array.isArray(res.data.online) ? res.data.online : []
      total.value = res.data.total || 0
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

const onPageSizeChange = () => {
  page.value = 1
  loadData()
}

// 手动刷新：进入页面时加载一次，之后由用户点击「刷新」按钮触发（不再自动轮询）
onMounted(() => {
  updateTableSize()
  tableResizeObs = new ResizeObserver(updateTableSize)
  if (wrapRef.value) {
    tableResizeObs.observe(wrapRef.value)
  }
  loadData()
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
        color: @text-color-placeholder;
        font-size: @font-size-sm;
      }
    }

    .header-right {
      display: flex;
      align-items: center;
      gap: 12px;

      .online-summary {
        font-size: 14px;
        color: @text-color-secondary;

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

  .table-wrap {
    flex: 1 1 auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    background: @bg-color;
    border-radius: @content-radius;
    box-shadow: @shadow-sm;
    overflow: hidden;
    position: relative;

    :deep(.el-table) {
      flex: 1 1 auto;
      min-height: 0;
    }

    .ua-cell {
      display: block;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }

    .pagination-wrap {
      flex-shrink: 0;
      display: flex;
      justify-content: flex-end;
      padding: 12px 16px;
      border-top: 1px solid @border-color-light;
    }
  }
}
</style>