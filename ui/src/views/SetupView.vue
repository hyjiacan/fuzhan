<template>
  <div class="setup-view">
    <n-layout class="setup-layout">
      <n-layout-content class="setup-content">
        <n-card class="setup-card" :bordered="false" content-style="padding: 32px;">
          <template #header>
            <div class="setup-header">
              <span class="setup-icon">⚙️</span>
              <div class="setup-title-group">
                <h2 class="setup-title">{{ form.appName || '轻共享' }}</h2>
                <p class="setup-subtitle">部署向导</p>
              </div>
            </div>
          </template>

          <!-- Basic Config Section -->
          <div class="setup-section">
            <h3 class="section-title">基本配置</h3>
            <n-form ref="formRef" :model="form" label-placement="top" :show-feedback="true">
              <n-grid :cols="1" :x-gap="24">
                <n-gi>
                  <n-form-item label="应用名称" path="appName" :rule="requiredRule">
                    <n-input v-model:value="form.appName" placeholder="请输入应用名称" size="large" />
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label="监听地址" path="host">
                    <n-select v-model:value="form.host" :options="ipOptions" size="large" />
                    <template #feedback>
                      <span class="field-hint">0.0.0.0 表示监听所有网络接口</span>
                    </template>
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label="服务端口" path="port">
                    <n-input-number v-model:value="form.port" :min="1" :max="65535" size="large" class="full-width" />
                    <template #feedback>
                      <span class="field-hint">访问地址: http://{{ form.host === '0.0.0.0' ? 'localhost' : form.host }}:{{ form.port }}</span>
                    </template>
                  </n-form-item>
                </n-gi>
                </n-grid>
            </n-form>
          </div>

          <!-- HTTPS Config Section -->
          <div class="setup-section">
            <h3 class="section-title">HTTPS 服务配置</h3>
            <n-form label-placement="top">
              <n-form-item label="启用 HTTPS">
                <n-switch v-model:value="form.https.enabled" />
                <template #feedback>
                  <span class="field-hint">启用 HTTPS 加密传输，需要上传证书和密钥文件</span>
                </template>
              </n-form-item>

              <n-form-item v-if="form.https.enabled" label="HTTPS 端口">
                <n-input-number v-model:value="form.https.port" :min="1" :max="65535" size="large" class="full-width" />
                <template #feedback>
                  <span class="field-hint">HTTPS 端口，默认 8443。访问地址: https://{{ form.host === '0.0.0.0' ? 'localhost' : form.host }}:{{ form.https.port }}</span>
                </template>
              </n-form-item>

              <template v-if="form.https.enabled">
                <n-grid :cols="2" :x-gap="16">
                  <n-gi>
                    <n-form-item label="证书文件 (.pem)">
                      <n-upload
                        :max="1"
                        accept=".pem,.crt"
                        :custom-request="handleCertUpload"
                        :file-list="certFileList"
                        @update:file-list="handleCertFileListChange"
                      >
                        <n-button>上传证书</n-button>
                      </n-upload>
                      <template #feedback>
                        <span class="field-hint" v-if="form.tls.certFile">当前: {{ form.tls.certFile }}</span>
                        <span class="field-hint" v-else>支持 .pem 或 .crt 格式</span>
                      </template>
                    </n-form-item>
                  </n-gi>
                  <n-gi>
                    <n-form-item label="密钥文件 (.key)">
                      <n-upload
                        :max="1"
                        accept=".key"
                        :custom-request="handleKeyUpload"
                        :file-list="keyFileList"
                        @update:file-list="handleKeyFileListChange"
                      >
                        <n-button>上传密钥</n-button>
                      </n-upload>
                      <template #feedback>
                        <span class="field-hint" v-if="form.tls.keyFile">当前: {{ form.tls.keyFile }}</span>
                        <span class="field-hint" v-else>支持 .key 格式</span>
                      </template>
                    </n-form-item>
                  </n-gi>
                </n-grid>
              </template>
            </n-form>
          </div>

          <!-- Database Config Section -->
          <div class="setup-section">
            <h3 class="section-title">数据库配置</h3>
            <n-form ref="dbFormRef" :model="form" label-placement="top">
              <n-form-item label="数据库类型" path="dbDriver">
                <n-radio-group v-model:value="form.dbDriver" size="large">
                  <n-space>
                    <n-radio value="sqlite">
                      <div class="db-option">
                        <div class="db-option-title">SQLite</div>
                        <div class="db-option-desc">轻量级，无需安装（推荐）</div>
                      </div>
                    </n-radio>
                    <n-radio value="mysql">
                      <div class="db-option">
                        <div class="db-option-title">MySQL</div>
                        <div class="db-option-desc">需要 MySQL 5.7+</div>
                      </div>
                    </n-radio>
                    <n-radio value="postgres">
                      <div class="db-option">
                        <div class="db-option-title">PostgreSQL</div>
                        <div class="db-option-desc">需要 PostgreSQL 10+</div>
                      </div>
                    </n-radio>
                  </n-space>
                </n-radio-group>
              </n-form-item>

              <!-- SQLite 配置 -->
              <n-form-item v-if="form.dbDriver === 'sqlite'" label="数据库文件" path="dsn" :rule="requiredRule">
                <n-input v-model:value="form.dsn" placeholder="fuzhan.db" size="large" />
                <template #feedback>
                  <span class="field-hint">SQLite 数据库文件路径，如 <code>./fuzhan.db</code></span>
                </template>
              </n-form-item>

              <!-- MySQL 配置 -->
              <template v-if="form.dbDriver === 'mysql'">
                <n-grid :cols="2" :x-gap="16">
                  <n-gi>
                    <n-form-item label="主机地址" path="mysqlHost" :rule="requiredRule">
                      <n-input v-model:value="form.mysqlHost" placeholder="localhost" size="large" />
                      <template #feedback><span class="field-hint">MySQL 服务器地址，通常为 <code>localhost</code></span></template>
                    </n-form-item>
                  </n-gi>
                  <n-gi>
                    <n-form-item label="端口" path="mysqlPort">
                      <n-input-number v-model:value="form.mysqlPort" :min="1" :max="65535" size="large" class="full-width" />
                      <template #feedback><span class="field-hint">默认 <code>3306</code></span></template>
                    </n-form-item>
                  </n-gi>
                </n-grid>
                <n-grid :cols="2" :x-gap="16">
                  <n-gi>
                    <n-form-item label="用户名" path="mysqlUser" :rule="requiredRule">
                      <n-input v-model:value="form.mysqlUser" placeholder="root" size="large" />
                      <template #feedback><span class="field-hint">MySQL 数据库用户名</span></template>
                    </n-form-item>
                  </n-gi>
                  <n-gi>
                    <n-form-item label="密码">
                      <n-input v-model:value="form.mysqlPassword" type="password" placeholder="输入密码（可选）" show-password-on="click" size="large" />
                      <template #feedback><span class="field-hint">数据库密码，留空表示无密码</span></template>
                    </n-form-item>
                  </n-gi>
                </n-grid>
                <n-form-item label="数据库名" path="mysqlDatabase" :rule="requiredRule">
                  <n-input v-model:value="form.mysqlDatabase" placeholder="fuzhan" size="large" />
                <template #feedback>
                  <span class="field-hint">
                    需提前创建数据库：<br>
                    <code>CREATE DATABASE fuzhan CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;</code>
                    </span>
                  </template>
                </n-form-item>

                <!-- 测试连接按钮 -->
                <n-form-item>
                  <n-space align="center">
                    <n-button
                      :loading="testingDb"
                      :type="dbTestResult?.success === true ? 'success' : dbTestResult?.success === false ? 'error' : 'default'"
                      @click="testDbConnection"
                    >
                      {{ testingDb ? '测试中...' : dbTestResult ? (dbTestResult.success ? '重新测试' : '重试') : '测试连接' }}
                    </n-button>
                    <span v-if="dbTestResult" class="test-result" :class="dbTestResult.success ? 'success' : 'error'">
                      {{ dbTestResult.success ? '连接成功' : dbTestResult.error }}
                    </span>
                  </n-space>
                </n-form-item>
              </template>

              <!-- PostgreSQL 配置 -->
              <template v-if="form.dbDriver === 'postgres'">
                <n-grid :cols="2" :x-gap="16">
                  <n-gi>
                    <n-form-item label="主机地址" path="postgresHost" :rule="requiredRule">
                      <n-input v-model:value="form.postgresHost" placeholder="localhost" size="large" />
                      <template #feedback><span class="field-hint">PostgreSQL 服务器地址，通常为 <code>localhost</code></span></template>
                    </n-form-item>
                  </n-gi>
                  <n-gi>
                    <n-form-item label="端口" path="postgresPort">
                      <n-input-number v-model:value="form.postgresPort" :min="1" :max="65535" size="large" class="full-width" />
                      <template #feedback><span class="field-hint">默认 <code>5432</code></span></template>
                    </n-form-item>
                  </n-gi>
                </n-grid>
                <n-grid :cols="2" :x-gap="16">
                  <n-gi>
                    <n-form-item label="用户名" path="postgresUser" :rule="requiredRule">
                      <n-input v-model:value="form.postgresUser" placeholder="postgres" size="large" />
                      <template #feedback><span class="field-hint">PostgreSQL 数据库用户名</span></template>
                    </n-form-item>
                  </n-gi>
                  <n-gi>
                    <n-form-item label="密码">
                      <n-input v-model:value="form.postgresPassword" type="password" placeholder="输入密码" show-password-on="click" size="large" />
                      <template #feedback><span class="field-hint">数据库密码</span></template>
                    </n-form-item>
                  </n-gi>
                </n-grid>
                <n-form-item label="数据库名" path="postgresDatabase" :rule="requiredRule">
                  <n-input v-model:value="form.postgresDatabase" placeholder="fuzhan" size="large" />
                  <template #feedback>
                    <span class="field-hint">
                      需提前创建数据库：<br>
                      <code>CREATE DATABASE fuzhan;</code>
                    </span>
                  </template>
                </n-form-item>

                <!-- 测试连接按钮 -->
                <n-form-item>
                  <n-space align="center">
                    <n-button
                      :loading="testingDb"
                      :type="dbTestResult?.success === true ? 'success' : dbTestResult?.success === false ? 'error' : 'default'"
                      @click="testDbConnection"
                    >
                      {{ testingDb ? '测试中...' : dbTestResult ? (dbTestResult.success ? '重新测试' : '重试') : '测试连接' }}
                    </n-button>
                    <span v-if="dbTestResult" class="test-result" :class="dbTestResult.success ? 'success' : 'error'">
                      {{ dbTestResult.success ? '连接成功' : dbTestResult.error }}
                    </span>
                  </n-space>
                </n-form-item>
              </template>
            </n-form>
          </div>

          <!-- Root Dirs Section -->
          <div class="setup-section">
            <h3 class="section-title">共享目录</h3>
            <n-form ref="dirsFormRef" :model="form" label-placement="top">
              <div class="dir-list">
                <div v-for="(dir, index) in form.rootDirs" :key="index" class="dir-item">
                  <div class="dir-header">
                    <span class="dir-index">{{ form.rootDirs.length > 1 ? '目录 ' + (index + 1) : '目录' }}</span>
                    <n-button type="error" quaternary circle size="tiny" @click="removeDir(index)"
                      :disabled="form.rootDirs.length <= 1" class="dir-delete-btn" title="删除">
                      <template #icon><n-icon><CloseIcon /></n-icon></template>
                    </n-button>
                  </div>
                  <n-form-item label="路径" label-placement="top" path-path :show-feedback="false">
                    <n-input v-model:value="dir.path" placeholder="D:\SharedFiles"
                      @blur="() => validateDir(index)" @keyup.enter="() => validateDir(index)" size="large">
                      <template #prefix><n-icon><FolderIcon /></n-icon></template>
                      <template #suffix>
                        <n-icon v-if="dir.validating" class="rotate" size="small">
                          <LoadingIcon />
                        </n-icon>
                        <n-icon v-else-if="dir.validated === true" color="#18a058" size="small">
                          <CheckIcon />
                        </n-icon>
                        <n-icon v-else-if="dir.validated === false" color="#d03050" size="small">
                          <CloseIcon />
                        </n-icon>
                      </template>
                    </n-input>
                    <template #feedback>
                      <div class="dir-feedback" :class="{ 'feedback-error': dir.validated === false }">
                        <template v-if="dir.validating">正在验证目录...</template>
                        <template v-else-if="dir.validated === true">目录有效</template>
                        <template v-else-if="dir.validated === false">{{ dir.error }}</template>
                        <template v-else>按回车或失焦验证目录</template>
                      </div>
                    </template>
                  </n-form-item>
                  <div class="dir-hint">例: D:\SharedFiles 或 /home/user/shared</div>

                  <n-form-item label="显示名称" label-placement="top" path-name :show-feedback="false">
                    <n-input v-model:value="dir.name" placeholder="共享文件" size="large" />
                  </n-form-item>
                  <div class="dir-hint">设置你自己喜欢的名称，比如：张三的共享</div>

                  <n-form-item label="存储配额" label-placement="top" path-quota :show-feedback="false">
                    <n-input v-model:value="dir.quota" placeholder="0" size="large" />
                  </n-form-item>
                  <div class="dir-hint">配额留空或0表示无限制，支持 K / M / G / T 单位</div>
                </div>
                <n-button type="primary" dashed block @click="addDir" size="large" class="add-dir-btn">
                  <template #icon><n-icon><PlusIcon /></n-icon></template>
                  添加共享目录
                </n-button>
              </div>
            </n-form>
          </div>

          <!-- Admin Config Section -->
          <div class="setup-section">
            <h3 class="section-title">管理员配置</h3>
            <n-form ref="adminFormRef" :model="form" label-placement="top">
              <n-grid :cols="2" :x-gap="16">
                <n-gi>
                  <n-form-item label="管理员用户名" path="adminUsername" :rule="requiredRule">
                    <n-input v-model:value="form.adminUsername" placeholder="admin" size="large" />
                    <template #feedback>
                      <span class="field-hint">用于管理后台登录</span>
                    </template>
                  </n-form-item>
                </n-gi>
                <n-gi>
                  <n-form-item label="管理员密码" path="adminPassword" :rule="requiredRule">
                    <n-input v-model:value="form.adminPassword" type="password" placeholder="设置管理员密码" show-password-on="click" size="large" />
                    <template #feedback>
                      <span class="field-hint">请妥善保管</span>
                    </template>
                  </n-form-item>
                </n-gi>
              </n-grid>
            </n-form>
          </div>

          <!-- Error Alert -->
          <n-alert v-if="errorMessage" type="error" :title="errorMessage" class="error-alert" />

          <template #footer>
            <div class="setup-footer">
              <n-button type="primary" size="large" @click="submitConfig" :loading="submitting">
                完成配置
              </n-button>
            </div>
          </template>
        </n-card>
      </n-layout-content>
    </n-layout>
  </div>
</template>

<script setup>
import { ref, reactive, h, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { NLayout, NLayoutContent, NCard, NForm, NFormItem, NInput, NInputNumber, NRadioGroup, NRadio, NButton, NIcon, NAlert, NSpace, NGrid, NGi, NSelect, NSwitch, NUpload, useMessage, useDialog } from 'naive-ui'
import { SetupApi, DatabaseApi, SystemApi } from '@/api'
import { NumberUtils } from '@/utils'
import { formatErrorMessage } from '@/utils/error'

const router = useRouter()
const message = useMessage()
const dialog = useDialog()

// Icons
const PlusIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M19 13h-6v6h-2v-6H5v-2h6V5h2v6h6v2z' })
])
const CheckIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M9 16.17L4.83 12l-1.42 1.41L9 19 21 7l-1.41-1.41z' })
])
const CloseIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M19 6.41L17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z' })
])
const LoadingIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', class: 'rotate' }, [
  h('path', { d: 'M12 4V1L8 5l4 4V6c3.31 0 6 2.69 6 6 0 1.01-.25 1.97-.7 2.8l1.46 1.46C19.54 15.03 20 13.57 20 12c0-4.42-3.58-8-8-8zm0 14c-3.31 0-6-2.69-6-6 0-1.01.25-1.97.7-2.8L5.24 7.74C4.46 8.97 4 10.43 4 12c0 4.42 3.58 8 8 8v3l4-4-4-4v3z' })
])
const FolderIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z' })
])

// 构建 MySQL DSN
const buildMysqlDsn = () => {
  const passwordPart = form.mysqlPassword ? `${form.mysqlPassword}@` : '@'
  return `${form.mysqlUser}:${passwordPart}tcp(${form.mysqlHost}:${form.mysqlPort})/${form.mysqlDatabase}`
}

// 构建 PostgreSQL DSN
const buildPostgresDsn = () => {
  const parts = [
    `host=${form.postgresHost}`,
    `port=${form.postgresPort}`,
    `user=${form.postgresUser}`,
    `dbname=${form.postgresDatabase}`
  ]
  if (form.postgresPassword) {
    parts.push(`password=${form.postgresPassword}`)
  }
  return parts.join(' ')
}

// 测试数据库连接
const testDbConnection = async () => {
  testingDb.value = true
  dbTestResult.value = null

  let dsn = form.dsn
  let driver = form.dbDriver

  if (form.dbDriver === 'mysql') {
    dsn = buildMysqlDsn()
  } else if (form.dbDriver === 'postgres') {
    dsn = buildPostgresDsn()
  }

  try {
    const result = await DatabaseApi.testConnection({ driver, dsn })
    if (result.data?.connected) {
      dbTestResult.value = { success: true, info: result.data }
      message.success('连接成功')
    } else {
      dbTestResult.value = {
        success: false,
        error: result.data?.errorInfo?.message || '连接失败'
      }
      message.error(result.data?.errorInfo?.message || '连接失败')
    }
  } catch (err) {
    dbTestResult.value = { success: false, error: formatErrorMessage(err, '连接失败') }
    message.error(formatErrorMessage(err, '连接测试失败'))
  } finally {
    testingDb.value = false
  }
}

// State
const submitting = ref(false)
const errorMessage = ref('')
const ipOptions = ref([])
const certFileList = ref([])
const keyFileList = ref([])
const testingDb = ref(false)
const dbTestResult = ref(null)
const initialHost = ref('0.0.0.0')
const initialPort = ref(8888)

// 文件上传处理
const handleCertUpload = async ({ file }) => {
  try {
    const res = await SystemApi.uploadCert(file.file)
    if (res.success && res.data?.path) {
      form.tls.certFile = res.data.path
      message.success('证书上传成功')
    } else {
      message.error(res.message || '证书上传失败')
    }
  } catch (e) {
    message.error('证书上传失败')
  }
  return false
}

const handleKeyUpload = async ({ file }) => {
  try {
    const res = await SystemApi.uploadKey(file.file)
    if (res.success && res.data?.path) {
      form.tls.keyFile = res.data.path
      message.success('密钥上传成功')
    } else {
      message.error(res.message || '密钥上传失败')
    }
  } catch (e) {
    message.error('密钥上传失败')
  }
  return false
}

const handleCertFileListChange = (list) => {
  certFileList.value = list
}

const handleKeyFileListChange = (list) => {
  keyFileList.value = list
}

// 获取网络接口列表
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

const form = reactive({
  appName: '',
  host: '',
  port: 0,
  adminUsername: 'admin',
  adminPassword: '',
  dbDriver: 'sqlite',
  dsn: 'fuzhan.db',
  // MySQL 配置
  mysqlHost: 'localhost',
  mysqlPort: 3306,
  mysqlUser: 'root',
  mysqlPassword: '',
  mysqlDatabase: 'fuzhan',
  // PostgreSQL 配置
  postgresHost: 'localhost',
  postgresPort: 5432,
  postgresUser: 'postgres',
  postgresPassword: '',
  postgresDatabase: 'fuzhan',
  // Root dirs
  rootDirs: [{ name: '共享文件', path: '', quota: '0', validating: false, validated: null, error: '' }],
  // HTTPS 配置
  https: {
    enabled: false,
    port: 8443
  },
  // TLS 证书（共享）
  tls: {
    certFile: '',
    keyFile: ''
  }
})

// Validation rules
const requiredRule = { required: true, message: '此项为必填' }

// Directory operations
const addDir = () => form.rootDirs.push({ name: '', path: '', quota: '0', validating: false, validated: null, error: '' })
const removeDir = (index) => {
  const dir = form.rootDirs[index]
  dialog.warning({
    title: '确认删除',
    content: `确定要删除目录「${dir.name || dir.path || '目录 ' + (index + 1)}」吗？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: () => {
      form.rootDirs.splice(index, 1)
    }
  })
}

// Normalize Windows path to forward slashes
const normalizePath = (path) => {
  if (!path) return ''
  return path.replace(/\\/g, '/')
}

// Validate directory via API
const validateDir = async (index) => {
  const dir = form.rootDirs[index]
  if (!dir.path) {
    dir.validated = null
    dir.error = ''
    return
  }

  dir.validating = true
  dir.validated = null
  dir.error = ''

  try {
    const normalizedPath = normalizePath(dir.path)
    const data = await SetupApi.validateDir(normalizedPath)
    if (data.success) {
      dir.validated = true
      if (data.data?.created) {
        message.success('目录已自动创建')
      }
    } else {
      dir.validated = false
      dir.error = data.message || '目录无效'
    }
  } catch (e) {
    dir.validated = false
    dir.error = '验证失败，请检查路径'
  } finally {
    dir.validating = false
  }
}

const submitConfig = async () => {
  errorMessage.value = ''

  // Validate basic config
  if (!form.appName) {
    message.warning('请填写应用名称')
    return
  }

  // Validate admin password
  if (!form.adminPassword) {
    message.warning('请设置管理员密码')
    return
  }

  // Validate directories
  const invalidDirs = form.rootDirs.filter(d => !d.path || d.validated === false)
  if (invalidDirs.length > 0) {
    message.warning('请确保所有目录路径有效')
    return
  }

  submitting.value = true

  try {
    // 构建数据库 DSN
    let dsn = form.dsn
    if (form.dbDriver === 'mysql') {
      const passwordPart = form.mysqlPassword ? `${form.mysqlPassword}@` : '@'
      dsn = `${form.mysqlUser}:${passwordPart}tcp(${form.mysqlHost}:${form.mysqlPort})/${form.mysqlDatabase}?charset=utf8mb4&parseTime=True&loc=Local`
    } else if (form.dbDriver === 'postgres') {
      const parts = [
        `host=${form.postgresHost}`,
        `port=${form.postgresPort}`,
        `user=${form.postgresUser}`,
        `dbname=${form.postgresDatabase}`
      ]
      if (form.postgresPassword) {
        parts.push(`password=${form.postgresPassword}`)
      }
      dsn = parts.join(' ') + ' sslmode=disable'
    }

    const config = {
      appName: form.appName,
      host: form.host,
      port: form.port,
      httpsEnabled: form.https.enabled,
      httpsPort: form.https.port,
      tlsCertFile: form.tls.certFile,
      tlsKeyFile: form.tls.keyFile,
      adminUsername: form.adminUsername,
      adminPassword: form.adminPassword,
      rootDirs: form.rootDirs.filter(d => d.path).map(d => ({
        name: d.name || '共享文件',
        path: d.path,
        quota: NumberUtils.parseFileSize(d.quota)
      })),
      database: {
        driver: form.dbDriver,
        dsn: dsn
      },
    }

    const data = await SetupApi.save(config)

    if (data.success) {
      showRestartDialog()
    } else {
      errorMessage.value = data.message || '配置保存失败'
    }
  } catch (e) {
    errorMessage.value = '配置保存失败，请检查网络连接'
  } finally {
    submitting.value = false
  }
}

// 等待服务就绪后导航
const pollServerHealth = (url, maxRetries, interval, onReady) => {
  let retries = 0
  const check = () => {
    retries++
    // 同源下试图访问 API
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 3000)
    fetch(url + '/api/v1/health', { mode: 'no-cors', signal: controller.signal })
      .then(() => { clearTimeout(timeoutId); onReady() })
      .catch(() => {
        clearTimeout(timeoutId)
        if (retries < maxRetries) {
          setTimeout(check, interval)
        } else {
          // 超时后仍然尝试导航
          onReady()
        }
      })
  }
  check()
}

// 提交成功后的强提醒对话框
const showRestartDialog = () => {
  const hostChanged = form.host !== initialHost.value
  const portChanged = form.port !== initialPort.value
  if (hostChanged || portChanged) {
    const displayHost = form.host === '0.0.0.0' ? window.location.hostname : form.host
    const newUrl = 'http://' + displayHost + ':' + form.port
    dialog.warning({
      title: '服务器地址已变更',
      content: '\u670d\u52a1\u5df2\u5207\u6362\u5230\u65b0\u5730\u5740\uff1a' + newUrl,
      positiveText: '前往新地址',
      negativeText: '\u7559\u5728\u5f53\u524d\u9875\u9762',
      maskClosable: false,
      closable: false,
      onPositiveClick: () => {
        // 等待新服务端口就绪后再导航
        pollServerHealth(newUrl, 20, 500, () => {
          window.location.href = newUrl
        })
      },
      onNegativeClick: () => {
        router.push('/')
      }
    })
  } else {
    // \u7aef\u53e3\u672a\u53d8\u5316\uff0c\u76f4\u63a5\u8df3\u8f6c\u9996\u9875
    router.push('/')
  }

}
// Load existing config on mount
onMounted(async () => {
  // 加载默认配置
  try {
    const defaultConfig = await SetupApi.getDefaultConfig()
    if (defaultConfig.success && defaultConfig.data) {
      const cfg = defaultConfig.data
      if (cfg.appName) form.appName = cfg.appName
      if (cfg.host) form.host = cfg.host
      if (cfg.port) form.port = cfg.port
      initialHost.value = form.host || '0.0.0.0'
      initialPort.value = form.port || 8888
      if (cfg.dbDriver) form.dbDriver = cfg.dbDriver
      if (cfg.dsn) form.dsn = cfg.dsn
    }
  } catch (e) {
    console.error('Failed to load default config:', e)
  }

  // 加载网络接口列表
  loadNetworkInterfaces()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.setup-view {
  .rotate {
    animation: rotate 1s linear infinite;
  }

  @keyframes rotate {
    from { transform: rotate(0deg); }
    to { transform: rotate(360deg); }
  }
}

.setup-layout {
  height: 100vh;
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.setup-content {
  padding: 40px;
  overflow-y: auto;
}

.setup-card {
  width: 100%;
  max-width: 920px;
  margin: 0 auto;
  border-radius: 16px;
}

.setup-header {
  display: flex;
  align-items: center;
  gap: 16px;

  .setup-icon {
    font-size: 48px;
  }

  .setup-title-group {
    .setup-title {
      margin: 0;
      color: #333;
      font-size: 24px;
      font-weight: 600;
    }

    .setup-subtitle {
      margin: 4px 0 0;
      color: #666;
      font-size: 14px;
    }
  }
}

.setup-section {
  margin-bottom: 32px;
  padding-bottom: 32px;
  border-bottom: 1px solid #eee;

  &:last-of-type {
    border-bottom: none;
    margin-bottom: 0;
  }

  .section-title {
    margin: 0 0 16px;
    font-size: 18px;
    font-weight: 600;
    color: #333;
    padding-left: 12px;
    border-left: 3px solid @primary-color;
  }
}

.db-option {
  .db-option-title {
    font-weight: 500;
  }

  .db-option-desc {
    font-size: 12px;
    color: #999;
  }
}

.field-hint {
  color: #999;
  font-size: 12px;

  code {
    background: #f0f0f0;
    padding: 2px 4px;
    border-radius: 2px;
  }
}

.full-width {
  width: 100%;
}

.dir-list {
  flex: 1;
}

.dir-item {
  margin-bottom: 16px;
  padding: 16px;
  background: #f8f9fa;
  border-radius: 12px;

  .dir-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 12px;

    .dir-index {
      font-weight: 500;
      color: #333;
    }
  }

  .n-form-item {
    margin-bottom: 12px;
  }

  .dir-feedback {
    color: #999;
    font-size: 12px;

    &.feedback-error {
      color: #d03050;
    }
  }

  .dir-hint {
    color: #999;
    font-size: 12px;
    margin-top: -4px;
    margin-bottom: 12px;
  }
}

.dir-delete-btn {
  flex-shrink: 0;
}

.add-dir-btn {
  margin-top: 8px;
}

.error-alert {
  margin-top: 20px;
}

.setup-footer {
  display: flex;
  justify-content: center;
}

.test-result {
  margin-left: 12px;
  font-size: 14px;

  &.success {
    color: #52c41a;
  }

  &.error {
    color: #ff4d4f;
  }
}

@media @tablet {
  .setup-content {
    padding: 24px 16px;
  }

  .setup-section {
    .n-grid {
      // 2列在小屏变1列
    }
  }
}

@media @mobile {
  .setup-content {
    padding: 16px 8px;
  }

  .setup-card {
    border-radius: 8px;
  }

  .setup-header {
    flex-direction: column;
    text-align: center;
  }

  .dir-item {
    padding: 12px;
  }
}
</style>