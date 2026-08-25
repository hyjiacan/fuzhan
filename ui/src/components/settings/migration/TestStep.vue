<template>
  <div class="test-step">
    <el-alert
      v-if="testResult"
      :type="testResult.connected ? 'success' : 'error'"
      :closable="false"
      :title="testResult.connected ? '连接成功！' : '连接失败'"
      class="test-alert"
    >
      <div v-if="testResult.connected" class="connection-info">
        <div>版本: {{ testResult.version }}</div>
        <div>字符集: {{ testResult.characterSet || testResult.DatabaseInfo?.charset || 'N/A' }}</div>
        <div>表数量: {{ testResult.tableCount || testResult.Tables?.length || 0 }}</div>
        <div>记录数: {{ testResult.recordCount || testResult.DatabaseInfo?.recordCount || 0 }}</div>
        <div>预估大小: {{ testResult.estimatedSize || testResult.DatabaseInfo?.dataSize || '0 KB' }}</div>
      </div>
      <div v-else>
        <div v-if="testResult.errorInfo">
          <div>错误码: {{ testResult.errorInfo.code }}</div>
          <div>原因: {{ testResult.errorInfo.message }}</div>
          <div v-if="testResult.errorInfo.suggestion">建议: {{ testResult.errorInfo.suggestion }}</div>
        </div>
        <div v-else>请检查连接配置</div>
      </div>
    </el-alert>

    <div class="test-actions">
      <el-button :loading="testing" type="primary" @click="handleTest">
        {{ testing ? '测试中...' : '测试连接' }}
      </el-button>
    </div>

    <div class="step-actions">
      <el-button @click="$emit('back')">上一步</el-button>
      <el-button type="primary" :disabled="!testResult?.connected" @click="handleNext">
        下一步
      </el-button>
    </div>
  </div>
</template>

<script>
import { ref } from 'vue'
import { DatabaseApi } from '@/api'
import { formatErrorMessage } from '@/utils/error'

export default {
  name: 'TestStep',

  props: {
    config: {
      type: Object,
      required: true
    }
  },

  emits: ['next', 'back'],

  setup(props, { emit }) {
    const testing = ref(false)
    const testResult = ref(null)

    const handleTest = async () => {
      testing.value = true
      testResult.value = null

      try {
        // 根据 config 构建请求
        const [driver, dsn] = parseConfig(props.config)
        testResult.value = await DatabaseApi.testConnection({ driver, dsn })
        if (testResult.value.data?.connected) {
          testResult.value = testResult.value.data
        }
      } catch (err) {
        testResult.value = {
          connected: false,
          errorInfo: {
            code: 'TEST_FAILED',
            message: formatErrorMessage(err, '连接测试失败')
          }
        }
      } finally {
        testing.value = false
      }
    }

    const parseConfig = (config) => {
      // config 是 DSN 字符串
      const dsn = config
      let driver = 'sqlite'

      if (dsn.includes('@tcp(') || dsn.includes('localhost')) {
        driver = 'mysql'
      } else if (dsn.includes('host=') || dsn.includes('postgres://')) {
        driver = 'postgres'
      }

      return [driver, dsn]
    }

    const handleNext = () => {
      emit('next', testResult.value)
    }

    return {
      testing,
      testResult,
      handleTest,
      handleNext
    }
  }
}
</script>

<style scoped>
.test-step {
  padding: 16px 0;
}

.test-alert {
  margin-bottom: 24px;
}

.alert-icon {
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: rgba(255, 255, 255, 0.2);
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 14px;
  color: white;
}

.connection-info {
  margin-top: 8px;
  font-size: 14px;
  line-height: 1.6;
}

.test-actions {
  margin-top: 16px;
}

.step-actions {
  display: flex;
  justify-content: space-between;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--border-color);
}
</style>