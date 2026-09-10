<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="12" :xs="24">
      <el-card>
        <template #header><span>预览配置</span></template>
        <el-form label-width="140px">
          <el-form-item label="MIME 类型">
            <el-input
              v-model="settings.preview.allowMimes"
              :maxlength="1024"
              type="textarea"
              placeholder="text/*,image/*,application/pdf,application/json"
              :rows="2"
            />
            <div class="field-hint">允许预览的 MIME 类型，逗号分隔，支持通配符</div>
          </el-form-item>
          <el-form-item label="文件扩展名">
            <el-input
              v-model="settings.preview.allowExts"
              :maxlength="1024"
              type="textarea"
              placeholder="txt,md,log,json,html,css,js"
              :rows="2"
            />
            <div class="field-hint">允许预览的文件扩展名，逗号分隔</div>
          </el-form-item>
          <el-form-item label="最大内联大小">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="maxInlineSizeDisplay" :maxlength="32" placeholder="如 1m, 2m" style="width: 200px;" />
              <span style="color: #999;">{{ formatSize(settings.preview.maxInlineSize) }}</span>
            </div>
            <div class="field-hint">浏览器直接预览的文件大小上限</div>
          </el-form-item>
          <el-form-item label="文本分块大小">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="textChunkSizeDisplay" placeholder="如 100k, 200k" style="width: 200px;" />
              <span style="color: #999;">{{ formatSize(settings.preview.textChunkSize) }}</span>
            </div>
            <div class="field-hint">预览大文本文件时分块读取的大小</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>
  </el-row>
</template>

<script setup>
import { computed } from 'vue'
import { NumberUtils } from '@/utils'
import { formatToUnit, formatSize } from './useSettingsUtils'

const props = defineProps({
  settings: { type: Object, required: true }
})

const maxInlineSizeDisplay = computed({
  get: () => formatToUnit(props.settings.preview.maxInlineSize),
  set: (val) => { props.settings.preview.maxInlineSize = NumberUtils.parseFileSize(val) }
})
const textChunkSizeDisplay = computed({
  get: () => formatToUnit(props.settings.preview.textChunkSize),
  set: (val) => { props.settings.preview.textChunkSize = NumberUtils.parseFileSize(val) }
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
  color: #999;
  font-size: 12px;
}
</style>