<template>
  <el-row :gutter="16">
    <el-col :span="24" :sm="24" :xs="24">
      <el-card class="backend-card">
        <template #header><span>存储后端</span></template>
        <el-alert
          type="warning"
          :closable="false"
          show-icon
          title="修改存储类型或连接参数后需重启服务生效，且不会自动迁移已有文件。"
        >
          <template #default>
            切换后端前，请先按相同的相对路径把已有文件同步到新后端，否则历史文件将无法访问。
            本地磁盘与远端存储的目录配置含义不同：远端后端下目录路径仅作为对象 key 前缀。
          </template>
        </el-alert>

        <el-form label-width="140px" class="backend-form">
          <el-form-item label="存储类型">
            <el-radio-group v-model="settings.storageBackend.backend">
              <el-radio-button label="local">本地磁盘</el-radio-button>
              <el-radio-button label="s3">S3</el-radio-button>
              <el-radio-button label="minio">MinIO</el-radio-button>
              <el-radio-button label="oss">阿里云 OSS</el-radio-button>
              <el-radio-button label="webdav">WebDAV</el-radio-button>
            </el-radio-group>
            <div class="field-hint">默认使用本地磁盘；选择远端后端后由对应服务承载文件读写。</div>
          </el-form-item>

          <template v-if="isS3">
            <el-form-item label="服务端点">
              <el-input v-model="settings.storageBackend.s3.endpoint" placeholder="如 https://s3.amazonaws.com 或 http://127.0.0.1:9000" />
              <div class="field-hint">S3/MinIO 服务地址，MinIO 通常为 http://主机:9000</div>
            </el-form-item>
            <el-form-item label="AccessKey">
              <el-input v-model="settings.storageBackend.s3.accessKey" placeholder="访问密钥 ID" />
            </el-form-item>
            <el-form-item label="SecretKey">
              <el-input v-model="settings.storageBackend.s3.secretKey" type="password" show-password placeholder="访问密钥" />
            </el-form-item>
            <el-form-item label="Bucket">
              <el-input v-model="settings.storageBackend.s3.bucket" placeholder="存储桶名称" />
            </el-form-item>
            <el-form-item label="区域">
              <el-input v-model="settings.storageBackend.s3.region" placeholder="如 us-east-1，MinIO 可留空" />
            </el-form-item>
            <el-form-item label="使用 HTTPS">
              <el-switch v-model="settings.storageBackend.s3.useSSL" />
            </el-form-item>
          </template>

          <template v-if="backend === 'oss'">
            <el-form-item label="服务端点">
              <el-input v-model="settings.storageBackend.oss.endpoint" placeholder="如 oss-cn-hangzhou.aliyuncs.com" />
            </el-form-item>
            <el-form-item label="AccessKey">
              <el-input v-model="settings.storageBackend.oss.accessKey" placeholder="访问密钥 ID" />
            </el-form-item>
            <el-form-item label="SecretKey">
              <el-input v-model="settings.storageBackend.oss.secretKey" type="password" show-password placeholder="访问密钥" />
            </el-form-item>
            <el-form-item label="Bucket">
              <el-input v-model="settings.storageBackend.oss.bucket" placeholder="存储桶名称" />
            </el-form-item>
          </template>

          <template v-if="backend === 'webdav'">
            <el-form-item label="服务地址">
              <el-input v-model="settings.storageBackend.webdav.url" placeholder="如 https://example.com/dav" />
            </el-form-item>
            <el-form-item label="用户名">
              <el-input v-model="settings.storageBackend.webdav.username" placeholder="可留空" />
            </el-form-item>
            <el-form-item label="密码">
              <el-input v-model="settings.storageBackend.webdav.password" type="password" show-password placeholder="可留空" />
            </el-form-item>
          </template>

          <el-form-item v-if="backend === 'local'" label="存储位置">
            <div class="field-hint">
              本地文件分别保存在“应用与共享目录”的共享目录、“私有与临时文件”的私有/临时目录下。
            </div>
          </el-form-item>
        </el-form>
      </el-card>

      <el-card class="index-card">
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
import { computed } from 'vue'

const props = defineProps({
  settings: { type: Object, required: true }
})

// 当前选中的存储后端
const backend = computed(() => props.settings.storageBackend?.backend || 'local')
// S3 与 MinIO 共用同一组连接参数
const isS3 = computed(() => backend.value === 's3' || backend.value === 'minio')
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.el-card {
  transition: transform @transition-smooth, box-shadow @transition-smooth;

  &:hover {
    box-shadow: @shadow-md;
  }
}

.backend-card {
  margin-bottom: 16px;
}

.field-hint {
  color: @text-color-secondary;
  font-size: 12px;
}

.backend-form {
  margin-top: 16px;
}
</style>
