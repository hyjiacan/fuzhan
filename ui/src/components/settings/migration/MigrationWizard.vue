<template>
  <n-modal
    v-model:show="showModal"
    preset="card"
    title="数据库迁移向导"
    style="width: 700px; max-width: 90vw;"
    :mask-closable="false"
    :closable="currentStep < 4"
    @close="handleClose"
  >
    <n-steps :current="currentStep" size="small" class="migration-steps">
      <n-step title="配置" />
      <n-step title="测试连接" />
      <n-step title="预览" />
      <n-step title="迁移" />
      <n-step title="完成" />
    </n-steps>

    <div class="step-content">
      <!-- 步骤1: 配置 -->
      <ConfigStep
        v-if="currentStep === 0"
        :database-type="targetType"
        :initial-config="initialConfig"
        @next="handleConfigNext"
        @cancel="handleClose"
      />

      <!-- 步骤2: 测试连接 -->
      <TestStep
        v-else-if="currentStep === 1"
        :config="targetConfig"
        @next="handleTestNext"
        @back="currentStep = 0"
      />

      <!-- 步骤3: 预览 -->
      <PreviewStep
        v-else-if="currentStep === 2"
        :source-info="sourceInfo"
        :target-info="targetInfo"
        :risk-level="riskLevel"
        :warnings="warnings"
        :estimated-duration="estimatedDuration"
        @next="handlePreviewNext"
        @back="currentStep = 1"
      />

      <!-- 步骤4: 进度 -->
      <ProgressStep
        v-else-if="currentStep === 3"
        :migration-config="targetConfig"
        :event-source="eventSource"
        @complete="handleComplete"
        @error="handleError"
        @cancel="handleCancel"
      />

      <!-- 步骤5: 完成 -->
      <CompleteStep
        v-else-if="currentStep === 4"
        :success="migrationSuccess"
        :message="resultMessage"
        @restart="handleRestart"
        @close="handleClose"
      />
    </div>
  </n-modal>
</template>

<script>
import { ref, defineComponent } from 'vue'
import { NModal, NSteps, NStep } from 'naive-ui'
import ConfigStep from './ConfigStep.vue'
import TestStep from './TestStep.vue'
import PreviewStep from './PreviewStep.vue'
import ProgressStep from './ProgressStep.vue'
import CompleteStep from './CompleteStep.vue'

export default defineComponent({
  name: 'MigrationWizard',

  components: {
    NModal,
    NSteps,
    NStep,
    ConfigStep,
    TestStep,
    PreviewStep,
    ProgressStep,
    CompleteStep
  },

  props: {
    show: {
      type: Boolean,
      default: false
    },
    initialType: {
      type: String,
      default: 'sqlite'
    },
    initialConfig: {
      type: Object,
      default: null
    }
  },

  emits: ['update:show', 'complete', 'cancel'],

  setup(props, { emit }) {
    const showModal = ref(props.show)
    const currentStep = ref(0)
    const targetType = ref(props.initialType)
    const targetConfig = ref({})
    const sourceInfo = ref(null)
    const targetInfo = ref(null)
    const riskLevel = ref('low')
    const warnings = ref([])
    const estimatedDuration = ref('')
    const eventSource = ref(null)
    const migrationSuccess = ref(false)
    const resultMessage = ref('')

    const handleConfigNext = (config) => {
      targetConfig.value = config
      targetType.value = config.driver || props.initialType
      currentStep.value = 1
    }

    const handleTestNext = (info) => {
      targetInfo.value = info
      currentStep.value = 2
    }

    const handlePreviewNext = (result) => {
      sourceInfo.value = result.source
      riskLevel.value = result.riskLevel
      warnings.value = result.warnings || []
      estimatedDuration.value = result.estimatedDuration || ''
      currentStep.value = 3
    }

    const handleComplete = () => {
      migrationSuccess.value = true
      resultMessage.value = '数据库迁移成功完成！'
      currentStep.value = 4
    }

    const handleError = (error) => {
      migrationSuccess.value = false
      resultMessage.value = error || '迁移过程中发生错误'
      currentStep.value = 4
    }

    const handleCancel = () => {
      currentStep.value = 0
      eventSource.value = null
    }

    const handleClose = () => {
      showModal.value = false
      emit('update:show', false)
      emit('cancel')
    }

    const handleRestart = () => {
      // 触发页面刷新
      window.location.reload()
    }

    return {
      showModal,
      currentStep,
      targetType,
      targetConfig,
      sourceInfo,
      targetInfo,
      riskLevel,
      warnings,
      estimatedDuration,
      eventSource,
      migrationSuccess,
      resultMessage,
      handleConfigNext,
      handleTestNext,
      handlePreviewNext,
      handleComplete,
      handleError,
      handleCancel,
      handleClose,
      handleRestart
    }
  }
})
</script>

<style scoped>
.migration-steps {
  margin-bottom: 24px;
}

.step-content {
  min-height: 300px;
}
</style>