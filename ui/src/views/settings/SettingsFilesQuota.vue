<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>私有文件配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用私有文件">
            <el-switch v-model="props.settings.privateFiles.enabled" />
            <div class="field-hint">开启后用户需登录才能上传文件</div>
          </el-form-item>
          <el-form-item label="存储路径">
            <el-input v-model="props.settings.privateFiles.path" :maxlength="1024" placeholder="私有文件存储路径" />
            <div class="field-hint">私有文件的存储目录</div>
          </el-form-item>
          <el-form-item label="全局配额">
            <div class="size-input">
              <el-input v-model="privateQuotaGlobalDisplay" :maxlength="32" placeholder="如 500m, 10g, 1t" />
              <span class="size-echo">{{ formatSize(props.settings.privateQuotaGlobal) }}</span>
            </div>
            <div class="field-hint">所有私有文件的总存储上限</div>
          </el-form-item>
          <el-form-item label="用户配额">
            <div class="size-input">
              <el-input v-model="privateQuotaUserDisplay" :maxlength="32" placeholder="如 100m, 5g" />
              <span class="size-echo">{{ formatSize(props.settings.privateQuotaUser) }}</span>
            </div>
            <div class="field-hint">每个用户的私有文件存储上限</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>临时文件配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用临时文件">
            <el-switch v-model="props.settings.tempFiles.enabled" />
            <div class="field-hint">开启后无需登录即可上传文件</div>
          </el-form-item>
          <el-form-item label="存储路径">
            <el-input v-model="props.settings.tempFiles.path" :maxlength="1024" placeholder="临时文件存储路径" />
            <div class="field-hint">临时文件的存储目录</div>
          </el-form-item>
          <el-form-item label="全局配额">
            <div class="size-input">
              <el-input v-model="tempQuotaGlobalDisplay" :maxlength="32" placeholder="如 10g, 100g" />
              <span class="size-echo">{{ formatSize(props.settings.tempFilesQuotaGlobal) }}</span>
            </div>
            <div class="field-hint">所有临时文件的总存储上限</div>
          </el-form-item>
          <el-form-item label="IP 配额">
            <div class="size-input">
              <el-input v-model="tempQuotaPerIPDisplay" :maxlength="32" placeholder="如 500m, 2g" />
              <span class="size-echo">{{ formatSize(props.settings.tempFilesQuotaPerIP) }}</span>
            </div>
            <div class="field-hint">每个 IP 的临时文件存储上限</div>
          </el-form-item>
          <el-form-item label="默认过期天数">
            <el-input-number v-model="props.settings.tempFiles.defaultExpireDays" :min="1" :max="365" />
            <div class="field-hint">临时文件默认的有效天数</div>
          </el-form-item>
          <el-form-item label="下载后删除">
            <el-switch v-model="props.settings.tempFiles.deleteOnDownload" />
            <div class="field-hint">开启后文件被下载一次即自动删除</div>
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

const privateQuotaGlobalDisplay = computed({
  get: () => formatToUnit(props.settings.privateQuotaGlobal),
  set: (val) => { props.settings.privateQuotaGlobal = NumberUtils.parseFileSize(val) }
})
const privateQuotaUserDisplay = computed({
  get: () => formatToUnit(props.settings.privateQuotaUser),
  set: (val) => { props.settings.privateQuotaUser = NumberUtils.parseFileSize(val) }
})
const tempQuotaGlobalDisplay = computed({
  get: () => formatToUnit(props.settings.tempFilesQuotaGlobal),
  set: (val) => { props.settings.tempFilesQuotaGlobal = NumberUtils.parseFileSize(val) }
})
const tempQuotaPerIPDisplay = computed({
  get: () => formatToUnit(props.settings.tempFilesQuotaPerIP),
  set: (val) => { props.settings.tempFilesQuotaPerIP = NumberUtils.parseFileSize(val) }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.field-col-cards {
  margin-bottom: @spacing-md;

  :deep(.el-card) {
    transition: box-shadow @transition-smooth;

    &:hover {
      box-shadow: @shadow-md;
    }
  }
}

.size-input {
  display: flex;
  align-items: center;
  gap: @spacing-md;

  :deep(.el-input) {
    width: 200px;
  }
}

.size-echo {
  color: @text-color-placeholder;
}

.field-hint {
  color: @text-color-secondary;
  font-size: @font-size-xs;
}
</style>