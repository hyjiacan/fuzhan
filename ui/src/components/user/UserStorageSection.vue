<template>
  <div class="user-storage-section">
    <div class="header-section">
      <h2>存储空间</h2>
      <p class="description">查看您的存储使用情况</p>
    </div>

    <el-row :gutter="24">
      <!-- Private storage quota -->
      <el-col :xs="24" :md="12">
        <el-card class="quota-card">
          <template #header><span>私有存储</span></template>
          <el-progress
            type="line"
            :percentage="quotaPercentage"
            :stroke-width="20"
            :status="quotaStatus === 'error' ? 'exception' : quotaStatus"
          >
            {{ formatSize(quotaInfo.used) }} / {{ formatSize(quotaInfo.quota) }}
          </el-progress>
          <template #footer>
            <div class="quota-hint">已存储 {{ fileCount }} 个文件</div>
          </template>
        </el-card>
      </el-col>

      <!-- Temp file quota -->
      <el-col :xs="24" :md="12">
        <el-card class="quota-card">
          <template #header><span>临时文件配额</span></template>
          <el-progress
            type="line"
            :percentage="tempQuotaPercentage"
            :stroke-width="20"
            :status="tempQuotaStatus === 'error' ? 'exception' : tempQuotaStatus"
          >
            {{ formatSize(tempQuotaInfo.used) }} / {{ formatSize(tempQuotaInfo.limit) }}
          </el-progress>
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { PrivateApi, TempApi } from '@/api'
import { NumberUtils } from '@/utils'

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
