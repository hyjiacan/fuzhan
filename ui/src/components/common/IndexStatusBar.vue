<template>
  <div class="index-status-bar">
    <!-- 正在扫描 -->
    <n-tooltip v-if="status.isScanning">
      <template #trigger>
        <span class="status-item scanning">
          <span class="icon">⏳</span>
          正在扫描 {{ status.scanScope }}
          <span v-if="status.scanTotal > 0" class="progress">
            {{ status.scanProgress }} / {{ status.scanTotal }}
          </span>
        </span>
      </template>
      点击「立即扫描」可重新触发
    </n-tooltip>

    <!-- 空闲状态 -->
    <template v-else>
      <n-tooltip>
        <template #trigger>
          <span class="status-item idle">
            <span class="icon">📋</span>
            索引:
            <span v-if="status.lastScanTime" class="time">
              上次 {{ formatTime(status.lastScanTime) }}
            </span>
            <span v-else class="time">尚未扫描</span>
            <span v-if="status.nextScanTime" class="time next">
              · 下次 {{ formatTime(status.nextScanTime) }}
            </span>
            <span v-if="!status.scanCronExpression" class="time warn">
              · 定时扫描未配置
            </span>
          </span>
        </template>
        <div>
          <div v-if="status.scanCronExpression">
            Cron: {{ status.scanCronExpression }}
          </div>
          <div v-else>定时扫描未配置</div>
          <div>
            <a class="tooltip-link" href="javascript:void(0)" @click="$emit('triggerScan')">
              点击立即扫描
            </a>
          </div>
        </div>
      </n-tooltip>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted, onUnmounted } from 'vue'
import { NTooltip } from 'naive-ui'
import { IndexApi } from '@/api'

const props = defineProps({
  // 是否自动轮询（仅 HomeView 等需要显示状态的页面）
  autoPoll: {
    type: Boolean,
    default: true
  }
})

defineEmits(['triggerScan'])

const status = ref({
  lastScanTime: null,
  nextScanTime: null,
  isScanning: false,
  scanScope: '',
  scanProgress: 0,
  scanTotal: 0,
  scanCronExpression: '',
})

let pollTimer = null

const fetchStatus = async () => {
  try {
    const res = await IndexApi.getScanStatus()
    if (res && res.data) {
      status.value = res.data
    }
  } catch (e) {
    // 静默失败，Footer 不弹错误
  }
}

const formatTime = (t) => {
  if (!t) return '-'
  const d = new Date(t)
  const pad = (n) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

onMounted(() => {
  if (props.autoPoll) {
    fetchStatus()
    pollTimer = setInterval(fetchStatus, 10000) // 每 10 秒轮询
  }
})

onUnmounted(() => {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.index-status-bar {
  display: flex;
  align-items: center;

  .status-item {
    font-size: 12px;
    color: @text-color-secondary;
    display: flex;
    align-items: center;
    gap: 4px;

    .icon {
      font-size: 13px;
    }

    .time {
      &.next {
        color: @text-color-disabled;
      }

      &.warn {
        color: #e6a23c;
      }
    }

    .progress {
      color: @primary-color;
      font-variant-numeric: tabular-nums;
    }

    &.scanning {
      color: @primary-color;
    }

    &.idle {
      &:hover {
        color: @text-color;
      }
    }
  }

  .tooltip-link {
    color: @primary-color;
    text-decoration: none;
    cursor: pointer;

    &:hover {
      text-decoration: underline;
    }
  }
}
</style>
