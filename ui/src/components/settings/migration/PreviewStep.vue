<template>
  <div class="preview-step">
    <n-grid :cols="2" :x-gap="16" :y-gap="16">
      <n-gi>
        <n-card title="源数据库" size="small">
          <n-descriptions :column="1" size="small">
            <n-descriptions-item label="类型">{{ sourceInfo?.driver || 'SQLite' }}</n-descriptions-item>
            <n-descriptions-item label="表数量">{{ sourceInfo?.tableCount || 0 }}</n-descriptions-item>
            <n-descriptions-item label="记录数">{{ sourceInfo?.recordCount || 0 }}</n-descriptions-item>
            <n-descriptions-item label="预估大小">{{ sourceInfo?.estimatedSize || '0 KB' }}</n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>
      <n-gi>
        <n-card title="目标数据库" size="small">
          <n-descriptions :column="1" size="small">
            <n-descriptions-item label="类型">{{ targetInfo?.driver || 'MySQL' }}</n-descriptions-item>
            <n-descriptions-item label="状态">
              <n-tag v-if="targetInfo?.connected" type="success" size="small">已连接</n-tag>
              <n-tag v-else type="error" size="small">未连接</n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="表数量">{{ targetInfo?.tableCount || 0 }}</n-descriptions-item>
            <n-descriptions-item label="数据库状态">
              <n-tag v-if="targetInfo?.databaseEmpty" type="success" size="small">空库</n-tag>
              <n-tag v-else type="warning" size="small">非空</n-tag>
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>
    </n-grid>

    <n-card title="风险评估" size="small" class="risk-card">
      <template #header-extra>
        <n-tag :type="riskType" size="small">{{ riskLevel || 'low' }}</n-tag>
      </template>
      <n-space vertical>
        <div>预估耗时: {{ estimatedDuration || '未知' }}</div>
        <n-alert v-if="warnings?.length" type="warning" :title="'风险提示: ' + warnings.length + ' 项'">
          <ul class="warnings-list">
            <li v-for="(warning, i) in warnings" :key="i">{{ warning }}</li>
          </ul>
        </n-alert>
      </n-space>
    </n-card>

    <n-checkbox v-model:checked="confirmed" class="confirm-checkbox">
      我已了解迁移风险，确认执行数据库迁移
    </n-checkbox>

    <div class="step-actions">
      <n-button @click="$emit('back')">上一步</n-button>
      <n-button type="warning" :disabled="!confirmed" @click="handleNext">
        开始迁移
      </n-button>
    </div>
  </div>
</template>

<script>
import { ref, computed } from 'vue'
import { NGrid, NGi, NCard, NDescriptions, NDescriptionsItem, NTag, NSpace, NCheckbox, NButton, NAlert } from 'naive-ui'

export default {
  name: 'PreviewStep',

  components: {
    NGrid, NGi, NCard, NDescriptions, NDescriptionsItem, NTag, NSpace, NCheckbox, NButton, NAlert
  },

  props: {
    sourceInfo: Object,
    targetInfo: Object,
    riskLevel: String,
    warnings: Array,
    estimatedDuration: String
  },

  emits: ['next', 'back'],

  setup(props, { emit }) {
    const confirmed = ref(false)

    const riskType = computed(() => {
      switch (props.riskLevel) {
        case 'high': return 'error'
        case 'medium': return 'warning'
        default: return 'success'
      }
    })

    const handleNext = () => {
      if (confirmed.value) {
        emit('next', {
          source: props.sourceInfo,
          target: props.targetInfo,
          riskLevel: props.riskLevel,
          warnings: props.warnings,
          estimatedDuration: props.estimatedDuration
        })
      }
    }

    return {
      confirmed,
      riskType,
      handleNext
    }
  }
}
</script>

<style scoped>
.preview-step {
  padding: 16px 0;
}

.risk-card {
  margin-top: 16px;
}

.warnings-list {
  margin: 8px 0 0 20px;
  padding: 0;
  color: var(--warning-color);
}

.warnings-list li {
  margin-bottom: 4px;
}

.confirm-checkbox {
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