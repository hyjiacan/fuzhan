<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="12" :xs="24">
      <el-card>
        <template #header><span>文件索引配置</span></template>
        <el-form label-width="140px">
          <el-form-item label="扫描启动延迟">
            <el-input-number v-model="settings.scanStartDelaySeconds" :min="0" :step="1" style="width: 200px;" />
            <div class="field-hint">启动后延迟执行首次扫描（秒），默认 10</div>
          </el-form-item>
          <el-form-item label="定时扫描间隔">
            <el-input v-model="settings.scanCronExpression" placeholder="如 0 1 * * *（每天凌晨1点）" />
            <div class="field-hint">Cron 表达式，默认 <code>0 1 * * *</code>（每天凌晨 1:00）</div>
          </el-form-item>
          <el-form-item label="检索索引对齐间隔">
            <el-input v-model="settings.searchReconcileCronExpression" placeholder="如 0 5 * * *（每天凌晨5点）" />
            <div class="field-hint">定时校正检索索引与文件表对齐，默认 <code>0 5 * * *</code>（每天凌晨 5:00，与定时扫描错开）</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>
  </el-row>
</template>

<script setup>
defineProps({
  settings: { type: Object, required: true }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.el-card {
  transition: transform @transition-smooth, box-shadow @transition-smooth;

  &:hover {
    box-shadow: @shadow-md;
  }
}

.field-hint {
  color: var(--el-text-color-placeholder);
  font-size: 12px;
}
</style>