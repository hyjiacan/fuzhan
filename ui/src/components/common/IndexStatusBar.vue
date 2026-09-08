<template>
  <div class="index-status-bar">
    <!-- 正在扫描 -->
    <el-tooltip v-if="status.isScanning">
      <template #default>
        <span class="status-item scanning">
          <span class="icon">⏳</span>
          正在扫描 {{ status.scanScope }}
          <span v-if="status.scanTotal > 0" class="progress">
            {{ status.scanProgress }} / {{ status.scanTotal }}
          </span>
        </span>
      </template>
      <template #content>点击「立即扫描」可重新触发</template>
    </el-tooltip>

    <!-- 空闲状态 -->
    <template v-else>
      <el-tooltip>
        <template #default>
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
        <template #content>
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
        </template>
      </el-tooltip>
    </template>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { IndexApi } from '@/api'
import { TimeUtils } from '@/utils'

const props = defineProps({
  // 是否在挂载时加载一次扫描状态（底部扫描时间不做自动轮询）
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
