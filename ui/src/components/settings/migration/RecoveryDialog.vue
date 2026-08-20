<template>
  <n-modal v-model:show="showModal" preset="card" title="迁移中断恢复" style="max-width: 600px">
    <n-tabs type="segment">
      <n-tab-pane name="resume" tab="继续迁移">
        <div class="recovery-option">
          <div class="option-icon resume-icon">▶</div>
          <h3>继续之前的迁移</h3>
          <p>从中断位置继续，不丢失已迁移的数据</p>
          <n-button type="primary" @click="handleResume" :loading="loading">
            继续迁移
          </n-button>
        </div>
      </n-tab-pane>

      <n-tab-pane name="restart" tab="重新迁移">
        <div class="recovery-option">
          <div class="option-icon restart-icon">↻</div>
          <h3>重新开始迁移</h3>
          <p>清除之前进度，重新开始迁移（不会影响原数据库）</p>
          <n-button type="warning" @click="handleRestart" :loading="loading">
            重新迁移
          </n-button>
        </div>
      </n-tab-pane>

      <n-tab-pane name="rollback" tab="回滚">
        <div class="recovery-option">
          <div class="option-icon rollback-icon">↩</div>
          <h3>回滚到迁移前</h3>
          <p>使用备份恢复原数据库，迁移将被标记为已回滚</p>
          <n-button type="error" @click="handleRollback" :loading="loading">
            执行回滚
          </n-button>
        </div>
      </n-tab-pane>
    </n-tabs>

    <template #footer>
      <div class="modal-footer">
        <span class="migration-info" v-if="interrupted">
          中断时间: {{ formatTime(interrupted.startedAt) }}
        </span>
        <n-button @click="showModal = false">关闭</n-button>
      </div>
    </template>
  </n-modal>
</template>

<script setup>
import { ref, computed } from 'vue'
import { NModal, NTabs, NTabPane, NButton, useMessage } from 'naive-ui'
import { databaseApi } from '@/api'
import { TimeUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'

const props = defineProps({
  interrupted: Object,
  visible: Boolean
})

const emit = defineEmits(['update:visible', 'action-complete'])

const message = useMessage()
const showModal = computed({
  get: () => props.visible,
  set: (val) => emit('update:visible', val)
})

const loading = ref(false)

const formatTime = (time) => {
  if (!time) return '-'
  return TimeUtils.formatDateTime(time)
}

const handleResume = async () => {
  loading.value = true
  try {
    await databaseApi.resumeMigration(props.interrupted.migrationId)
    message.success('已启动继续迁移')
    emit('action-complete')
    showModal.value = false
  } catch (err) {
    message.error(formatErrorMessage(err, '启动继续迁移失败'))
  } finally {
    loading.value = false
  }
}

const handleRestart = async () => {
  loading.value = true
  try {
    await databaseApi.restartMigration(props.interrupted.migrationId)
    message.success('已启动重新迁移')
    emit('action-complete')
    showModal.value = false
  } catch (err) {
    message.error(formatErrorMessage(err, '启动重新迁移失败'))
  } finally {
    loading.value = false
  }
}

const handleRollback = async () => {
  loading.value = true
  try {
    await databaseApi.rollbackMigration(props.interrupted.migrationId)
    message.success('回滚成功，数据库已恢复')
    emit('action-complete')
    showModal.value = false
  } catch (err) {
    message.error(formatErrorMessage(err, '回滚失败'))
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.recovery-option {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 24px;
  text-align: center;
}

.recovery-option h3 {
  margin: 16px 0 8px;
}

.recovery-option p {
  color: #666;
  margin-bottom: 16px;
}

.option-icon {
  width: 64px;
  height: 64px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 28px;
  color: white;
}

.resume-icon {
  background: #18a058;
}

.restart-icon {
  background: #f0a020;
}

.rollback-icon {
  background: #d03050;
}

.modal-footer {
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.migration-info {
  color: #666;
  font-size: 12px;
}
</style>