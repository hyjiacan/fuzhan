<template>
  <div class="settings-view">
    <div class="header-section">
      <h2>系统设置</h2>
      <p class="description">管理系统配置和参数调整</p>
    </div>

    <el-tabs v-model="activeTab">
      <!-- 基本信息 -->
      <el-tab-pane name="basic" label="基本信息">
        <SettingsBasic :settings="settings" />
      </el-tab-pane>

      <!-- 服务配置 -->
      <el-tab-pane name="service" label="服务配置">
        <SettingsService :settings="settings" />
      </el-tab-pane>

      <!-- 存储配置 -->
      <el-tab-pane name="storage" label="存储配置">
        <SettingsStorage :settings="settings" />
      </el-tab-pane>

      <!-- 预览配置 -->
      <el-tab-pane name="preview" label="预览配置">
        <SettingsPreview :settings="settings" />
      </el-tab-pane>

      <!-- 数据库 -->
      <el-tab-pane name="database" label="数据库">
        <SettingsDatabase :settings="settings" />
      </el-tab-pane>

      <!-- 访问控制 -->
      <el-tab-pane name="access" label="访问控制">
        <SettingsAccess :settings="settings" />
      </el-tab-pane>

      <!-- 资源监控 -->
      <el-tab-pane name="resource" label="资源监控">
        <SettingsResource :settings="settings" />
      </el-tab-pane>
    </el-tabs>

    <div class="actions-bar">
      <div class="actions-inner">
        <el-button @click="loadSettings">重置</el-button>
        <el-button type="primary" :loading="saving" @click="saveSettings">保存配置</el-button>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { NumberUtils } from '@/utils'
import { ConfigApi, SystemApi } from '@/api'
import store from '@/store'
import SettingsBasic from './settings/SettingsBasic.vue'
import SettingsService from './settings/SettingsService.vue'
import SettingsStorage from './settings/SettingsStorage.vue'
import SettingsPreview from './settings/SettingsPreview.vue'
import SettingsDatabase from './settings/SettingsDatabase.vue'
import SettingsAccess from './settings/SettingsAccess.vue'
import SettingsResource from './settings/SettingsResource.vue'
import { formatToUnit } from './settings/useSettingsUtils'

const message = ElMessage
const dialog = {
  warning: (opts) => ElMessageBox.confirm(opts.content, opts.title, {
    confirmButtonText: opts.positiveText || '确定',
    cancelButtonText: opts.negativeText || '取消',
    type: 'warning',
    closeOnClickModal: !!opts.onMaskClick
  }).then(() => { opts.onPositiveClick?.() }).catch(() => { opts.onNegativeClick?.() })
}
const saving = ref(false)
const activeTab = ref('basic')

// 保存原始服务器配置用于变更检测
const originalServerConfig = ref(null)

const settings = reactive({
  appName: '',
  account: {
    anonymous: { username: 'public' },
    reservedUsernames: []
  },
  server: {
    host: '0.0.0.0',
    http: { enabled: true, port: 8080 },
    https: { enabled: false, port: 8443 },
    ftp: { enabled: false, port: 21, passivePortStart: 2122, passivePortEnd: 2221 },
    ftps: { enabled: false, port: 990 },
    webdav: { enabled: false },
    tls: { certFile: '', keyFile: '' }
  },
  database: {
    driver: 'sqlite',
    dsn: '',
    mysqlHost: 'localhost',
    mysqlPort: 3306,
    mysqlUser: 'root',
    mysqlPassword: '',
    mysqlDatabase: 'fuzhan',
    postgresHost: 'localhost',
    postgresPort: 5432,
    postgresUser: 'postgres',
    postgresPassword: '',
    postgresDatabase: 'fuzhan',
  },
  rootDirs: [],
  allowedExtensions: [],
  privateFiles: {
    enabled: false,
    path: ''
  },
  privateQuotaGlobal: 0,
  privateQuotaUser: 0,
  upload: {
    chunkSize: 10485760,
    maxFileSize: 17179869184,
    urlUpload: { enabled: false, allowedIPRanges: [], insecureSkipVerify: false }
  },
  download: {
    rateLimit: { windowMinutes: 1, maxRequests: 0, lockAfter: 0, lockMinutes: 10 }
  },
  tempFiles: {
    enabled: true,
    path: '',
    defaultExpireDays: 7,
    deleteOnDownload: true
  },
  tempFilesQuotaGlobal: 0,
  tempFilesQuotaPerIP: 0,
  preview: {
    allowMimes: '',
    allowExts: '',
    maxInlineSize: 1048576,
    textChunkSize: 102400
  },
  openApi: {
    enabled: false,
    ipAccessMode: 'none',
    ipWhitelist: '',
    ipBlacklist: '',
    rateLimitEnabled: true,
    requestsPerMinute: 60
  },
  scanStartDelaySeconds: 10,
  scanCronExpression: '0 1 * * *',
  searchReconcileCronExpression: '0 5 * * *',
  security: {
    trustProxy: false,
    allowedOrigins: []
  },
  resource: {
    enabled: false,
    samplingInterval: 5,
    collectInterval: 60,
    retentionDays: 7
  },
})

// 解析 MySQL DSN
const parseMysqlDsn = (dsn) => {
  const match = dsn.match(/([^:@]+):([^@]*)@tcp\(([^:]+):(\d+)\)\/([^?]+)/)
  if (match) {
    settings.database.mysqlUser = match[1]
    settings.database.mysqlPassword = match[2]
    settings.database.mysqlHost = match[3]
    settings.database.mysqlPort = parseInt(match[4])
    settings.database.mysqlDatabase = match[5]
  }
}

// 解析 PostgreSQL DSN
const parsePostgresDsn = (dsn) => {
  const getParam = (str, key) => {
    const match = str.match(new RegExp(`${key}=(\\S+)`))
    return match ? match[1] : ''
  }
  settings.database.postgresHost = getParam(dsn, 'host') || 'localhost'
  settings.database.postgresPort = parseInt(getParam(dsn, 'port')) || 5432
  settings.database.postgresUser = getParam(dsn, 'user') || 'postgres'
  settings.database.postgresPassword = getParam(dsn, 'password')
  settings.database.postgresDatabase = getParam(dsn, 'dbname') || 'fuzhan'
}

// 构建 MySQL DSN
const buildMysqlDsn = () => {
  const { mysqlHost, mysqlPort, mysqlUser, mysqlPassword, mysqlDatabase } = settings.database
  const passwordPart = mysqlPassword ? `${mysqlPassword}@` : '@'
  return `${mysqlUser}:${passwordPart}tcp(${mysqlHost}:${mysqlPort})/${mysqlDatabase}`
}

// 构建 PostgreSQL DSN
const buildPostgresDsn = () => {
  const { postgresHost, postgresPort, postgresUser, postgresPassword, postgresDatabase } = settings.database
  const parts = [
    `host=${postgresHost}`,
    `port=${postgresPort}`,
    `user=${postgresUser}`,
    `dbname=${postgresDatabase}`
  ]
  if (postgresPassword) {
    parts.push(`password=${postgresPassword}`)
  }
  return parts.join(' ')
}

const loadSettings = async () => {
  try {
    const data = await ConfigApi.get()
    if (data.success && data.data) {
      const cfg = data.data
      settings.appName = cfg.app?.name || ''
      settings.server.host = cfg.server?.host || '0.0.0.0'
      settings.server.http = {
        enabled: cfg.server?.http?.enabled ?? true,
        port: cfg.server?.http?.port || 8080
      }
      settings.server.https = {
        enabled: cfg.server?.https?.enabled ?? false,
        port: cfg.server?.https?.port || 8443
      }
      settings.server.ftp = {
        enabled: cfg.server?.ftp?.enabled ?? false,
        port: cfg.server?.ftp?.port || 21,
        passivePortStart: cfg.server?.ftp?.passivePortStart || 2122,
        passivePortEnd: cfg.server?.ftp?.passivePortEnd || 2221
      }
      settings.server.ftps = {
        enabled: cfg.server?.ftps?.enabled ?? false,
        port: cfg.server?.ftps?.port || 990
      }
      settings.account = {
        anonymous: { username: cfg.account?.anonymous?.username || 'public' },
        reservedUsernames: cfg.account?.reservedUsernames || []
      }
      settings.server.webdav = {
        enabled: cfg.server?.webdav?.enabled ?? false
      }
      settings.database.driver = cfg.database?.driver || 'sqlite'
      settings.database.dsn = cfg.database?.dsn || ''

      // 解析 MySQL DSN
      if (settings.database.driver === 'mysql' && cfg.database?.dsn) {
        parseMysqlDsn(cfg.database.dsn)
      }
      // 解析 PostgreSQL DSN
      if (settings.database.driver === 'postgres' && cfg.database?.dsn) {
        parsePostgresDsn(cfg.database.dsn)
      }
      settings.rootDirs = cfg.rootDirs?.map(d => ({ path: d.fullPath, name: d.name })) || []
      settings.allowedExtensions = cfg.allowedExtensions || []

      // 私有文件配置
      settings.privateFiles.enabled = cfg.privateFiles?.enabled ?? false
      settings.privateFiles.path = cfg.privateFiles?.path || ''
      settings.privateQuotaGlobal = cfg.privateFiles?.quotaGlobal || 0
      settings.privateQuotaUser = cfg.privateFiles?.quotaPerUser || 0

      // 临时文件配置
      settings.tempFiles.enabled = cfg.tempFiles?.enabled ?? true
      settings.tempFiles.path = cfg.tempFiles?.path || ''
      settings.tempFiles.defaultExpireDays = cfg.tempFiles?.defaultExpireDays || 7
      settings.tempFiles.deleteOnDownload = cfg.tempFiles?.deleteOnDownload ?? true
      settings.tempFilesQuotaGlobal = cfg.tempFiles?.quotaGlobal || 0
      settings.tempFilesQuotaPerIP = cfg.tempFiles?.quotaPerIP || 0

      settings.upload = cfg.upload || { chunkSize: 10485760, maxFileSize: 17179869184, urlUpload: { enabled: false, allowedIPRanges: [], insecureSkipVerify: false } }

      // 下载限流配置（maxRequests=0 表示不限制）
      settings.download = {
        rateLimit: {
          windowMinutes: cfg.download?.rateLimit?.windowMinutes || 1,
          maxRequests: cfg.download?.rateLimit?.maxRequests || 0,
          lockAfter: cfg.download?.rateLimit?.lockAfter || 0,
          lockMinutes: cfg.download?.rateLimit?.lockMinutes || 10
        }
      }

      // 预览配置
      settings.preview = {
        allowMimes: cfg.preview?.allowMimes || '',
        allowExts: cfg.preview?.allowExts || '',
        maxInlineSize: NumberUtils.parseFileSize(cfg.preview?.maxInlineSize || '1M'),
        textChunkSize: NumberUtils.parseFileSize(cfg.preview?.textChunkSize || '100K')
      }

      // Open API 配置
      settings.openApi = {
        enabled: cfg.openApi?.enabled ?? false,
        ipAccessMode: cfg.openApi?.ipAccessMode || 'none',
        ipWhitelist: (cfg.openApi?.ipWhitelist || []).join('\n'),
        ipBlacklist: (cfg.openApi?.ipBlacklist || []).join('\n'),
        rateLimitEnabled: cfg.openApi?.rateLimitEnabled ?? true,
        requestsPerMinute: cfg.openApi?.requestsPerMinute || 60
      }

      // 文件索引配置
      settings.scanStartDelaySeconds = cfg.index?.scanStartDelaySeconds || 10
      settings.scanCronExpression = cfg.index?.scanCronExpression || '0 1 * * *'
      settings.searchReconcileCronExpression = cfg.index?.searchReconcileCronExpression || '0 5 * * *'

      // 安全配置（trust_proxy / CORS）
      settings.security = {
        trustProxy: cfg.security?.trustProxy ?? false,
        allowedOrigins: cfg.security?.allowedOrigins || []
      }

      // 资源监控配置
      settings.resource = {
        enabled: cfg.resource?.enabled ?? false,
        samplingInterval: cfg.resource?.samplingInterval || 5,
        collectInterval: cfg.resource?.collectInterval || 60,
        retentionDays: cfg.resource?.retentionDays || 7
      }

      // 保存原始服务器配置用于变更检测
      originalServerConfig.value = {
        host: settings.server.host,
        httpPort: settings.server.http.port
      }
    }
  } catch (e) {
    console.error('加载设置失败', e)
    message.error('加载设置失败')
  }
}

// 等待服务就绪后导航
const pollServerHealth = (url, maxRetries, interval, onReady) => {
  let retries = 0
  const check = () => {
    retries++
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 3000)
    fetch(url + '/api/v1/health', { mode: 'no-cors', signal: controller.signal })
      .then(() => { clearTimeout(timeoutId); onReady() })
      .catch(() => {
        clearTimeout(timeoutId)
        if (retries < maxRetries) {
          setTimeout(check, interval)
        } else {
          onReady()
        }
      })
  }
  check()
}

// 执行保存配置
const doSaveSettings = async () => {
  saving.value = true
  try {
    let finalDsn = settings.database.dsn
    if (settings.database.driver === 'mysql') {
      finalDsn = buildMysqlDsn()
    } else if (settings.database.driver === 'postgres') {
      finalDsn = buildPostgresDsn()
    }

    const configData = {
      app: { name: settings.appName },
      account: {
        anonymous: { username: settings.account.anonymous.username },
        reservedUsernames: settings.account.reservedUsernames
      },
      server: {
        host: settings.server.host,
        http: { enabled: settings.server.http.enabled, port: settings.server.http.port },
        https: { enabled: settings.server.https.enabled, port: settings.server.https.port },
        ftp: {
          enabled: settings.server.ftp.enabled,
          port: settings.server.ftp.port,
          passivePortStart: settings.server.ftp.passivePortStart,
          passivePortEnd: settings.server.ftp.passivePortEnd
        },
        ftps: { enabled: settings.server.ftps.enabled, port: settings.server.ftps.port },
        webdav: { enabled: settings.server.webdav.enabled }
      },
      database: {
        driver: settings.database.driver,
        dsn: finalDsn
      },
      rootDirs: settings.rootDirs,
      allowedExtensions: settings.allowedExtensions,
      privateFiles: {
        ...settings.privateFiles,
        quotaGlobal: settings.privateQuotaGlobal,
        quotaPerUser: settings.privateQuotaUser
      },
      tempFiles: {
        ...settings.tempFiles,
        quotaGlobal: settings.tempFilesQuotaGlobal,
        quotaPerIP: settings.tempFilesQuotaPerIP
      },
      upload: settings.upload,
      download: {
        rateLimit: settings.download.rateLimit
      },
      preview: {
        allowMimes: settings.preview.allowMimes,
        allowExts: settings.preview.allowExts,
        maxInlineSize: formatToUnit(settings.preview.maxInlineSize),
        textChunkSize: formatToUnit(settings.preview.textChunkSize)
      },
      openApi: {
        enabled: settings.openApi.enabled,
        ipAccessMode: settings.openApi.ipAccessMode,
        ipWhitelist: settings.openApi.ipWhitelist.split('\n').filter(ip => ip.trim()),
        ipBlacklist: settings.openApi.ipBlacklist.split('\n').filter(ip => ip.trim()),
        rateLimitEnabled: settings.openApi.rateLimitEnabled,
        requestsPerMinute: settings.openApi.requestsPerMinute
      },
      index: {
        scanStartDelaySeconds: settings.scanStartDelaySeconds,
        scanCronExpression: settings.scanCronExpression,
        searchReconcileCronExpression: settings.searchReconcileCronExpression,
      },
      resource: {
        enabled: settings.resource.enabled,
        samplingInterval: settings.resource.samplingInterval,
        collectInterval: settings.resource.collectInterval,
        retentionDays: settings.resource.retentionDays
      },
      security: {
        trustProxy: settings.security.trustProxy,
        allowedOrigins: settings.security.allowedOrigins
      }
    }

    const data = await ConfigApi.save(configData)
    if (data.success) {
      message.success(data.message || '配置已保存并生效')
      try {
        const optionsRes = await SystemApi.getOptions()
        if (optionsRes.data) {
          store.setConfig({
            appName: optionsRes.data.app?.name || '',
            privateStorageEnabled: optionsRes.data.privateStorage?.enabled ?? false,
            tempFilesEnabled: optionsRes.data.tempFiles?.enabled ?? true,
            ftpEnabled: optionsRes.data.ftp?.enabled ?? false,
            ftpPort: optionsRes.data.ftp?.port ?? 2121,
            webdavEnabled: optionsRes.data.webdav?.enabled ?? false,
            openApiEnabled: optionsRes.data.openApi?.enabled ?? false,
            anonymousUsername: optionsRes.data.account?.anonymous?.username || 'public'
          })
        }
      } catch (e) {
        console.error('刷新配置失败', e)
      }
      const hostChanged = originalServerConfig.value !== null &&
        settings.server.host !== originalServerConfig.value.host
      const portChanged = originalServerConfig.value !== null &&
        settings.server.http.port !== originalServerConfig.value.httpPort
      if (hostChanged || portChanged) {
        const displayHost = settings.server.host === '0.0.0.0' ? window.location.hostname : settings.server.host
        const newUrl = 'http://' + displayHost + ':' + settings.server.http.port
        dialog.warning({
          title: '服务器地址已变更',
          content: '服务已切换到新地址：' + newUrl,
          positiveText: '前往新地址',
          negativeText: '留在当前页面',
          onPositiveClick: () => {
            pollServerHealth(newUrl, 20, 500, () => {
              window.location.href = newUrl
            })
          }
        })
      }
    } else {
      message.error(data.message || '保存失败')
    }
  } catch (e) {
    console.error('保存设置失败', e)
    message.error('保存设置失败')
  } finally {
    saving.value = false
  }
}

// 保存配置入口
const saveSettings = () => {
  doSaveSettings()
}

onMounted(() => {
  loadSettings()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.settings-view {
  padding: @container-padding;
  animation: slideUp 0.4s ease-out;

  .header-section {
    margin-bottom: 24px;

    h2 {
      margin: 0 0 8px 0;
      font-size: @font-size-xxl;
      font-weight: 600;
    }

    .description {
      margin: 0;
      color: @text-color-secondary;
      font-size: @font-size-base;
    }
  }

  .actions-inner {
    display: flex;
    justify-content: flex-end;
    gap: 12px;
  }

  .actions-bar {
    margin-top: 24px;
    padding: 16px;
    background: @bg-color-secondary;
    border-radius: @border-radius-lg;
    position: sticky;
    bottom: 16px;
    box-shadow: @shadow-md;
  }

  .field-hint {
    color: var(--el-text-color-placeholder);
    font-size: 12px;
  }
}

@media @tablet {
  .settings-view {
    padding: 12px;
  }
}

@media @mobile {
  .settings-view {
    padding: 8px;
  }

  .actions-bar {
    padding: 12px !important;
    position: static !important;

    .actions-inner {
      justify-content: stretch;

      .el-button {
        flex: 1;
      }
    }
  }
}
</style>