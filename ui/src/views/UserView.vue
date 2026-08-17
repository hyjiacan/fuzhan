<template>
  <div class="user-view">
    <div class="header-section">
      <h2>个人中心</h2>
      <p class="description">管理您的个人信息和存储空间</p>
    </div>

    <n-grid :cols="2" :x-gap="24" :y-gap="24" responsive="screen" :item-responsive="true">
      <!-- 用户信息卡片 -->
      <n-gi :span="2" :md="1">
        <n-card title="个人信息" class="info-card">
          <n-descriptions label-placement="left" :column="1">
            <n-descriptions-item label="用户名">
              <n-badge :value="userInfo.role" type="success" :offset="[10, 0]">
                {{ userInfo.username }}
              </n-badge>
            </n-descriptions-item>
            <n-descriptions-item label="用户ID">
              {{ userInfo.uuid }}
            </n-descriptions-item>
            <n-descriptions-item label="注册时间">
              {{ formatDate(userInfo.createdAt) }}
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>

      <!-- 存储空间卡片 -->
      <n-gi :span="2" :md="1">
        <n-card title="存储空间" class="quota-card">
          <n-progress
            type="line"
            :percentage="quotaPercentage"
            :indicator-placement-inside="true"
            :status="quotaStatus"
          >
            {{ NumberUtils.formatFileSize(quotaInfo.used) }} / {{ NumberUtils.formatFileSize(quotaInfo.quota) }}
          </n-progress>
          <template #footer>
            <div class="quota-hint">已存储 {{ fileCount }} 个文件</div>
          </template>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- 临时文件配额 -->
    <n-card title="临时文件配额" class="quota-card">
      <n-progress
        type="line"
        :percentage="tempQuotaPercentage"
        :indicator-placement-inside="true"
        :status="tempQuotaStatus"
      >
        {{ NumberUtils.formatFileSize(tempQuotaInfo.used) }} / {{ NumberUtils.formatFileSize(tempQuotaInfo.limit) }}
      </n-progress>
    </n-card>

    <!-- 修改密码 -->
    <n-card title="修改密码" class="password-card">
      <n-form ref="formRef" :model="passwordForm" :rules="passwordRules" label-placement="left" label-width="120">
        <n-form-item label="当前密码" path="oldPassword">
          <n-input
            v-model:value="passwordForm.oldPassword"
            type="password"
            show-password-on="click"
            placeholder="请输入当前密码"
            autocomplete="current-password"
          />
        </n-form-item>
        <n-form-item label="新密码" path="newPassword">
          <n-input
            v-model:value="passwordForm.newPassword"
            type="password"
            show-password-on="click"
            placeholder="请输入新密码"
            autocomplete="new-password"
          />
        </n-form-item>
        <n-form-item label="确认密码" path="confirmPassword">
          <n-input
            v-model:value="passwordForm.confirmPassword"
            type="password"
            show-password-on="click"
            placeholder="请再次输入新密码"
            autocomplete="new-password"
          />
        </n-form-item>
        <n-form-item>
          <n-button type="primary" :loading="changingPassword" @click="handleChangePassword">
            确认修改
          </n-button>
        </n-form-item>
      </n-form>
    </n-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import {
  NCard, NGrid, NGi, NDescriptions, NDescriptionsItem, NProgress,
  NForm, NFormItem, NInput, NButton, NBadge, useMessage
} from 'naive-ui'
import { AuthApi, PrivateApi, TempApi } from '@/api'
import { NumberUtils } from '@/utils'

const router = useRouter()
const message = useMessage()

// State
const userInfo = ref({
  username: '',
  uuid: '',
  role: 'user',
  createdAt: ''
})
const quotaInfo = ref({
  used: 0,
  quota: 1024 * 1024 * 1024 // 默认 1GB
})
const fileCount = ref(0)
const tempQuotaInfo = ref({
  used: 0,
  limit: 1024 * 1024 * 1024
})
const passwordForm = ref({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})
const changingPassword = ref(false)
const formRef = ref(null)

// Computed
const quotaPercentage = computed(() => {
  if (quotaInfo.value.quota === 0) return 0
  return Math.round((quotaInfo.value.used / quotaInfo.value.quota) * 100)
})

const tempQuotaPercentage = computed(() => {
  if (tempQuotaInfo.value.limit === 0) return 0
  return Math.round((tempQuotaInfo.value.used / tempQuotaInfo.value.limit) * 100)
})

const tempQuotaStatus = computed(() => {
  const pct = tempQuotaPercentage.value
  if (pct >= 90) return "error"
  if (pct >= 70) return "warning"
  return "success"
})

const quotaStatus = computed(() => {
  const pct = quotaPercentage.value
  if (pct >= 90) return 'error'
  if (pct >= 70) return 'warning'
  return 'success'
})

// Password validation rules
const validatePasswordSame = (rule, value) => {
  if (value !== passwordForm.value.newPassword) {
    return new Error('两次输入的密码不一致')
  }
  return true
}

const passwordRules = {
  oldPassword: [{ required: true, message: '请输入当前密码', trigger: 'blur' }],
  newPassword: [
    { required: true, message: '请输入新密码', trigger: 'blur' },
    { min: 6, message: '密码至少6位', trigger: 'blur' }
  ],
  confirmPassword: [
    { required: true, message: '请确认新密码', trigger: 'blur' },
    { validator: validatePasswordSame, trigger: 'blur' }
  ]
}

// Methods
const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

const loadUserInfo = async () => {
  try {
    const data = await AuthApi.getUserInfo()
    if (data.success && data.data) {
      userInfo.value = data.data
      // 保存角色到 localStorage
      localStorage.setItem('userRole', data.data.role || 'user')
    }
  } catch (error) {
    console.error('获取用户信息失败:', error)
    message.error('获取用户信息失败')
  }
}

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

const handleChangePassword = async () => {
  try {
    await formRef.value?.validate()
  } catch {
    return
  }

  changingPassword.value = true
  try {
    const data = await AuthApi.changePassword(
      passwordForm.value.oldPassword,
      passwordForm.value.newPassword
    )
    if (data.success) {
      message.success('密码修改成功')
      passwordForm.value = { oldPassword: '', newPassword: '', confirmPassword: '' }
    } else {
      message.error(data.message || '密码修改失败')
    }
  } catch (error) {
    message.error('密码修改失败')
    console.error(error)
  } finally {
    changingPassword.value = false
  }
}

// Lifecycle
onMounted(() => {
  loadUserInfo()
  loadQuota()
  loadFileCount()
  loadTempQuota()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-view {
  display: flex;
  flex-direction: column;
  gap: 16px;
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 8px;

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

  .n-card {
    transition: transform @transition-smooth, box-shadow @transition-smooth;

    &:hover {
      transform: translateY(-2px);
      box-shadow: @card-hover-shadow;
    }

    .n-card-header {
      font-weight: 600;
    }
  }

  .quota-hint {
    color: @text-color-placeholder;
    font-size: @font-size-xs;
    text-align: center;
  }
}

@media @tablet {
  .user-view {
    padding: 12px;
  }
}

@media @mobile {
  .user-view {
    padding: 8px;
  }

  :deep(.n-form .n-form-item .n-form-item-label) {
    padding-bottom: 4px;
  }
}
</style>