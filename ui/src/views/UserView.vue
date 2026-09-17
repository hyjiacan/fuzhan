<template>
  <div class="user-view">
    <div class="header-section">
      <h2>个人中心</h2>
      <p class="description">管理您的个人信息和存储空间</p>
    </div>

    <el-row :gutter="24">
      <!-- 用户信息卡片 -->
      <el-col :xs="24" :md="12">
        <el-card class="info-card">
          <template #header><span>个人信息</span></template>
          <el-descriptions :column="1">
            <el-descriptions-item label="用户名">
              <el-badge :value="userInfo.role" type="success">
                {{ userInfo.username }}
              </el-badge>
            </el-descriptions-item>
            <el-descriptions-item label="用户ID">
              {{ userInfo.uuid }}
            </el-descriptions-item>
            <el-descriptions-item label="注册时间">
              {{ formatDate(userInfo.createdAt) }}
            </el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>

      <!-- 存储空间卡片 -->
      <el-col :xs="24" :md="12">
        <el-card class="quota-card">
          <template #header><span>存储空间</span></template>
          <el-progress
            type="line"
            :percentage="quotaPercentage"
            :stroke-width="20"
            :status="quotaStatus === 'error' ? 'exception' : quotaStatus"
          >
            {{ NumberUtils.formatFileSize(quotaInfo.used) }} / {{ NumberUtils.formatFileSize(quotaInfo.quota) }}
          </el-progress>
          <template #footer>
            <div class="quota-hint">已存储 {{ fileCount }} 个文件</div>
          </template>
        </el-card>
      </el-col>
    </el-row>

    <!-- 临时文件配额 -->
    <el-card class="quota-card">
      <template #header><span>临时文件配额</span></template>
      <el-progress
        type="line"
        :percentage="tempQuotaPercentage"
        :stroke-width="20"
        :status="tempQuotaStatus === 'error' ? 'exception' : tempQuotaStatus"
      >
        {{ NumberUtils.formatFileSize(tempQuotaInfo.used) }} / {{ NumberUtils.formatFileSize(tempQuotaInfo.limit) }}
      </el-progress>
    </el-card>

    <!-- 修改密码 -->
    <el-card class="password-card">
      <template #header><span>修改密码</span></template>
      <el-form ref="formRef" :model="passwordForm" :rules="passwordRules" label-width="120">
        <el-form-item label="当前密码" prop="oldPassword">
          <el-input
            v-model="passwordForm.oldPassword"
            type="password"
            show-password
            placeholder="请输入当前密码"
            autocomplete="current-password"
          />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="passwordForm.newPassword"
            type="password"
            show-password
            placeholder="请输入新密码"
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="passwordForm.confirmPassword"
            type="password"
            show-password
            placeholder="请再次输入新密码"
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item>
          <el-button type="primary" :loading="changingPassword" @click="handleChangePassword">
            确认修改
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { AuthApi, PrivateApi, TempApi } from '@/api'
import { NumberUtils, TimeUtils } from '@/utils'

const router = useRouter()

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
const formatDate = (dateStr) => TimeUtils.formatDateTime(dateStr)

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
    ElMessage.error('获取用户信息失败')
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
      ElMessage.success('密码修改成功')
      passwordForm.value = { oldPassword: '', newPassword: '', confirmPassword: '' }
    } else {
      ElMessage.error(data.message || '密码修改失败')
    }
  } catch (error) {
    ElMessage.error('密码修改失败')
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

  .el-card {
    transition: transform @transition-smooth, box-shadow @transition-smooth;

    &:hover {
      transform: translateY(-2px);
      box-shadow: @card-hover-shadow;
    }

    .el-card__header {
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

  :deep(.el-form .el-form-item .el-form-item__label) {
    padding-bottom: 4px;
  }
}
</style>