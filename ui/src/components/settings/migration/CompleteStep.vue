<template>
  <div class="complete-step">
    <div class="complete-icon">
      <div v-if="success" class="icon-success">✓</div>
      <div v-else class="icon-error">✗</div>
    </div>

    <h3 class="complete-title">{{ success ? '迁移完成' : '迁移失败' }}</h3>

    <p class="complete-message">{{ message }}</p>

    <n-divider />

    <div v-if="success" class="next-steps">
      <h4>后续步骤</h4>
      <n-space vertical>
        <n-text>1. 数据库已成功迁移到新配置</n-text>
        <n-text>2. 点击"重启应用"使配置生效</n-text>
        <n-text>3. 重启后验证数据完整性</n-text>
      </n-space>
    </div>

    <div v-else class="error-details">
      <h4>错误详情</h4>
      <n-alert type="error">
        {{ message }}
      </n-alert>
      <n-text depth="3" class="help-text">
        如需帮助，请查看服务器日志或联系技术支持
      </n-text>
    </div>

    <div class="step-actions">
      <n-button @click="$emit('close')">关闭</n-button>
      <n-button v-if="success" type="primary" @click="$emit('restart')">
        重启应用
      </n-button>
    </div>
  </div>
</template>

<script>
import { NDivider, NSpace, NText, NAlert } from 'naive-ui'

export default {
  name: 'CompleteStep',

  components: {
    NDivider, NSpace, NText, NAlert
  },

  props: {
    success: {
      type: Boolean,
      default: false
    },
    message: {
      type: String,
      default: ''
    }
  },

  emits: ['close', 'restart']
}
</script>

<style scoped>
.complete-step {
  text-align: center;
  padding: 24px 0;
}

.complete-icon {
  margin-bottom: 16px;
}

.icon-success,
.icon-error {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  font-size: 32px;
  color: white;
}

.icon-success {
  background: #52c41a;
}

.icon-error {
  background: #ff4d4f;
}

.complete-title {
  font-size: 20px;
  font-weight: 600;
  margin-bottom: 8px;
}

.complete-message {
  color: var(--text-color-secondary);
  margin-bottom: 24px;
}

.next-steps,
.error-details {
  text-align: left;
  margin-bottom: 24px;
}

.next-steps h4,
.error-details h4 {
  margin-bottom: 12px;
}

.help-text {
  margin-top: 8px;
  display: block;
}

.step-actions {
  display: flex;
  justify-content: center;
  gap: 12px;
  padding-top: 16px;
  border-top: 1px solid var(--border-color);
}
</style>