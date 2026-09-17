<template>
  <div class="user-info-section">
    <div class="header-section">
      <h2>个人信息</h2>
      <p class="description">查看您的账号信息</p>
    </div>

    <el-card class="info-card">
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
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
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

onMounted(() => {
  loadUserInfo()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.user-info-section {
  padding: @container-padding;

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

  .info-card {
    max-width: 600px;
  }
}
</style>
