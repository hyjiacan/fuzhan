<template>
  <div class="startup-error-page">
    <div class="startup-error-card">
      <div class="startup-error-icon">
        <el-icon :size="56"><WarningFilled /></el-icon>
      </div>
      <h2 class="startup-error-title">服务暂不可用</h2>
      <p class="startup-error-desc">无法获取服务器配置，后端服务可能未启动或出现异常。请稍后重试。</p>
      <el-button type="primary" :loading="retrying" @click="retry">
        {{ retrying ? '正在重试…' : '重新加载' }}
      </el-button>
    </div>
  </div>
</template>

<script setup>
import { ref } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'

const retrying = ref(false)

// 重新加载：直接刷新页面，让启动流程重新走一遍完整请求
function retry() {
  retrying.value = true
  window.location.reload()
}
</script>

<style lang="less" scoped>
.startup-error-page {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--el-bg-color-page);
}

.startup-error-card {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  text-align: center;
  padding: 48px;
  border-radius: 12px;
  background: var(--el-bg-color);
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.06);
}

.startup-error-icon {
  color: var(--el-color-danger);
}

.startup-error-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: var(--el-text-color-primary);
}

.startup-error-desc {
  margin: 0;
  max-width: 360px;
  font-size: 14px;
  color: var(--el-text-color-secondary);
}
</style>
