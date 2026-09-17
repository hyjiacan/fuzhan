<template>
  <div class="index-status-bar">
    <!-- 正在扫描 -->
    <el-tooltip v-if="status.isScanning">
      <template #default>
        <span class="status-item scanning">
          <el-icon class="icon is-spinning"><Loading /></el-icon>
          正在扫描 {{ status.scanScope }}
          <span v-if="status.scanTotal > 0" class="progress">
            {{ status.scanProgress }} / {{ status.scanTotal }}
          </span>
        </span>
      </template>
      <template #content>索引扫描进行中，请勿在页面上重复触发</template>
    </el-tooltip>

    <!-- 空闲状态 -->
    <template v-else>
      <span class="status-item idle">
        <el-icon class="icon"><FolderChecked /></el-icon>
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
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { Loading, FolderChecked } from '@element-plus/icons-vue'
import { IndexApi } from '@/api'
import { TimeUtils } from '@/utils'

const props = defineProps({
  // 是否在挂载时加载一次扫描状态（底部扫描时间不做自动轮询）
  autoPoll: {
    type: Boolean,
    default: true
  }
})

const status = ref({
  lastScanTime: null,
  nextScanTime: null,
  isScanning: false,
  scanScope: '',
  scanProgress: 0,
  scanTotal: 0,
})

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

const formatTime = (t) => TimeUtils.formatDateTime(t)

onMounted(() => {
  // 底部扫描时间不需要自动轮询更新，仅在进入页面时加载一次
  if (props.autoPoll) {
    fetchStatus()
  }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.index-status-bar {
  display: flex;
  align-items: center;

  @keyframes index-status-spin {
    from {
      transform: rotate(0deg);
    }
    to {
      transform: rotate(360deg);
    }
  }

  .status-item {
    font-size: 12px;
    color: @text-color-secondary;
    display: flex;
    align-items: center;
    gap: 4px;

    .icon {
      font-size: 13px;
      flex-shrink: 0;

      &.is-spinning {
        animation: index-status-spin 1s linear infinite;
        color: @primary-accent;
      }
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
}
</style>
