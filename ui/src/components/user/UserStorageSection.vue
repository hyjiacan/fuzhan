<template>
  <div class="user-storage-section">
    <div class="header-section">
      <div class="header-left">
        <h2>存储空间</h2>
        <p class="description">查看您的文件上传情况与存储使用统计</p>
      </div>
    </div>

    <el-card class="quota-card">
      <template #header><span>公共文件</span></template>
      <div class="stat-body">
        <div class="stat-row">
          <span class="stat-label">已上传</span>
          <span class="stat-value">{{ publicStats.files ?? '—' }} 个文件</span>
        </div>
        <div class="stat-row">
          <span class="stat-label">占用空间</span>
          <span class="stat-value">{{ formatSize(publicStats.bytes) }}</span>
        </div>
        <div class="stat-profile" v-if="publicProfileText">{{ publicProfileText }}</div>
      </div>
      <template #footer>
        <div class="quota-hint">你在公共存储上传的文件</div>
      </template>
    </el-card>

    <el-card class="quota-card">
      <template #header><span>私有存储</span></template>
      <el-progress type="line" :percentage="quotaPercentage" :stroke-width="20"
        :status="quotaStatus === 'error' ? 'exception' : quotaStatus">
        {{ formatSize(quotaInfo.used) }} / {{ formatSize(quotaInfo.quota) }}
      </el-progress>
      <template #footer>
        <div class="quota-hint">
          已存储 {{ fileCount }} 个文件
          <template v-if="privateStats.files != null && privateStats.files !== fileCount">
            （私有记录 {{ privateStats.files }}）
          </template>
        </div>
      </template>
    </el-card>

    <el-card class="quota-card">
      <template #header><span>临时文件配额</span></template>
      <el-progress type="line" :percentage="tempQuotaPercentage" :stroke-width="20"
        :status="tempQuotaStatus === 'error' ? 'exception' : tempQuotaStatus">
        {{ formatSize(tempQuotaInfo.used) }} / {{ formatSize(tempQuotaInfo.limit) }}
      </el-progress>
      <template #footer>
        <div class="quota-hint">
          当前 {{ tempStats.files ?? '—' }} 个临时文件
          <template v-if="clientIP">（IP：{{ clientIP }}）</template>
        </div>
      </template>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { PrivateApi, TempApi, PersonalApi } from '@/api'
import { NumberUtils } from '@/utils'
import store from '@/store'

// 私有存储未启用时隐藏私有配额卡片
const privateStorageEnabled = computed(() => store.state.config.privateStorageEnabled !== false)

const quotaInfo = ref({
  used: 0,
  quota: null
})
const fileCount = ref(0)
const tempQuotaInfo = ref({
  used: 0,
  limit: null
})
const clientIP = ref('')

// 该用户三类上传统计（后端 /user/stats）
const publicStats = ref({
  files: null,
  bytes: null
})
const privateStats = ref({
  files: null,
  bytes: null
})
const tempStats = ref({
  files: null,
  bytes: null
})

// 公共文件说明：后端以 user_id=当前IP 或 client_ip=当前IP 聚合，展示用户视角的口径
const publicProfileText = computed(() => {
  if (publicStats.value.files == null) return ''
  if (publicStats.value.files > 0) return `已在公共存储累计上传文件`
  return '尚未在公共存储上传文件'
})

// 配额未知（后端未返回）时显示占位符
const formatSize = (bytes) => (bytes == null || bytes === 0) ? '—' : NumberUtils.formatFileSize(bytes)

const quotaPercentage = computed(() => {
  if (!quotaInfo.value.quota) return 0
  return Math.round((quotaInfo.value.used / quotaInfo.value.quota) * 100)
})

const tempQuotaPercentage = computed(() => {
  if (!tempQuotaInfo.value.limit) return 0
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
  try {
    const ipData = await TempApi.getClientIP()
    if (ipData.success && ipData.data) {
      clientIP.value = ipData.data.ip || ''
    }
  } catch (error) {
    console.error('获取客户端IP失败:', error)
  }
}

const loadPublicStats = async () => {
  try {
    const data = await PersonalApi.getStats()
    if (data.success && data.data) {
      publicStats.value = {
        files: data.data.publicFiles ?? null,
        bytes: data.data.publicBytes ?? null
      }
      privateStats.value = {
        files: data.data.privateFiles ?? null,
        bytes: data.data.privateBytes ?? null
      }
      tempStats.value = {
        files: data.data.tempFiles ?? null,
        bytes: data.data.tempBytes ?? null
      }
    }
  } catch (error) {
    console.error('获取上传统计失败:', error)
  }
}

onMounted(() => {
  loadPublicStats()
  if (privateStorageEnabled.value) {
    loadQuota()
    loadFileCount()
  }
  loadTempQuota()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-storage-section {
  padding: @container-padding;

  .quota-card {
    margin: 20px 0;

    .stat-body {
      padding: 8px 0;

      .stat-row {
        display: flex;
        gap: 10px;
        align-items: center;
        padding: 6px 0;
        font-size: @font-size-base;

        .stat-label {
          color: @text-color-secondary;
        }

        .stat-value {
          font-weight: 600;
          color: @text-color-secondary;
        }
      }

      .stat-profile {
        margin-top: 8px;
        padding-top: 8px;
        border-top: 1px dashed @border-color;
        color: @text-color-placeholder;
        font-size: @font-size-xs;
      }
    }

    .quota-hint {
      color: @text-color-placeholder;
      font-size: @font-size-xs;
      text-align: center;
    }
  }
}
</style>
