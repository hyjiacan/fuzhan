<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>上传配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="分片大小">
            <div class="size-input">
              <el-input v-model="chunkSizeDisplay" :maxlength="32" placeholder="如 10m, 1g" />
              <span class="size-echo">{{ formatSize(props.settings.upload.chunkSize) }}</span>
            </div>
            <div class="field-hint">文件分块上传的块大小，建议 5m-10m</div>
          </el-form-item>
          <el-form-item label="最大文件大小">
            <div class="size-input">
              <el-input v-model="maxFileSizeDisplay" :maxlength="32" placeholder="如 2g, 10g, 无限制" />
              <span class="size-echo">
                {{ props.settings.upload.maxFileSize === 0 ? '无限制' : formatSize(props.settings.upload.maxFileSize) }}
              </span>
            </div>
            <div class="field-hint">允许上传的单文件最大体积，0 表示不限制</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="24" class="field-col-cards">
      <el-card>
        <template #header><span>URL 上传配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用 URL 上传">
            <el-switch v-model="props.settings.upload.urlUpload.enabled" />
            <div class="field-hint">开启后可通过远程 URL 下载文件到服务器</div>
          </el-form-item>
          <el-form-item label="跳过证书验证">
            <el-switch v-model="props.settings.upload.urlUpload.insecureSkipVerify" />
            <div class="field-hint">跳过 HTTPS/FTPS 的 TLS 证书验证</div>
          </el-form-item>
          <el-form-item label="允许 IP 网段">
            <el-input v-model="allowedIPRangesDisplay" type="textarea" :rows="3" :maxlength="2000"
              placeholder="每行一个 CIDR 网段，如 10.0.0.0/8、1.2.3.4/32" />
            <div class="field-hint">URL 上传允许解析到哪些目标网段（SSRF 防护），每行一个，留空表示不限制（允许所有 IP）</div>
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

const chunkSizeDisplay = computed({
  get: () => formatToUnit(props.settings.upload.chunkSize),
  set: (val) => { props.settings.upload.chunkSize = NumberUtils.parseFileSize(val) }
})
const maxFileSizeDisplay = computed({
  get: () => props.settings.upload.maxFileSize === 0 ? '无限制' : formatToUnit(props.settings.upload.maxFileSize),
  set: (val) => {
    if (val === '无限制' || val === '0') {
      props.settings.upload.maxFileSize = 0
    } else {
      props.settings.upload.maxFileSize = NumberUtils.parseFileSize(val)
    }
  }
})
const allowedIPRangesDisplay = computed({
  get: () => (props.settings.upload.urlUpload.allowedIPRanges || []).join('\n'),
  set: (val) => {
    props.settings.upload.urlUpload.allowedIPRanges = String(val).split('\n')
      .map(s => s.trim()).filter(s => s.length > 0)
  }
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