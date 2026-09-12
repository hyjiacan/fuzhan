<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>应用信息</span></template>
        <el-form label-width="120px">
          <el-form-item label="应用名称">
            <el-input v-model="settings.appName" :maxlength="64" placeholder="请输入应用名称" />
            <div class="field-hint">显示在页面标题和界面顶部的应用名称</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>共享目录配置</span></template>
        <div v-for="(dir, index) in settings.rootDirs" :key="index" style="margin-bottom: 12px; padding: 12px; background: #f8f9fa; border-radius: 8px;">
          <el-form label-width="100px">
            <el-form-item label="目录路径">
              <el-input v-model="dir.path" :maxlength="1024" placeholder="目录路径" />
              <div class="field-hint">共享目录的绝对路径或相对路径</div>
            </el-form-item>
            <el-form-item label="显示名称">
              <el-input v-model="dir.name" :maxlength="255" placeholder="显示名称" />
              <div class="field-hint">在界面上显示的目录名称</div>
            </el-form-item>
          </el-form>
          <el-button type="danger" size="small" @click="removeDir(index)">删除</el-button>
        </div>
        <el-button plain block @click="addDir">添加共享目录</el-button>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>私有文件配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用私有文件">
            <el-switch v-model="settings.privateFiles.enabled" />
            <div class="field-hint">开启后用户需登录才能上传文件</div>
          </el-form-item>
          <el-form-item label="存储路径">
            <el-input v-model="settings.privateFiles.path" :maxlength="1024" placeholder="私有文件存储路径" />
            <div class="field-hint">私有文件的存储目录</div>
          </el-form-item>
          <el-form-item label="全局配额">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="privateQuotaGlobalDisplay" :maxlength="32" placeholder="如 500m, 10g, 1t" style="width: 200px;" />
              <span style="color: #999;">{{ formatSize(settings.privateQuotaGlobal) }}</span>
            </div>
            <div class="field-hint">所有私有文件的总存储上限</div>
          </el-form-item>
          <el-form-item label="用户配额">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="privateQuotaUserDisplay" :maxlength="32" placeholder="如 100m, 5g" style="width: 200px;" />
              <span style="color: #999;">{{ formatSize(settings.privateQuotaUser) }}</span>
            </div>
            <div class="field-hint">每个用户的私有文件存储上限</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>临时文件配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用临时文件">
            <el-switch v-model="settings.tempFiles.enabled" />
            <div class="field-hint">开启后无需登录即可上传文件</div>
          </el-form-item>
          <el-form-item label="存储路径">
            <el-input v-model="settings.tempFiles.path" :maxlength="1024" placeholder="临时文件存储路径" />
            <div class="field-hint">临时文件的存储目录</div>
          </el-form-item>
          <el-form-item label="全局配额">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="tempQuotaGlobalDisplay" :maxlength="32" placeholder="如 10g, 100g" style="width: 200px;" />
              <span style="color: #999;">{{ formatSize(settings.tempFilesQuotaGlobal) }}</span>
            </div>
            <div class="field-hint">所有临时文件的总存储上限</div>
          </el-form-item>
          <el-form-item label="IP 配额">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="tempQuotaPerIPDisplay" :maxlength="32" placeholder="如 500m, 2g" style="width: 200px;" />
              <span style="color: #999;">{{ formatSize(settings.tempFilesQuotaPerIP) }}</span>
            </div>
            <div class="field-hint">每个 IP 的临时文件存储上限</div>
          </el-form-item>
          <el-form-item label="默认过期天数">
            <el-input-number v-model="settings.tempFiles.defaultExpireDays" :min="1" :max="365" />
            <div class="field-hint">临时文件默认的有效天数</div>
          </el-form-item>
          <el-form-item label="下载后删除">
            <el-switch v-model="settings.tempFiles.deleteOnDownload" />
            <div class="field-hint">开启后文件被下载一次即自动删除</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>上传配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="分片大小">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="chunkSizeDisplay" :maxlength="32" placeholder="如 10m, 1g" style="width: 200px;" />
              <span style="color: #999;">{{ formatSize(settings.upload.chunkSize) }}</span>
            </div>
            <div class="field-hint">文件分块上传的块大小，建议 5m-10m</div>
          </el-form-item>
          <el-form-item label="最大文件大小">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="maxFileSizeDisplay" :maxlength="32" placeholder="如 2g, 10g, 无限制" style="width: 200px;" />
              <span style="color: #999;">
                {{ settings.upload.maxFileSize === 0 ? '无限制' : formatSize(settings.upload.maxFileSize) }}
              </span>
            </div>
            <div class="field-hint">允许上传的单文件最大体积，0 表示不限制</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>URL 上传配置</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用 URL 上传">
            <el-switch v-model="settings.upload.urlUpload.enabled" />
            <div class="field-hint">开启后可通过远程 URL 下载文件到服务器</div>
          </el-form-item>
          <el-form-item label="跳过证书验证">
            <el-switch v-model="settings.upload.urlUpload.insecureSkipVerify" />
            <div class="field-hint">跳过 HTTPS/FTPS 的 TLS 证书验证</div>
          </el-form-item>
          <el-form-item label="允许 IP 网段">
            <el-input v-model="allowedIPRangesDisplay" type="textarea" :rows="3" :maxlength="2000"
              placeholder="每行一个 CIDR 网段，如 10.0.0.0/8、1.2.3.4/32" style="width: 100%;" />
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

// 单位输入的双向绑定
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

const addDir = () => {
  props.settings.rootDirs.push({ path: '', name: '' })
}

const removeDir = (index) => {
  props.settings.rootDirs.splice(index, 1)
}
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