<template>
  <el-row :gutter="16">
    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>WEB 服务</span></template>
        <el-form label-width="120px">
          <el-form-item label="监听地址">
            <el-select v-model="settings.server.host">
              <el-option v-for="opt in ipOptions" :key="opt.value" :label="opt.label" :value="opt.value" />
            </el-select>
            <div class="field-hint">服务器监听的 IP 地址。<code>0.0.0.0</code> 表示监听所有网卡</div>
          </el-form-item>
          <el-divider />
          <span style="font-weight: 600; color: #909399;">HTTP</span>
          <div style="width: 100%;">
            <el-form-item label="启用 HTTP">
              <el-switch v-model="settings.server.http.enabled" />
              <div class="field-hint">关闭后 HTTP 端口将不可用</div>
            </el-form-item>
            <el-form-item label="HTTP 端口" v-if="settings.server.http.enabled">
              <el-input-number v-model="settings.server.http.port" :min="1" :max="65535" />
              <div class="field-hint">HTTP 服务监听端口，常用：8080（开发）、80（生产）</div>
            </el-form-item>
          </div>
          <el-divider />
          <span style="font-weight: 600; color: #909399;">HTTPS</span>
          <div style="width: 100%;">
            <el-form-item label="启用 HTTPS">
              <el-switch v-model="settings.server.https.enabled" />
              <div class="field-hint">启用后可通过 HTTPS 加密访问</div>
            </el-form-item>
            <el-form-item label="HTTPS 端口" v-if="settings.server.https.enabled">
              <el-input-number v-model="settings.server.https.port" :min="1" :max="65535" />
              <div class="field-hint">HTTPS 服务监听端口，默认 8443</div>
            </el-form-item>
          </div>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>FTP 服务</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用 FTP">
            <el-switch v-model="settings.server.ftp.enabled" />
            <div class="field-hint">开启后可通过 FTP 协议访问共享文件</div>
          </el-form-item>
          <el-form-item label="FTP 端口" v-if="settings.server.ftp.enabled">
            <el-input-number v-model="settings.server.ftp.port" :min="1" :max="65535" />
            <div class="field-hint">FTP 端口，默认 21</div>
          </el-form-item>
          <el-form-item label="被动端口起始" v-if="settings.server.ftp.enabled">
            <el-input-number v-model="settings.server.ftp.passivePortStart" :min="1" :max="65535" />
            <div class="field-hint">被动模式数据端口起始，默认 2122</div>
          </el-form-item>
          <el-form-item label="被动端口结束" v-if="settings.server.ftp.enabled">
            <el-input-number v-model="settings.server.ftp.passivePortEnd" :min="1" :max="65535" />
            <div class="field-hint">被动模式数据端口结束，默认 2221</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>FTPS 服务</span></template>
        <el-form label-width="120px">
          <el-form-item label="启用 FTPS">
            <el-switch v-model="settings.server.ftps.enabled" />
            <div class="field-hint">开启后可通过 FTPS（FTP over TLS）安全访问</div>
          </el-form-item>
          <el-form-item label="FTPS 端口" v-if="settings.server.ftps.enabled">
            <el-input-number v-model="settings.server.ftps.port" :min="1" :max="65535" />
            <div class="field-hint">FTPS 端口，默认 990</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>WebDAV 服务</span></template>
        <el-alert type="info" :show-icon="false" :closable="false" style="margin-bottom: 16px;">
          <span class="field-hint">WebDAV 通过 HTTP/HTTPS 端口提供访问，无需额外端口配置。</span>
        </el-alert>
        <el-form label-width="120px">
          <el-form-item label="启用 WebDAV">
            <el-switch v-model="settings.server.webdav.enabled" />
            <div class="field-hint">开启后可通过 WebDAV 客户端浏览文件</div>
          </el-form-item>
          <el-form-item label="公开用户名" v-if="settings.server.webdav.enabled">
            <el-input v-model="settings.account.anonymous.username" placeholder="public" />
            <div class="field-hint">WebDAV/FTP 公开目录的默认用户名</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>

    <el-col :span="12" :sm="12" :xs="24" style="margin-bottom: 16px;">
      <el-card>
        <template #header><span>TLS 证书</span></template>
        <el-alert type="info" :show-icon="false" :closable="false" style="margin-bottom: 16px;">
          <span class="field-hint">TLS 证书由 HTTPS 和 FTPS 共享使用，配置一次即可。</span>
        </el-alert>
        <el-form label-width="120px">
          <el-form-item label="证书文件">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="settings.server.tls.certFile" placeholder="未配置" readonly style="width: 250px;" />
              <el-upload
                :show-file-list="false"
                accept=".pem,.crt"
                :http-request="handleCertUpload"
              >
                <el-button size="small">上传</el-button>
              </el-upload>
            </div>
            <div class="field-hint">上传 .pem 或 .crt 格式的证书文件</div>
          </el-form-item>
          <el-form-item label="密钥文件">
            <div style="display: flex; align-items: center; gap: 12px;">
              <el-input v-model="settings.server.tls.keyFile" placeholder="未配置" readonly style="width: 250px;" />
              <el-upload
                :show-file-list="false"
                accept=".key"
                :http-request="handleKeyUpload"
              >
                <el-button size="small">上传</el-button>
              </el-upload>
            </div>
            <div class="field-hint">上传 .key 格式的密钥文件</div>
          </el-form-item>
        </el-form>
      </el-card>
    </el-col>
  </el-row>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { ElMessage } from 'element-plus'
import { SetupApi, SystemApi } from '@/api'

const props = defineProps({
  settings: { type: Object, required: true }
})

const message = ElMessage
const ipOptions = ref([])

// 加载网络接口列表
const loadNetworkInterfaces = async () => {
  try {
    const res = await SetupApi.getNetworkInterfaces()
    if (res.success && res.data) {
      ipOptions.value = res.data.map(item => ({
        label: `${item.name} (${item.ip})`,
        value: item.ip
      }))
    }
  } catch (e) {
    console.error('获取网络接口失败:', e)
  }
}

// 证书上传处理（el-upload http-request）
const handleCertUpload = async ({ file, onSuccess, onError }) => {
  try {
    const res = await SystemApi.uploadCert(file)
    if (res.success && res.data?.path) {
      props.settings.server.tls.certFile = res.data.path
      message.success('证书上传成功')
    } else {
      message.error(res.message || '证书上传失败')
    }
    onSuccess?.(res)
  } catch (e) {
    message.error('证书上传失败')
    onError?.(e)
  }
}

// 密钥上传处理（el-upload http-request）
const handleKeyUpload = async ({ file, onSuccess, onError }) => {
  try {
    const res = await SystemApi.uploadKey(file)
    if (res.success && res.data?.path) {
      props.settings.server.tls.keyFile = res.data.path
      message.success('密钥上传成功')
    } else {
      message.error(res.message || '密钥上传失败')
    }
    onSuccess?.(res)
  } catch (e) {
    message.error('密钥上传失败')
    onError?.(e)
  }
}

onMounted(() => {
  loadNetworkInterfaces()
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