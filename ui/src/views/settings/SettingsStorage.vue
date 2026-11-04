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

        <el-form autocomplete="off" label-width="140px" class="backend-form">
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
              <el-input autocomplete="off" v-model="settings.storageBackend.s3.endpoint" placeholder="如 https://s3.amazonaws.com 或 http://127.0.0.1:9000" />
              <div class="field-hint">S3/MinIO 服务地址，MinIO 通常为 http://主机:9000</div>
            </el-form-item>
            <el-form-item label="AccessKey">
              <el-input autocomplete="off" v-model="settings.storageBackend.s3.accessKey" placeholder="访问密钥 ID" />
            </el-form-item>
            <el-form-item label="SecretKey">
              <el-input autocomplete="new-password" v-model="settings.storageBackend.s3.secretKey" type="password" show-password placeholder="访问密钥" />
            </el-form-item>
            <el-form-item label="Bucket">
              <el-input autocomplete="off" v-model="settings.storageBackend.s3.bucket" placeholder="存储桶名称" />
            </el-form-item>
            <el-form-item label="区域">
              <el-input autocomplete="off" v-model="settings.storageBackend.s3.region" placeholder="如 us-east-1，MinIO 可留空" />
            </el-form-item>
            <el-form-item label="使用 HTTPS">
              <el-switch v-model="settings.storageBackend.s3.useSSL" />
            </el-form-item>
          </template>

          <template v-if="backend === 'oss'">
            <el-form-item label="服务端点">
              <el-input autocomplete="off" v-model="settings.storageBackend.oss.endpoint" placeholder="如 oss-cn-hangzhou.aliyuncs.com" />
            </el-form-item>
            <el-form-item label="AccessKey">
              <el-input autocomplete="off" v-model="settings.storageBackend.oss.accessKey" placeholder="访问密钥 ID" />
            </el-form-item>
            <el-form-item label="SecretKey">
              <el-input autocomplete="new-password" v-model="settings.storageBackend.oss.secretKey" type="password" show-password placeholder="访问密钥" />
            </el-form-item>
            <el-form-item label="Bucket">
              <el-input autocomplete="off" v-model="settings.storageBackend.oss.bucket" placeholder="存储桶名称" />
            </el-form-item>
          </template>

          <template v-if="backend === 'webdav'">
            <el-form-item label="服务地址">
              <el-input autocomplete="off" v-model="settings.storageBackend.webdav.url" placeholder="如 https://example.com/dav" />
            </el-form-item>
            <el-form-item label="用户名">
              <el-input autocomplete="off" v-model="settings.storageBackend.webdav.username" placeholder="可留空" />
            </el-form-item>
            <el-form-item label="密码">
              <el-input autocomplete="new-password" v-model="settings.storageBackend.webdav.password" type="password" show-password placeholder="可留空" />
            </el-form-item>
          </template>

          <el-form-item v-if="backend === 'local'" label="存储位置">
            <div class="field-hint">
              本地文件分别保存在“应用与共享目录”的共享目录、“私有与临时文件”的私有/临时目录下。
            </div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="24" :sm="24" :xs="24">
      <el-card class="backend-card">
        <template #header><span>存储告警</span></template>
        <el-alert
          type="info"
          :closable="false"
          show-icon
          title="根目录配置配额时按配额使用率计算，未配置配额时按所在磁盘使用率计算。"
        >
          <template #default>
            达到阈值后通过所选通道发送告警：Web 消息仅管理员可见（可在管理后台首页查看），
            Webhook 事件为 storage.alert，邮件通过下方 SMTP 配置发送。
          </template>
        </el-alert>

        <el-form autocomplete="off" label-width="140px" class="backend-form">
          <el-form-item label="启用告警">
            <el-switch v-model="settings.storageBackend.alert.enabled" />
            <div class="field-hint">关闭后不再检测存储使用率。</div>
          </el-form-item>
          <el-form-item label="使用率阈值">
            <el-input-number v-model="settings.storageBackend.alert.thresholdPercent" :min="1" :max="100" :step="1" />
            <span class="unit">%</span>
            <div class="field-hint">达到或超过该百分比时触发告警。</div>
          </el-form-item>
          <el-form-item label="检测间隔">
            <el-input-number v-model="settings.storageBackend.alert.checkIntervalMinutes" :min="1" :max="1440" :step="1" />
            <span class="unit">分钟</span>
          </el-form-item>
          <el-form-item label="重复告警间隔">
            <el-input-number v-model="settings.storageBackend.alert.cooldownMinutes" :min="1" :max="10080" :step="1" />
            <span class="unit">分钟</span>
            <div class="field-hint">持续超过阈值时，两次告警之间的最短间隔；恢复后再次越线会立即告警。</div>
          </el-form-item>

          <el-form-item label="通知通道">
            <div class="channel-list">
              <el-checkbox v-model="settings.storageBackend.alert.webEnabled">Web 消息（仅管理员）</el-checkbox>
              <el-checkbox v-model="settings.storageBackend.alert.webhookEnabled">Webhook 推送</el-checkbox>
              <el-checkbox v-model="settings.storageBackend.alert.emailEnabled">邮件通知</el-checkbox>
            </div>
          </el-form-item>

          <template v-if="settings.storageBackend.alert.emailEnabled">
            <el-form-item label="收件人">
              <el-input
                v-model="recipientsText"
                type="textarea"
                :rows="3"
                placeholder="每行一个邮箱地址，也可用逗号分隔"
              />
            </el-form-item>
            <el-form-item label="SMTP 服务器">
              <el-input v-model="settings.storageBackend.alert.smtp.host" placeholder="如 smtp.example.com" />
            </el-form-item>
            <el-form-item label="端口">
              <el-input-number v-model="settings.storageBackend.alert.smtp.port" :min="1" :max="65535" />
              <div class="field-hint">常见：587（STARTTLS）、465（SSL）。</div>
            </el-form-item>
            <el-form-item label="加密方式">
              <el-select v-model="settings.storageBackend.alert.smtp.tlsMode" style="width: 180px">
                <el-option label="STARTTLS" value="starttls" />
                <el-option label="SSL/TLS" value="ssl" />
                <el-option label="不加密" value="none" />
              </el-select>
            </el-form-item>
            <el-form-item label="用户名">
              <el-input v-model="settings.storageBackend.alert.smtp.username" placeholder="可留空" />
            </el-form-item>
            <el-form-item label="密码">
              <el-input
                v-model="settings.storageBackend.alert.smtp.password"
                type="password"
                autocomplete="new-password"
                show-password
                placeholder="可留空"
              />
            </el-form-item>
            <el-form-item label="发件人">
              <el-input v-model="settings.storageBackend.alert.smtp.from" placeholder="留空时使用用户名" />
            </el-form-item>
          </template>
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

// 邮件收件人以多行文本编辑，写回时按换行/逗号/分号拆分
const recipientsText = computed({
  get: () => (props.settings.storageBackend?.alert?.emailRecipients || []).join('\n'),
  set: (value) => {
    props.settings.storageBackend.alert.emailRecipients = value
      .split(/[\n,，;；]+/)
      .map(item => item.trim())
      .filter(Boolean)
  }
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.backend-card {
  margin-bottom: 16px;
}

.backend-form {
  margin-top: 16px;
}

.unit {
  margin-left: 8px;
  color: var(--el-text-color-secondary);
}

.channel-list {
  display: flex;
  flex-wrap: wrap;
  gap: 8px 20px;
}
</style>
