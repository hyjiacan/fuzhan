<template>
  <div class="settings-view">
    <div class="page-head">
      <div class="title-wrap">
        <h2>系统设置</h2>
        <p class="description">管理系统配置和参数调整</p>
      </div>
      <div class="dirty-tip" v-if="isDirty">
        <span class="dot"></span>有未保存的更改
      </div>
    </div>

    <div class="settings-body">
      <!-- 侧栏分节导航 -->
      <aside class="settings-sider" :class="{ collapsed }">
        <button class="sider-toggle" :title="collapsed ? '展开导航' : '收起导航'" @click="collapsed = !collapsed">
          <el-icon><component :is="collapsed ? Expand : Fold" /></el-icon>
        </button>
        <el-menu
          :default-active="activePanel"
          :collapse="collapsed"
          :collapse-transition="false"
          class="settings-menu"
          @select="handleSelect"
        >
          <template v-for="group in navGroups" :key="group.title">
            <div class="menu-group-title" v-if="!collapsed">{{ group.title }}</div>
            <el-menu-item v-for="item in group.items" :key="item.key" :index="item.key">
              <el-icon><component :is="item.icon" /></el-icon>
              <template #title>
                <span class="menu-item-label">{{ item.label }}</span>
              </template>
            </el-menu-item>
          </template>
        </el-menu>
      </aside>

      <!-- 内容面板 -->
      <div class="settings-content">
        <transition name="panel-fade" mode="out-in">
          <component :is="currentComponent" :key="activePanel" :settings="settings" />
        </transition>
      </div>
    </div>

    <div class="actions-bar">
      <div class="actions-inner">
        <div class="actions-status">
          <template v-if="isDirty"><span class="dot warning"></span>当前有未保存的更改</template>
        </div>
        <div class="actions-btns">
          <el-button :disabled="!isDirty" @click="handleReset">重置</el-button>
          <el-button type="primary" :loading="saving" :disabled="!isDirty" @click="saveSettings">
            保存配置{{ isDirty ? '' : ' ✓' }}
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, shallowRef, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { onBeforeRouteLeave } from 'vue-router'
import { Setting, FolderOpened, Files, UploadFilled, Cpu, PictureRounded, Grid, Coin, Lock, Key, Odometer, Expand, Fold } from '@element-plus/icons-vue'
import { NumberUtils } from '@/utils'
import { ConfigApi, SystemApi } from '@/api'
import store from '@/store'
import SettingsAppInfo from './settings/SettingsAppInfo.vue'
import SettingsFilesQuota from './settings/SettingsFilesQuota.vue'
import SettingsUploadConfig from './settings/SettingsUploadConfig.vue'
import SettingsService from './settings/SettingsService.vue'
import SettingsStorage from './settings/SettingsStorage.vue'
import SettingsPreview from './settings/SettingsPreview.vue'
import SettingsMime from './settings/SettingsMime.vue'
import SettingsDatabase from './settings/SettingsDatabase.vue'
import SettingsAccess from './settings/SettingsAccess.vue'
import SettingsAuth from './settings/SettingsAuth.vue'
import SettingsResource from './settings/SettingsResource.vue'
import { formatToUnit } from './settings/useSettingsUtils'

const message = ElMessage

const saving = ref(false)
const activePanel = shallowRef('appInfo')
const collapsed = ref(false)

// 侧栏分节分组：基础 / 服务存储 / 预览类型 / 数据安全 / 资源
const navGroups = [
  {
    title: '基础信息',
    items: [
      { key: 'appInfo', label: '应用与共享目录', icon: Setting },
      { key: 'filesQuota', label: '私有与临时文件', icon: FolderOpened },
      { key: 'upload', label: '上传配置', icon: UploadFilled }
    ]
  },
  {
    title: '服务与存储',
    items: [
      { key: 'service', label: '服务配置', icon: Cpu },
      { key: 'storage', label: '存储索引', icon: Files }
    ]
  },
  {
    title: '预览与类型',
    items: [
      { key: 'preview', label: '预览配置', icon: PictureRounded },
      { key: 'mime', label: 'MIME 表', icon: Grid }
    ]
  },
  {
    title: '数据与安全',
    items: [
      { key: 'database', label: '数据库', icon: Coin },
      { key: 'access', label: '访问控制', icon: Lock },
      { key: 'auth', label: '身份认证', icon: Key }
    ]
  },
  {
    title: '资源',
    items: [
      { key: 'resource', label: '资源监控', icon: Odometer }
    ]
  }
]

const panelMap = {
  appInfo: SettingsAppInfo,
  filesQuota: SettingsFilesQuota,
  upload: SettingsUploadConfig,
  service: SettingsService,
  storage: SettingsStorage,
  preview: SettingsPreview,
  mime: SettingsMime,
  database: SettingsDatabase,
  access: SettingsAccess,
  auth: SettingsAuth,
  resource: SettingsResource
}

const currentComponent = computed(() => panelMap[activePanel.value])

// 脏标记：以 settings 序列化快照为基准，判断是否存在未保存更改
// 基线用 ref 而非普通变量，否则 markClean 更新基线时 isDirty 不会重算（保存后脏标记无法复位）
const baselineSnapshot = ref('')
// 直接序列化响应式对象：若用 toRaw 剥离代理，computed 无法收集字段依赖，isDirty 将永不响应变更
const serialize = (v) => JSON.stringify(v)
const isDirty = computed(() => baselineSnapshot.value !== '' && serialize(settings) !== baselineSnapshot.value)
// 各顶层配置节的基线序列化值，供保存时增量 diff：仅提交发生变化的配置节
let baselineSections = null

// 保存原始服务器/数据库配置用于变更检测与危险操作确认
const originalServerConfig = ref(null)
const originalDatabaseDriver = ref('')

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
  mimeTable: {},
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
  auth: {
    ldap: {
      enabled: false,
      host: '',
      port: 389,
      useSSL: false,
      baseDN: '',
      bindDN: '',
      bindPassword: '',
      userFilter: '',
      syncInterval: 3600,
      autoCreateUser: false,
      insecureSkipVerify: false
    },
    oauth2: {
      enabled: false,
      providerName: '',
      clientId: '',
      clientSecret: '',
      authUrl: '',
      tokenUrl: '',
      userInfoUrl: '',
      issuer: '',
      scopes: [],
      redirectUrl: '',
      usernameClaim: '',
      displayNameClaim: '',
      emailClaim: '',
      autoCreateUser: false,
      insecureSkipVerify: false
    }
  }
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

      if (settings.database.driver === 'mysql' && cfg.database?.dsn) {
        parseMysqlDsn(cfg.database.dsn)
      }
      if (settings.database.driver === 'postgres' && cfg.database?.dsn) {
        parsePostgresDsn(cfg.database.dsn)
      }
      settings.rootDirs = cfg.rootDirs?.map(d => ({ path: d.fullPath, name: d.name, uploadEnabled: d.uploadEnabled !== false })) || []
      settings.allowedExtensions = cfg.allowedExtensions || []

      settings.privateFiles.enabled = cfg.privateFiles?.enabled ?? false
      settings.privateFiles.path = cfg.privateFiles?.path || ''
      settings.privateQuotaGlobal = cfg.privateFiles?.quotaGlobal || 0
      settings.privateQuotaUser = cfg.privateFiles?.quotaPerUser || 0

      settings.tempFiles.enabled = cfg.tempFiles?.enabled ?? true
      settings.tempFiles.path = cfg.tempFiles?.path || ''
      settings.tempFiles.defaultExpireDays = cfg.tempFiles?.defaultExpireDays || 7
      settings.tempFiles.deleteOnDownload = cfg.tempFiles?.deleteOnDownload ?? true
      settings.tempFilesQuotaGlobal = cfg.tempFiles?.quotaGlobal || 0
      settings.tempFilesQuotaPerIP = cfg.tempFiles?.quotaPerIP || 0

      settings.upload = cfg.upload || { chunkSize: 10485760, maxFileSize: 17179869184, urlUpload: { enabled: false, allowedIPRanges: [], insecureSkipVerify: false } }

      settings.download = {
        rateLimit: {
          windowMinutes: cfg.download?.rateLimit?.windowMinutes || 1,
          maxRequests: cfg.download?.rateLimit?.maxRequests || 0,
          lockAfter: cfg.download?.rateLimit?.lockAfter || 0,
          lockMinutes: cfg.download?.rateLimit?.lockMinutes || 10
        }
      }

      settings.preview = {
        allowMimes: cfg.preview?.allowMimes || '',
        allowExts: cfg.preview?.allowExts || '',
        maxInlineSize: NumberUtils.parseFileSize(cfg.preview?.maxInlineSize || '1M'),
        textChunkSize: NumberUtils.parseFileSize(cfg.preview?.textChunkSize || '100K')
      }

      settings.mimeTable = { ...(cfg.mimeTable || {}) }

      settings.openApi = {
        enabled: cfg.openApi?.enabled ?? false,
        ipAccessMode: cfg.openApi?.ipAccessMode || 'none',
        ipWhitelist: (cfg.openApi?.ipWhitelist || []).join('\n'),
        ipBlacklist: (cfg.openApi?.ipBlacklist || []).join('\n'),
        rateLimitEnabled: cfg.openApi?.rateLimitEnabled ?? true,
        requestsPerMinute: cfg.openApi?.requestsPerMinute || 60
      }

      settings.scanStartDelaySeconds = cfg.index?.scanStartDelaySeconds || 10
      settings.scanCronExpression = cfg.index?.scanCronExpression || '0 1 * * *'
      settings.searchReconcileCronExpression = cfg.index?.searchReconcileCronExpression || '0 5 * * *'

      settings.security = {
        trustProxy: cfg.security?.trustProxy ?? false,
        allowedOrigins: cfg.security?.allowedOrigins || []
      }

      settings.resource = {
        enabled: cfg.resource?.enabled ?? false,
        samplingInterval: cfg.resource?.samplingInterval || 5,
        collectInterval: cfg.resource?.collectInterval || 60,
        retentionDays: cfg.resource?.retentionDays || 7
      }

      settings.auth = {
        ldap: {
          enabled: cfg.auth?.ldap?.enabled ?? false,
          host: cfg.auth?.ldap?.host || '',
          port: cfg.auth?.ldap?.port || 389,
          useSSL: cfg.auth?.ldap?.useSSL ?? false,
          baseDN: cfg.auth?.ldap?.baseDN || '',
          bindDN: cfg.auth?.ldap?.bindDN || '',
          bindPassword: cfg.auth?.ldap?.bindPassword || '',
          userFilter: cfg.auth?.ldap?.userFilter || '',
          syncInterval: cfg.auth?.ldap?.syncInterval || 3600,
          autoCreateUser: cfg.auth?.ldap?.autoCreateUser ?? false,
          insecureSkipVerify: cfg.auth?.ldap?.insecureSkipVerify ?? false
        },
        oauth2: {
          enabled: cfg.auth?.oauth2?.enabled ?? false,
          providerName: cfg.auth?.oauth2?.providerName || '',
          clientId: cfg.auth?.oauth2?.clientId || '',
          clientSecret: cfg.auth?.oauth2?.clientSecret || '',
          authUrl: cfg.auth?.oauth2?.authUrl || '',
          tokenUrl: cfg.auth?.oauth2?.tokenUrl || '',
          userInfoUrl: cfg.auth?.oauth2?.userInfoUrl || '',
          issuer: cfg.auth?.oauth2?.issuer || '',
          scopes: cfg.auth?.oauth2?.scopes || [],
          redirectUrl: cfg.auth?.oauth2?.redirectUrl || '',
          usernameClaim: cfg.auth?.oauth2?.usernameClaim || '',
          displayNameClaim: cfg.auth?.oauth2?.displayNameClaim || '',
          emailClaim: cfg.auth?.oauth2?.emailClaim || '',
          autoCreateUser: cfg.auth?.oauth2?.autoCreateUser ?? false,
          insecureSkipVerify: cfg.auth?.oauth2?.insecureSkipVerify ?? false
        }
      }

      originalServerConfig.value = {
        host: settings.server.host,
        httpPort: settings.server.http.port
      }
      originalDatabaseDriver.value = settings.database.driver

      // 重置脏标记基线
      markClean()
    }
  } catch (e) {
    console.error('加载设置失败', e)
    message.error('加载设置失败')
  }
}

// 标记为已保存，建立新基线
const markClean = () => {
  baselineSnapshot.value = serialize(settings)
  baselineSections = {}
  for (const k of Object.keys(settings)) baselineSections[k] = JSON.stringify(settings[k])
}

// configData 顶层节 -> 其来源的 settings 顶层节，用于判定该节是否因变更而随请求提交
const SECTION_SOURCES = {
  app: ['appName'],
  account: ['account'],
  server: ['server'],
  database: ['database'],
  rootDirs: ['rootDirs'],
  allowedExtensions: ['allowedExtensions'],
  privateFiles: ['privateFiles', 'privateQuotaGlobal', 'privateQuotaUser'],
  tempFiles: ['tempFiles', 'tempFilesQuotaGlobal', 'tempFilesQuotaPerIP'],
  upload: ['upload'],
  download: ['download'],
  preview: ['preview'],
  mimeTable: ['mimeTable'],
  openApi: ['openApi'],
  index: ['scanStartDelaySeconds', 'scanCronExpression', 'searchReconcileCronExpression'],
  resource: ['resource'],
  security: ['security'],
  auth: ['auth']
}

// 判定任一来源设置节是否相对基线变化；基线尚未建立时视为全量提交
const sectionChanged = (keys) => !baselineSections || keys.some(k => baselineSections[k] !== JSON.stringify(settings[k]))

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

// 危险操作二次确认：数据库驱动或访问控制等高风险变更
const confirmCriticalChange = async () => {
  const dbChanged = settings.database.driver !== originalDatabaseDriver.value
  if (dbChanged) {
    try {
      await ElMessageBox.confirm(
        '当前已切换数据库类型。切换后需重启服务方才生效，且已有数据需通过外部工具手动迁移。确定继续保存吗？',
        '检测到高危配置变更',
        {
          confirmButtonText: '仍要保存',
          cancelButtonText: '取消',
          type: 'warning',
          dangerouslyUseHTMLString: false
        }
      )
      return true
    } catch (e) {
      return false
    }
  }
  return true
}

// 执行保存配置
const doSaveSettings = async () => {
  // 危险操作二次确认
  const proceed = await confirmCriticalChange()
  if (!proceed) return

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
      mimeTable: settings.mimeTable,
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
      },
      auth: {
        ldap: {
          enabled: settings.auth.ldap.enabled,
          host: settings.auth.ldap.host,
          port: settings.auth.ldap.port,
          useSSL: settings.auth.ldap.useSSL,
          baseDN: settings.auth.ldap.baseDN,
          bindDN: settings.auth.ldap.bindDN,
          bindPassword: settings.auth.ldap.bindPassword,
          userFilter: settings.auth.ldap.userFilter,
          syncInterval: settings.auth.ldap.syncInterval,
          autoCreateUser: settings.auth.ldap.autoCreateUser,
          insecureSkipVerify: settings.auth.ldap.insecureSkipVerify
        },
        oauth2: {
          enabled: settings.auth.oauth2.enabled,
          providerName: settings.auth.oauth2.providerName,
          clientId: settings.auth.oauth2.clientId,
          clientSecret: settings.auth.oauth2.clientSecret,
          authUrl: settings.auth.oauth2.authUrl,
          tokenUrl: settings.auth.oauth2.tokenUrl,
          userInfoUrl: settings.auth.oauth2.userInfoUrl,
          issuer: settings.auth.oauth2.issuer,
          scopes: settings.auth.oauth2.scopes,
          redirectUrl: settings.auth.oauth2.redirectUrl,
          usernameClaim: settings.auth.oauth2.usernameClaim,
          displayNameClaim: settings.auth.oauth2.displayNameClaim,
          emailClaim: settings.auth.oauth2.emailClaim,
          autoCreateUser: settings.auth.oauth2.autoCreateUser,
          insecureSkipVerify: settings.auth.oauth2.insecureSkipVerify
        }
      }
    }

    // 增量保存：按 SECTION_SOURCES 只打包发生变化的配置节，未变更节点不随请求发送。
    // 后端对未提交节点按 present() 做节点级合并、保留原值，避免全量提交覆盖无关配置
    const payload = {}
    for (const [dstKey, srcKeys] of Object.entries(SECTION_SOURCES)) {
      if (sectionChanged(srcKeys)) payload[dstKey] = configData[dstKey]
    }

    const data = await ConfigApi.save(payload)
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
      // 刷新基线
      originalDatabaseDriver.value = settings.database.driver
      markClean()
      const hostChanged = originalServerConfig.value !== null &&
        settings.server.host !== originalServerConfig.value.host
      const portChanged = originalServerConfig.value !== null &&
        settings.server.http.port !== originalServerConfig.value.httpPort
      if (hostChanged || portChanged) {
        const displayHost = settings.server.host === '0.0.0.0' ? window.location.hostname : settings.server.host
        const newUrl = 'http://' + displayHost + ':' + settings.server.http.port
        ElMessageBox.confirm('服务已切换到新地址：' + newUrl, '服务器地址已变更', {
          confirmButtonText: '前往新地址',
          cancelButtonText: '留在当前页面',
          type: 'warning'
        }).then(() => {
          pollServerHealth(newUrl, 20, 500, () => {
            window.location.href = newUrl
          })
        }).catch(() => {})
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

// 重置：二次确认后重新加载
const handleReset = () => {
  ElMessageBox.confirm('重置将丢弃当前未保存的更改，重新加载服务器配置。确定继续吗？', '确认重置', {
    confirmButtonText: '重置',
    cancelButtonText: '取消',
    type: 'warning'
  }).then(() => {
    loadSettings()
    message.success('已重置，恢复服务器配置')
  }).catch(() => {})
}

const handleSelect = (key) => {
  activePanel.value = key
}

// 导航切换（侧栏折叠/展开由点击菜单标题项切换，这里仅处理面板切换）

// 离开拦截：有未保存更改时提示
onBeforeRouteLeave(() => {
  if (isDirty.value) {
    return ElMessageBox.confirm('当前存在未保存的更改，离开后将会丢失。确定离开吗？', '未保存的更改', {
      confirmButtonText: '仍要离开',
      cancelButtonText: '留在当前页',
      type: 'warning'
    }).then(() => true).catch(() => false)
  }
  return true
})

onMounted(() => {
  loadSettings()
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.settings-view {
  padding: @container-padding;

  .page-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 16px;
    margin-bottom: @spacing-lg;

    .title-wrap {
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
  }

  .dirty-tip {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    padding: 6px 12px;
    border-radius: @border-radius-lg;
    background: #fdf4e3;
    color: @warning-color;
    font-size: @font-size-sm;
    font-weight: 500;
    white-space: nowrap;

    .dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: @warning-color;
    }
  }

  .settings-body {
    display: flex;
    gap: 20px;
    align-items: flex-start;
  }

  .settings-sider {
    flex: 0 0 200px;
    position: sticky;
    top: 16px;
    border-radius: @border-radius-lg;
    overflow: hidden;

    .sider-toggle {
      display: flex;
      align-items: center;
      justify-content: center;
      width: 100%;
      height: 36px;
      border: none;
      background: @bg-color;
      color: @text-color-secondary;
      cursor: pointer;
      transition: color @transition-fast, background-color @transition-fast;

      &:hover {
        color: @text-color;
        background: var(--el-fill-color-light);
      }
    }

    &.collapsed {
      flex-basis: 64px;
    }

    .settings-menu {
      border-right: none;
      background: @bg-color;
      padding: 6px 0;
      border-radius: @border-radius-lg;

      :deep(.menu-group-title) {
        padding: 14px 20px 6px;
        font-size: @font-size-xs;
        font-weight: 500;
        color: @text-color-placeholder;
        letter-spacing: 1px;
      }

      :deep(.el-menu-item) {
        height: 44px;
        line-height: 44px;
        border-radius: @menu-item-radius;
        margin: 2px @menu-item-margin-x;

        &:hover {
          background-color: var(--el-fill-color-light);
        }
      }

      :deep(.el-menu-item.is-active) {
        background-color: var(--el-color-primary-light-9);
        color: var(--el-color-primary);
        font-weight: 500;
      }
    }
  }

  .menu-item-label {
    overflow: hidden;
    text-overflow: ellipsis;
  }

  .settings-content {
    flex: 1 1 auto;
    min-width: 0;
  }

  .panel-fade-enter-active,
  .panel-fade-leave-active {
    transition: opacity @transition-fast;
  }

  .panel-fade-enter-from,
  .panel-fade-leave-to {
    opacity: 0;
  }

  .actions-inner {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
  }

  .actions-status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    color: @text-color-secondary;
    font-size: @font-size-sm;

    .dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;

      &.ok {
        background: @success-color;
      }

      &.warning {
        background: @warning-color;
      }
    }
  }

  .actions-btns {
    display: flex;
    gap: 12px;
  }

  .actions-bar {
    margin-top: @spacing-lg;
    padding: 16px;
    background: @bg-color-secondary;
    border-radius: @border-radius-lg;
    position: sticky;
    bottom: 16px;
    z-index: 100;
    box-shadow: @shadow-md;
  }
}

@media @tablet {
  .settings-view {
    padding: 12px;

    .settings-sider {
      flex-basis: 64px;

      &:not(.collapsed) {
        flex-basis: 180px;
      }

      .menu-group-title {
        padding-left: 14px;
      }
    }
  }
}

@media @mobile {
  .settings-view {
    padding: 8px;

    .page-head {
      flex-direction: column;
      gap: 8px;

      .dirty-tip {
        align-self: flex-start;
      }
    }

    .settings-body {
      flex-direction: column;
      align-items: stretch;
    }

    .settings-sider {
      flex-basis: auto !important;
      position: static;
      overflow: visible;
      background: @bg-color;
      border: 1px solid @border-color-light;
      border-radius: @border-radius-lg;

      &.collapsed .el-menu {
        display: flex;
        flex-wrap: wrap;
      }

      .sider-toggle {
        display: none;
      }
    }

    .actions-bar {
      padding: 12px !important;
      position: static !important;

      .actions-inner {
        flex-direction: column;
        align-items: stretch;
      }

      .actions-status {
        justify-content: center;
      }

      .actions-btns {
        .el-button {
          flex: 1;
        }
      }
    }
  }
}
</style>