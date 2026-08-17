<template>
  <div class="user-info-section">
    <div class="header-section">
      <h2>个人信息</h2>
      <p class="description">查看您的账号信息</p>
    </div>

    <n-card class="info-card">
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
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import {
  NCard, NDescriptions, NDescriptionsItem, NBadge, useMessage
} from 'naive-ui'
import { AuthApi } from '@/api'

const message = useMessage()

const userInfo = ref({
  username: '',
  uuid: '',
  role: 'user',
  createdAt: ''
})

const formatDate = (dateStr) => {
  if (!dateStr) return '-'
  return new Date(dateStr).toLocaleString('zh-CN')
}

const loadUserInfo = async () => {
  try {
    const data = await AuthApi.getUserInfo()
    if (data.success && data.data) {
      userInfo.value = data.data
      localStorage.setItem('userRole', data.data.role || 'user')
    }
  } catch (error) {
    console.error('获取用户信息失败:', error)
    message.error('获取用户信息失败')
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

  .info-card {
    max-width: 600px;
  }
}
</style>
