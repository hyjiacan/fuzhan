<template>
  <div class="user-info-section">
    <div class="header-section">
      <div class="header-left">
        <h2>个人信息</h2>
        <p class="description">查看您的账号信息</p>
      </div>
    </div>

    <el-card class="info-card">
      <el-descriptions :column="1">
        <el-descriptions-item label="用户类型">
          <el-tag :type="userInfo.role === 'admin' ? 'info' : 'success'">
            {{ userInfo.role === 'admin' ? '管理员' : '普通用户' }}
          </el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="用户名">
          {{ userInfo.username }}
        </el-descriptions-item>
        <el-descriptions-item label="用户ID">
          {{ userInfo.uuid }}
        </el-descriptions-item>
        <el-descriptions-item label="注册时间">
          {{ formatDate(userInfo.createdAt) }}
        </el-descriptions-item>
      </el-descriptions>
    </el-card>

    <el-card class="password-card">
      <template #header>
        <div class="card-header">修改密码</div>
      </template>
      <el-form ref="formRef" :model="passwordForm" :rules="passwordRules" label-width="96">
        <el-form-item label="当前密码" prop="oldPassword">
          <el-input
            v-model="passwordForm.oldPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请输入当前密码"
            autocomplete="current-password"
          />
        </el-form-item>
        <el-form-item label="新密码" prop="newPassword">
          <el-input
            v-model="passwordForm.newPassword"
            :maxlength="128"
            type="password"
            show-password
            placeholder="请输入新密码"
            autocomplete="new-password"
          />
        </el-form-item>
        <el-form-item label="确认密码" prop="confirmPassword">
          <el-input
            v-model="passwordForm.confirmPassword"
            :maxlength="128"
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
import { ref, reactive, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { AuthApi } from '@/api'
import { TimeUtils } from '@/utils'

const userInfo = ref({
  username: '',
  uuid: '',
  role: 'user',
  createdAt: ''
})

const formatDate = (dateStr) => TimeUtils.formatDateTime(dateStr)

const loadUserInfo = async () => {
  try {
    const data = await AuthApi.getUserInfo()
    if (data.success && data.data) {
      userInfo.value = data.data
      localStorage.setItem('userRole', data.data.role || 'user')
    }
  } catch (error) {
    console.error('获取用户信息失败:', error)
    ElMessage.error('获取用户信息失败')
  }
}

const formRef = ref(null)
const changingPassword = ref(false)

const passwordForm = reactive({
  oldPassword: '',
  newPassword: '',
  confirmPassword: ''
})

const validatePasswordSame = (rule, value) => {
  if (value !== passwordForm.newPassword) {
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

const handleChangePassword = async () => {
  try {
    await formRef.value?.validate()
  } catch {
    return
  }

  changingPassword.value = true
  try {
    const data = await AuthApi.changePassword(
      passwordForm.oldPassword,
      passwordForm.newPassword
    )
    if (data.success) {
      ElMessage.success('密码修改成功')
      passwordForm.oldPassword = ''
      passwordForm.newPassword = ''
      passwordForm.confirmPassword = ''
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

onMounted(() => {
  loadUserInfo()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-info-section {
  padding: @container-padding;

  .info-card,
  .password-card {
    max-width: 600px;

    + .info-card,
    + .password-card {
      margin-top: 16px;
    }
  }

  .card-header {
    font-weight: 600;
  }
}
</style>
