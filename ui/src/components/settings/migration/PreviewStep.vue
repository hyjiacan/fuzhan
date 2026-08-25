<template>
  <div class="preview-step">
    <el-row :gutter="16">
      <el-col :span="12">
        <el-card>
          <template #header><span>源数据库</span></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item label="类型">{{ sourceInfo?.driver || 'SQLite' }}</el-descriptions-item>
            <el-descriptions-item label="表数量">{{ sourceInfo?.tableCount || 0 }}</el-descriptions-item>
            <el-descriptions-item label="记录数">{{ sourceInfo?.recordCount || 0 }}</el-descriptions-item>
            <el-descriptions-item label="预估大小">{{ sourceInfo?.estimatedSize || '0 KB' }}</el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
      <el-col :span="12">
        <el-card>
          <template #header><span>目标数据库</span></template>
          <el-descriptions :column="1" size="small">
            <el-descriptions-item label="类型">{{ targetInfo?.driver || 'MySQL' }}</el-descriptions-item>
            <el-descriptions-item label="状态">
              <el-tag v-if="targetInfo?.connected" type="success" size="small">已连接</el-tag>
              <el-tag v-else type="error" size="small">未连接</el-tag>
            </el-descriptions-item>
            <el-descriptions-item label="表数量">{{ targetInfo?.tableCount || 0 }}</el-descriptions-item>
            <el-descriptions-item label="数据库状态">
              <el-tag v-if="targetInfo?.databaseEmpty" type="success" size="small">空库</el-tag>
              <el-tag v-else type="warning" size="small">非空</el-tag>
            </el-descriptions-item>
          </el-descriptions>
        </el-card>
      </el-col>
    </el-row>

    <el-card class="risk-card">
      <template #header>
        <span>风险评估</span>
        <el-tag :type="riskType" size="small">{{ riskLevel || 'low' }}</el-tag>
      </template>
      <el-space direction="vertical" fill>
        <div>预估耗时: {{ estimatedDuration || '未知' }}</div>
        <el-alert v-if="warnings?.length" type="warning" :title="'风险提示: ' + warnings.length + ' 项'" :closable="false">
          <ul class="warnings-list">
            <li v-for="(warning, i) in warnings" :key="i">{{ warning }}</li>
          </ul>
        </el-alert>
      </el-space>
    </el-card>

    <el-checkbox v-model="confirmed" class="confirm-checkbox">
      我已了解迁移风险，确认执行数据库迁移
    </el-checkbox>

    <div class="step-actions">
      <el-button @click="$emit('back')">上一步</el-button>
      <el-button type="warning" :disabled="!confirmed" @click="handleNext">
        开始迁移
      </el-button>
    </div>
  </div>
</template>

<script>
import { ref, computed } from 'vue'

export default {
  name: 'PreviewStep',

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