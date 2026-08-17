<template>
  <div class="progress-step">
    <n-progress
      type="line"
      :percentage="progress"
      :status="progressStatus"
      :indicator-placement="inside"
    >
      {{ progress }}%
    </n-progress>

    <n-descriptions :column="1" size="small" class="progress-info">
      <n-descriptions-item label="当前阶段">
        <n-tag :type="stageType" size="small">{{ stageLabel }}</n-tag>
      </n-descriptions-item>
      <n-descriptions-item label="正在处理" v-if="currentTable">
        {{ currentTable }}
      </n-descriptions-item>
      <n-descriptions-item label="已迁移记录">
        {{ recordsMigrated.toLocaleString() }} 条
      </n-descriptions-item>
      <n-descriptions-item label="已处理表">
        {{ tablesCompleted }} / {{ tablesTotal }}
      </n-descriptions-item>
    </n-descriptions>

    <n-alert v-if="error" type="error" class="error-alert">
      <strong>迁移出错</strong>
      <div>{{ error }}</div>
      <div v-if="rollbackAvailable" class="rollback-info">
        系统已保存原数据库，可以安全回滚
      </div>
    </n-alert>

    <div class="step-actions">
      <n-popconfirm @positive-click="handleCancel">
        <template #trigger>
          <n-button type="error">取消迁移</n-button>
        </template>
        确定要取消迁移吗？迁移将被中断，可能需要回滚。
      </n-popconfirm>
    </div>
  </div>
</template>

<script>
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { NProgress, NDescriptions, NDescriptionsItem, NTag, NAlert, NButton, NPopconfirm } from 'naive-ui'
import { DatabaseApi } from '@/api'

export default {
  name: 'ProgressStep',

  components: {
    NProgress, NDescriptions, NDescriptionsItem, NTag, NAlert, NButton, NPopconfirm
  },

  emits: ['complete', 'error', 'cancel'],

  props: {
    eventSource: Object,
    migrationConfig: {
      type: Object,
      default: () => ({ driver: '', dsn: '' })
    }
  },

  setup(props, { emit }) {
    const progress = ref(0)
    const stage = ref('preparing')
    const currentTable = ref('')
    const recordsMigrated = ref(0)
    const tablesCompleted = ref(0)
    const tablesTotal = ref(0)
    const error = ref('')
    const rollbackAvailable = ref(false)

    let xhr = null
    let eventSourceClosed = false

    const stageLabels = {
      preparing: '准备环境',
      backup: '备份数据库',
      export: '导出数据',
      transform: '转换格式',
      import: '导入数据',
      verify: '验证完整性',
      complete: '迁移完成',
      error: '出错'
    }

    const stageTypes = {
      preparing: 'default',
      backup: 'info',
      export: 'info',
      transform: 'info',
      import: 'warning',
      verify: 'info',
      complete: 'success',
      error: 'error'
    }

    const stageLabel = computed(() => stageLabels[stage.value] || stage.value)
    const stageType = computed(() => stageTypes[stage.value] || 'default')

    const progressStatus = computed(() => {
      if (error.value) return 'error'
      if (progress.value >= 100) return 'success'
      return 'active'
    })

    const startMigration = async () => {
      xhr = new XMLHttpRequest()
      xhr.open('POST', '/api/v1/admin/database/migrate', true)
      xhr.setRequestHeader('Content-Type', 'application/json')

      const token = localStorage.getItem('token')
      if (token) {
        xhr.setRequestHeader('Authorization', `Bearer ${token}`)
      }

      // 设置 SSE 解析
      let buffer = ''
      xhr.onprogress = () => {
        buffer += xhr.responseText.substring(buffer.length)
        const lines = buffer.split('\n')
        buffer = lines.pop() || ''

        for (const line of lines) {
          if (line.startsWith('data: ')) {
            try {
              const event = JSON.parse(line.substring(6))
              handleEvent(event)
            } catch (e) {
              console.error('解析事件失败:', e)
            }
          }
        }
      }

      xhr.onload = () => {
        if (xhr.status >= 200 && xhr.status < 300) {
          if (progress.value < 100) {
            progress.value = 100
            stage.value = 'complete'
            emit('complete')
          }
        } else {
          try {
            const err = JSON.parse(xhr.responseText)
            error.value = err.message || '迁移失败'
          } catch {
            error.value = `迁移失败: ${xhr.status}`
          }
          emit('error', error.value)
        }
        eventSourceClosed = true
      }

      xhr.onerror = () => {
        error.value = '网络错误'
        emit('error', error.value)
        eventSourceClosed = true
      }

      // 获取迁移配置（通过 props 传递，避免 DSN 明文存储在 localStorage）
      const config = {
        targetDriver: props.migrationConfig?.driver || '',
        targetDSN: props.migrationConfig?.dsn || ''
      }

      xhr.send(JSON.stringify(config))
    }

    const handleEvent = (event) => {
      progress.value = event.progress || 0
      stage.value = event.stage || 'preparing'
      currentTable.value = event.current || ''
      recordsMigrated.value = event.recordsMigrated || 0
      tablesCompleted.value = event.tablesCompleted || 0
      tablesTotal.value = event.tablesTotal || 0

      if (event.stage === 'error') {
        error.value = event.message || event.error || '未知错误'
        rollbackAvailable.value = event.rollbackAvailable || false
        emit('error', error.value)
      } else if (event.stage === 'complete') {
        emit('complete')
      }
    }

    const handleCancel = async () => {
      if (xhr && !eventSourceClosed) {
        xhr.abort()
        eventSourceClosed = true
      }

      try {
        await DatabaseApi.cancel()
        emit('cancel')
      } catch (err) {
        emit('cancel')
      }
    }

    onMounted(() => {
      startMigration()
    })

    onUnmounted(() => {
      if (xhr && !eventSourceClosed) {
        xhr.abort()
      }
    })

    return {
      progress,
      stage,
      currentTable,
      recordsMigrated,
      tablesCompleted,
      tablesTotal,
      error,
      rollbackAvailable,
      stageLabel,
      stageType,
      progressStatus,
      handleCancel
    }
  }
}
</script>

<style scoped>
.progress-step {
  padding: 16px 0;
}

.progress-info {
  margin-top: 24px;
}

.error-alert {
  margin-top: 24px;
}

.rollback-info {
  margin-top: 8px;
  font-size: 13px;
}

.step-actions {
  display: flex;
  justify-content: flex-end;
  margin-top: 24px;
  padding-top: 16px;
  border-top: 1px solid var(--border-color);
}
</style>