<template>
  <div class="user-storage-section">
    <div class="header-section">
      <h2>存储空间</h2>
      <p class="description">查看您的存储使用情况</p>
    </div>

    <n-grid :cols="2" :x-gap="24" :y-gap="24" responsive="screen" :item-responsive="true">
      <!-- Private storage quota -->
      <n-gi :span="2" :md="1">
        <n-card title="私有存储" class="quota-card">
          <n-progress
            type="line"
            :percentage="quotaPercentage"
            :indicator-placement-inside="true"
            :status="quotaStatus"
          >
            {{ formatSize(quotaInfo.used) }} / {{ formatSize(quotaInfo.quota) }}
          </n-progress>
          <template #footer>
            <div class="quota-hint">已存储 {{ fileCount }} 个文件</div>
          </template>
        </n-card>
      </n-gi>

      <!-- Temp file quota -->
      <n-gi :span="2" :md="1">
        <n-card title="临时文件配额" class="quota-card">
          <n-progress
            type="line"
            :percentage="tempQuotaPercentage"
            :indicator-placement-inside="true"
            :status="tempQuotaStatus"
          >
            {{ formatSize(tempQuotaInfo.used) }} / {{ formatSize(tempQuotaInfo.limit) }}
          </n-progress>
        </n-card>
      </n-gi>
    </n-grid>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import {
  NCard, NGrid, NGi, NProgress, useMessage
} from 'naive-ui'
import { PrivateApi, TempApi } from '@/api'
import { NumberUtils } from '@/utils'

const message = useMessage()

const quotaInfo = ref({
  used: 0,
  quota: 1024 * 1024 * 1024
})
const fileCount = ref(0)
const tempQuotaInfo = ref({
  used: 0,
  limit: 1024 * 1024 * 1024
})

const formatSize = (bytes) => bytes === 0 ? '-' : NumberUtils.formatFileSize(bytes)

const quotaPercentage = computed(() => {
  if (quotaInfo.value.quota === 0) return 0
  return Math.round((quotaInfo.value.used / quotaInfo.value.quota) * 100)
})

const tempQuotaPercentage = computed(() => {
  if (tempQuotaInfo.value.limit === 0) return 0
  return Math.round((tempQuotaInfo.value.used / tempQuotaInfo.value.limit) * 100)
})

const quotaStatus = computed(() => {
  const pct = quotaPercentage.value
  if (pct >= 90) return 'error'
  if (pct >= 70) return 'warning'
  return 'success'
})

const tempQuotaStatus = computed(() => {
  const pct = tempQuotaPercentage.value
  if (pct >= 90) return 'error'
  if (pct >= 70) return 'warning'
  return 'success'
})

const loadQuota = async () => {
  try {
    const data = await PrivateApi.getQuota()
    if (data.success && data.data) {
      quotaInfo.value = data.data
    }
  } catch (error) {
    console.error('获取配额信息失败:', error)
  }
}

const loadFileCount = async () => {
  try {
    const data = await PrivateApi.list()
    if (data.success && data.data) {
      fileCount.value = data.data.files?.length || 0
    }
  } catch (error) {
    console.error('获取文件数量失败:', error)
  }
}

const loadTempQuota = async () => {
  try {
    const data = await TempApi.getQuota()
    if (data.success && data.data) {
      tempQuotaInfo.value = data.data
    }
  } catch (error) {
    console.error('获取临时文件配额失败:', error)
  }
}

onMounted(() => {
  loadQuota()
  loadFileCount()
  loadTempQuota()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-storage-section {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 16px;

    h2 {
      margin: 0 0 8px 0;
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .description {
      margin: 0;
      color: @text-color-secondary;
      font-size: @font-size-base;
    }
  }

  .quota-card {
    .quota-hint {
      color: @text-color-placeholder;
      font-size: @font-size-xs;
      text-align: center;
    }
  }
}
</style>
