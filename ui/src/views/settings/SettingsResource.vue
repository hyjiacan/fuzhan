<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>服务器资源监控配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用监控">
            <el-switch v-model="settings.resource.enabled" />
            <div class="field-hint">开启后在管理「资源监控」页展示服务器整体与本程序的 CPU、内存、磁盘占用、磁盘 IO</div>
          </el-form-item>
          <el-form-item label="实时采样间隔">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input-number v-model="settings.resource.samplingInterval" :min="1" :max="60" />
              <span>秒</span>
            </div>
            <div class="field-hint">实时曲线按此频率在内存采样刷新（不落库）</div>
          </el-form-item>
          <el-form-item label="历史采集间隔">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input-number v-model="settings.resource.collectInterval" :min="10" :max="3600" />
              <span>秒</span>
            </div>
            <div class="field-hint">历史数据按此间隔落库，默认 60 秒（每分钟一条）</div>
          </el-form-item>
          <el-form-item label="历史保留天数">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input-number v-model="settings.resource.retentionDays" :min="1" :max="365" />
              <span>天</span>
            </div>
            <div class="field-hint">超出保留期的历史数据将被定期清理，默认 7 天</div>
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