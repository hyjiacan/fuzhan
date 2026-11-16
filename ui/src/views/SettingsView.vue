<template>
  <div class="settings-view page-container">
    <div class="header-section">
      <div class="header-left">
        <h2>系统设置</h2>
        <p class="description">管理系统配置和参数调整</p>
      </div>
      <div class="header-right">
        <div class="dirty-tip dirty-tip--chip" v-if="isDirty" @click="showChanges = true">
          <span class="dot"></span>有未保存的更改（{{ changeList.length }} 项）
          <span class="dirty-link">查看更改</span>
        </div>
      </div>
    </div>

    <div class="settings-body">
      <!-- 侧栏分节导航 -->
      <aside class="settings-sider" :class="{ collapsed }">
        <!-- <button class="sider-toggle" :title="collapsed ? '展开导航' : '收起导航'" @click="collapsed = !collapsed">
          <el-icon><component :is="collapsed ? Expand : Fold" /></el-icon>
        </button> -->
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
        <el-skeleton v-if="!settingsLoaded && !settingsLoadError" :rows="8" animated style="padding: 24px;" />
        <el-empty v-else-if="settingsLoadError" description="配置加载失败，请稍后重试">
          <el-button type="primary" @click="loadSettings">重新加载</el-button>
        </el-empty>
        <transition v-else name="panel-fade" mode="out-in">
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
          <el-button :disabled="!settingsLoaded || !isDirty" @click="showChanges = true">
            查看更改{{ changeList.length ? `（${changeList.length}）` : '' }}
          </el-button>
          <el-button :disabled="!settingsLoaded || !isDirty" @click="handleReset">重置</el-button>
          <el-button type="primary" :loading="saving" :disabled="!settingsLoaded || !isDirty" @click="saveSettings">
            保存配置{{ isDirty ? '' : ' ✓' }}
          </el-button>
        </div>
      </div>
    </div>

    <SettingsChangesDrawer v-model="showChanges" :changes="changeList" @navigate="handleNavigateToPanel" />
  </div>
</template>

<script setup>
import { ref, reactive, shallowRef, computed, onMounted } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { onBeforeRouteLeave } from 'vue-router'
import { Setting, FolderOpened, Files, UploadFilled, Cpu, PictureRounded, Coin, Lock, Key, Expand, Fold } from '@element-plus/icons-vue'
import { NumberUtils } from '@/utils'
import { ConfigApi, SystemApi } from '@/api'
import store from '@/store'
import SettingsAppInfo from './settings/SettingsAppInfo.vue'
import SettingsFilesQuota from './settings/SettingsFilesQuota.vue'
import SettingsUploadConfig from './settings/SettingsUploadConfig.vue'
import SettingsService from './settings/SettingsService.vue'
import SettingsStorage from './settings/SettingsStorage.vue'
import SettingsPreview from './settings/SettingsPreview.vue'
import SettingsDatabase from './settings/SettingsDatabase.vue'
import SettingsAccess from './settings/SettingsAccess.vue'
import SettingsAuth from './settings/SettingsAuth.vue'
import SettingsChangesDrawer from './settings/SettingsChangesDrawer.vue'
import { diffSettings } from './settings/useSettingsDiff'
import { formatToUnit } from './settings/useSettingsUtils'

const message = ElMessage

// 预览分组键列表（UI 结构，与后端分组定义一一对应）；
// 生效值与默认值均以后端为唯一来源，前端不内置默认表
const PREVIEW_GROUP_KEYS = ['image', 'text', 'audio', 'video', 'document', 'custom']

// normalizePreviewGroups 将后端预览分组配置归一化为设置页结构：
// 后端 config 接口输出的是已解析默认值的生效配置，前端直接展示；文本组大小解析为字节数值。
const normalizePreviewGroups = (cfgGroups) => {
  const groups = {}
  for (const name of PREVIEW_GROUP_KEYS) {
    const src = cfgGroups?.[name] || {}
    groups[name] = {
      enabled: src.enabled === true,
      mimes: src.mimes ?? '',
      exts: src.exts ?? ''
    }
  }
  groups.text.maxInlineSize = NumberUtils.parseFileSize(cfgGroups?.text?.max_inline_size || '1M')
  groups.text.textChunkSize = NumberUtils.parseFileSize(cfgGroups?.text?.text_chunk_size || '100K')
  return groups
}

// buildPreviewPayload 组装预览提交 payload：分组开关与列表原样提交，
// 文本组大小转为单位字符串（后端按 1M/100K 格式解析）。
const buildPreviewPayload = () => {
  const groups = {}
  for (const [name, g] of Object.entries(settings.preview.groups)) {
    const item = { enabled: g.enabled, mimes: g.mimes, exts: g.exts }
    if (name === 'text') {
      item.max_inline_size = formatToUnit(g.maxInlineSize)
      item.text_chunk_size = formatToUnit(g.textChunkSize)
    }
    groups[name] = item
  }
  return { enabled: settings.preview.enabled, groups }
}

const saving = ref(false)
const activePanel = shallowRef('appInfo')
const collapsed = ref(false)
// 变更预览抽屉开关
const showChanges = ref(false)
// 配置是否已从后端加载成功；成功前不渲染表单面板，避免以默认值冒充真实配置
const settingsLoaded = ref(false)
const settingsLoadError = ref(false)

// 侧栏分节分组：基础 / 服务存储 / 预览类型 / 数据安全（webhook/mime/resource 已迁至「系统管理」独立页）
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
      { key: 'storage', label: '存储与索引', icon: Files }
    ]
  },
  {
    title: '预览与类型',
    items: [
      { key: 'preview', label: '预览配置', icon: PictureRounded }
    ]
  },
  {
    title: '数据与安全',
    items: [
      { key: 'database', label: '数据库', icon: Coin },
      { key: 'access', label: '访问控制', icon: Lock },
      { key: 'auth', label: '身份认证', icon: Key }
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
  database: SettingsDatabase,
  access: SettingsAccess,
  auth: SettingsAuth
}

const currentComponent = computed(() => panelMap[activePanel.value])

// 脏标记：以 settings 序列化快照为基准，判断是否存在未保存更改
// 基线用 ref 而非普通变量，否则 markClean 更新基线时 isDirty 不会重算（保存后脏标记无法复位）
const baselineSnapshot = ref('')
// 直接序列化响应式对象：若用 toRaw 剥离代理，computed 无法收集字段依赖，isDirty 将永不响应变更。
// 同时忽略以下划线开头的 UI 临时字段（如 Webhook 端点的 _headersRows），
// 这些字段由设置面板初始化时写入，不代表用户更改，不能计入脏标记。
const serialize = (v) => JSON.stringify(v, (key, value) => (key.startsWith('_') ? undefined : value))
const isDirty = computed(() => baselineSnapshot.value !== '' && serialize(settings) !== baselineSnapshot.value)
// 变更明细：逐字段对比当前 settings 与基线快照（同样剔除 `_` 前缀的 UI 临时字段）
const changeList = computed(() => {
  if (!isDirty.value || !baselineSnapshot.value) return []
  try {
    return diffSettings(JSON.parse(serialize(settings)), JSON.parse(baselineSnapshot.value))
  } catch (e) {
    console.error('生成设置变更明细失败', e)
    return []
  }
})
// 各顶层配置节的基线序列化值，供保存时增量 diff：仅提交发生变化的配置节
let baselineSections = null

// 保存原始服务器/数据库配置用于变更检测与危险操作确认
const originalServerConfig = ref(null)
const originalDatabaseDriver = ref('')

const settings = reactive({
  appName: '',
  account: {
    anonymous: { username: 'public' },
    reservedUsernames: [],
    allowRegistration: false,
    requireLogin: false,
    captcha: { mode: 'disabled', triggerAfter: 3 }
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
  storageBackend: {
    backend: 'local',
    s3: { endpoint: '', accessKey: '', secretKey: '', bucket: '', region: '', useSSL: false },
    oss: { endpoint: '', accessKey: '', secretKey: '', bucket: '' },
    webdav: { url: '', username: '', password: '' },
    alert: {
      enabled: false,
      thresholdPercent: 85,
      checkIntervalMinutes: 10,
      cooldownMinutes: 120,
      webEnabled: true,
      webhookEnabled: false,
      emailEnabled: false,
      emailRecipients: [],
      smtp: { host: '', port: 587, username: '', password: '', from: '', tlsMode: 'starttls' }
    }
  },
  privateFiles: {
    enabled: false,
    path: ''
  },
  privateQuotaGlobal: 0,
  privateQuotaUser: 0,
  upload: {
    chunkSize: 10485760,
    maxFileSize: 17179869184,
    urlUpload: { enabled: false, allowedIPRanges: [], insecureSkipVerify: false, maxConcurrent: 2, maxRetries: 3 },
    rateLimit: { windowMinutes: 1, maxRequests: 0 }
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
    enabled: true,
    groups: normalizePreviewGroups(null)
  },
  openApi: {
    enabled: false,
    ipAccessMode: 'none',
    ipWhitelist: '',
    ipBlacklist: '',
    domainWhitelist: '',
    rateLimitEnabled: true,
    requestsPerMinute: 60
  },
  security: {
    trustProxy: false,
    allowedOrigins: []
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
  settingsLoadError.value = false
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
        passivePortStart: cfg.server?.ftp?.passive_port_start || 2122,
        passivePortEnd: cfg.server?.ftp?.passive_port_end || 2221
      }
      settings.server.ftps = {
        enabled: cfg.server?.ftps?.enabled ?? false,
        port: cfg.server?.ftps?.port || 990
      }
      settings.account = {
        anonymous: { username: cfg.account?.anonymous?.username || 'public' },
        reservedUsernames: cfg.account?.reserved_usernames || [],
        allowRegistration: cfg.account?.allow_registration ?? false,
        requireLogin: cfg.account?.require_login ?? false,
        captcha: {
          mode: cfg.account?.captcha?.mode || 'disabled',
          triggerAfter: cfg.account?.captcha?.trigger_after ?? 3
        }
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
      settings.rootDirs = cfg.root_dirs?.map(d => ({ path: d.full_path, name: d.name, uploadEnabled: d.upload_enabled !== false })) || []
      settings.allowedExtensions = cfg.allowed_extensions || []
      settings.storageBackend = {
        backend: cfg.storage_backend?.backend || 'local',
        s3: {
          endpoint: cfg.storage_backend?.s3?.endpoint || '',
          accessKey: cfg.storage_backend?.s3?.access_key || '',
          secretKey: cfg.storage_backend?.s3?.secret_key || '',
          bucket: cfg.storage_backend?.s3?.bucket || '',
          region: cfg.storage_backend?.s3?.region || '',
          useSSL: cfg.storage_backend?.s3?.use_ssl ?? false
        },
        oss: {
          endpoint: cfg.storage_backend?.oss?.endpoint || '',
          accessKey: cfg.storage_backend?.oss?.access_key || '',
          secretKey: cfg.storage_backend?.oss?.secret_key || '',
          bucket: cfg.storage_backend?.oss?.bucket || ''
        },
        webdav: {
          url: cfg.storage_backend?.webdav?.url || '',
          username: cfg.storage_backend?.webdav?.username || '',
          password: cfg.storage_backend?.webdav?.password || ''
        },
        alert: {
          enabled: cfg.storage_backend?.alert?.enabled ?? false,
          thresholdPercent: cfg.storage_backend?.alert?.threshold_percent ?? 85,
          checkIntervalMinutes: cfg.storage_backend?.alert?.check_interval_minutes ?? 10,
          cooldownMinutes: cfg.storage_backend?.alert?.cooldown_minutes ?? 120,
          webEnabled: cfg.storage_backend?.alert?.web_enabled ?? true,
          webhookEnabled: cfg.storage_backend?.alert?.webhook_enabled ?? false,
          emailEnabled: cfg.storage_backend?.alert?.email_enabled ?? false,
          emailRecipients: cfg.storage_backend?.alert?.email_recipients || [],
          smtp: {
            host: cfg.storage_backend?.alert?.smtp?.host || '',
            port: cfg.storage_backend?.alert?.smtp?.port ?? 587,
            username: cfg.storage_backend?.alert?.smtp?.username || '',
            password: cfg.storage_backend?.alert?.smtp?.password || '',
            from: cfg.storage_backend?.alert?.smtp?.from || '',
            tlsMode: cfg.storage_backend?.alert?.smtp?.tls_mode || 'starttls'
          }
        }
      }

      settings.privateFiles.enabled = cfg.private_files?.enabled ?? false
      settings.privateFiles.path = cfg.private_files?.path || ''
      settings.privateQuotaGlobal = cfg.private_files?.quota_global || 0
      settings.privateQuotaUser = cfg.private_files?.quota_per_user || 0

      settings.tempFiles.enabled = cfg.temp_files?.enabled ?? true
      settings.tempFiles.path = cfg.temp_files?.path || ''
      settings.tempFiles.defaultExpireDays = cfg.temp_files?.default_expire_days || 7
      settings.tempFiles.deleteOnDownload = cfg.temp_files?.delete_on_download ?? true
      settings.tempFilesQuotaGlobal = cfg.temp_files?.quota_global || 0
      settings.tempFilesQuotaPerIP = cfg.temp_files?.quota_per_ip || 0

      settings.upload = {
        chunkSize: cfg.upload?.chunk_size || 10485760,
        maxFileSize: cfg.upload?.max_file_size || 17179869184,
        urlUpload: {
          enabled: cfg.upload?.url_upload?.enabled ?? false,
          allowedIPRanges: cfg.upload?.url_upload?.allowed_ip_ranges || [],
          insecureSkipVerify: cfg.upload?.url_upload?.insecure_skip_verify ?? false,
          maxConcurrent: cfg.upload?.url_upload?.max_concurrent || 2,
          maxRetries: cfg.upload?.url_upload?.max_retries || 3
        },
        rateLimit: {
          windowMinutes: cfg.upload?.rate_limit?.window_minutes || 1,
          maxRequests: cfg.upload?.rate_limit?.max_requests || 0
        }
      }

      settings.download = {
        rateLimit: {
          windowMinutes: cfg.download?.rate_limit?.window_minutes || 1,
          maxRequests: cfg.download?.rate_limit?.max_requests || 0,
          lockAfter: cfg.download?.rate_limit?.lock_after || 0,
          lockMinutes: cfg.download?.rate_limit?.lock_minutes || 10
        }
      }

      settings.preview = {
        enabled: cfg.preview?.enabled ?? true,
        groups: normalizePreviewGroups(cfg.preview?.groups)
      }

      settings.openApi = {
        enabled: cfg.open_api?.enabled ?? false,
        ipAccessMode: cfg.open_api?.ip_access_mode || 'none',
        ipWhitelist: (cfg.open_api?.ip_whitelist || []).join('\n'),
        ipBlacklist: (cfg.open_api?.ip_blacklist || []).join('\n'),
        domainWhitelist: (cfg.open_api?.domain_whitelist || []).join('\n'),
        rateLimitEnabled: cfg.open_api?.rate_limit_enabled ?? true,
        requestsPerMinute: cfg.open_api?.requests_per_minute || 60
      }

      settings.security = {
        trustProxy: cfg.security?.trust_proxy ?? false,
        allowedOrigins: cfg.security?.allowed_origins || []
      }

      settings.auth = {
        ldap: {
          enabled: cfg.auth?.ldap?.enabled ?? false,
          host: cfg.auth?.ldap?.host || '',
          port: cfg.auth?.ldap?.port || 389,
          useSSL: cfg.auth?.ldap?.use_ssl ?? false,
          baseDN: cfg.auth?.ldap?.base_dn || '',
          bindDN: cfg.auth?.ldap?.bind_dn || '',
          bindPassword: cfg.auth?.ldap?.bind_password || '',
          userFilter: cfg.auth?.ldap?.user_filter || '',
          syncInterval: cfg.auth?.ldap?.sync_interval || 3600,
          autoCreateUser: cfg.auth?.ldap?.auto_create_user ?? false,
          insecureSkipVerify: cfg.auth?.ldap?.insecure_skip_verify ?? false
        },
        oauth2: {
          enabled: cfg.auth?.oauth2?.enabled ?? false,
          providerName: cfg.auth?.oauth2?.provider_name || '',
          clientId: cfg.auth?.oauth2?.client_id || '',
          clientSecret: cfg.auth?.oauth2?.client_secret || '',
          authUrl: cfg.auth?.oauth2?.auth_url || '',
          tokenUrl: cfg.auth?.oauth2?.token_url || '',
          userInfoUrl: cfg.auth?.oauth2?.user_info_url || '',
          issuer: cfg.auth?.oauth2?.issuer || '',
          scopes: cfg.auth?.oauth2?.scopes || [],
          redirectUrl: cfg.auth?.oauth2?.redirect_url || '',
          usernameClaim: cfg.auth?.oauth2?.username_claim || '',
          displayNameClaim: cfg.auth?.oauth2?.display_name_claim || '',
          emailClaim: cfg.auth?.oauth2?.email_claim || '',
          autoCreateUser: cfg.auth?.oauth2?.auto_create_user ?? false,
          insecureSkipVerify: cfg.auth?.oauth2?.insecure_skip_verify ?? false
        }
      }

      originalServerConfig.value = {
        host: settings.server.host,
        httpPort: settings.server.http.port
      }
      originalDatabaseDriver.value = settings.database.driver

      // 重置脏标记基线
      markClean()
      settingsLoaded.value = true
    } else {
      throw new Error('配置数据为空')
    }
  } catch (e) {
    console.error('加载设置失败', e)
    message.error('加载设置失败')
    settingsLoaded.value = false
    settingsLoadError.value = true
  }
}

// 标记为已保存，建立新基线
const markClean = () => {
  baselineSnapshot.value = serialize(settings)
  baselineSections = {}
  for (const k of Object.keys(settings)) baselineSections[k] = serialize(settings[k])
}

// configData 顶层节（后端 snake_case）-> 其来源的 settings 顶层节（前端内部 camelCase），
// 用于判定该节是否因变更而随请求提交
const SECTION_SOURCES = {
  app: ['appName'],
  account: ['account'],
  server: ['server'],
  database: ['database'],
  root_dirs: ['rootDirs'],
  allowed_extensions: ['allowedExtensions'],
  storage_backend: ['storageBackend'],
  private_files: ['privateFiles', 'privateQuotaGlobal', 'privateQuotaUser'],
  temp_files: ['tempFiles', 'tempFilesQuotaGlobal', 'tempFilesQuotaPerIP'],
  upload: ['upload'],
  download: ['download'],
  preview: ['preview'],
  open_api: ['openApi'],
  security: ['security'],
  auth: ['auth']
}

// 判定任一来源设置节是否相对基线变化；基线尚未建立时视为全量提交
const sectionChanged = (keys) => !baselineSections || keys.some(k => baselineSections[k] !== serialize(settings[k]))

// 等待服务就绪后导航
const pollServerHealth = (url, maxRetries, interval, onReady) => {
  let retries = 0
  const check = () => {
    retries++
    const controller = new AbortController()
    const timeoutId = setTimeout(() => controller.abort(), 3000)
    fetch(url + '/api/v1/web/health', { mode: 'no-cors', signal: controller.signal })
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

// 危险操作二次确认：数据库驱动、存储后端等需重启生效的高风险变更
const confirmCriticalChange = async () => {
  const dbChanged = settings.database.driver !== originalDatabaseDriver.value
  const storageChanged = !!baselineSections && baselineSections.storageBackend !== serialize(settings.storageBackend)
  if (dbChanged || storageChanged) {
    const messages = []
    if (dbChanged) {
      messages.push('当前已切换数据库类型，切换后需重启服务生效，且已有数据需通过外部工具手动迁移。')
    }
    if (storageChanged) {
      messages.push('存储后端或其连接参数已修改，需重启服务后才会生效。')
    }
    try {
      await ElMessageBox.confirm(
        messages.join('\n') + '\n确定继续保存吗？',
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
        reserved_usernames: settings.account.reservedUsernames,
        allow_registration: settings.account.allowRegistration,
        require_login: settings.account.requireLogin,
        captcha: {
          mode: settings.account.captcha.mode,
          trigger_after: settings.account.captcha.triggerAfter
        }
      },
      server: {
        host: settings.server.host,
        http: { enabled: settings.server.http.enabled, port: settings.server.http.port },
        https: { enabled: settings.server.https.enabled, port: settings.server.https.port },
        ftp: {
          enabled: settings.server.ftp.enabled,
          port: settings.server.ftp.port,
          passive_port_start: settings.server.ftp.passivePortStart,
          passive_port_end: settings.server.ftp.passivePortEnd
        },
        ftps: { enabled: settings.server.ftps.enabled, port: settings.server.ftps.port },
        webdav: { enabled: settings.server.webdav.enabled }
      },
      database: {
        driver: settings.database.driver,
        dsn: finalDsn
      },
      root_dirs: settings.rootDirs.map(d => ({
        name: d.name,
        path: d.path,
        full_path: d.path,
        upload_enabled: d.uploadEnabled !== false
      })),
      allowed_extensions: settings.allowedExtensions,
      storage_backend: {
        backend: settings.storageBackend.backend,
        s3: {
          endpoint: settings.storageBackend.s3.endpoint,
          access_key: settings.storageBackend.s3.accessKey,
          secret_key: settings.storageBackend.s3.secretKey,
          bucket: settings.storageBackend.s3.bucket,
          region: settings.storageBackend.s3.region,
          use_ssl: settings.storageBackend.s3.useSSL
        },
        oss: {
          endpoint: settings.storageBackend.oss.endpoint,
          access_key: settings.storageBackend.oss.accessKey,
          secret_key: settings.storageBackend.oss.secretKey,
          bucket: settings.storageBackend.oss.bucket
        },
        webdav: {
          url: settings.storageBackend.webdav.url,
          username: settings.storageBackend.webdav.username,
          password: settings.storageBackend.webdav.password
        },
        alert: {
          enabled: settings.storageBackend.alert.enabled,
          threshold_percent: settings.storageBackend.alert.thresholdPercent,
          check_interval_minutes: settings.storageBackend.alert.checkIntervalMinutes,
          cooldown_minutes: settings.storageBackend.alert.cooldownMinutes,
          web_enabled: settings.storageBackend.alert.webEnabled,
          webhook_enabled: settings.storageBackend.alert.webhookEnabled,
          email_enabled: settings.storageBackend.alert.emailEnabled,
          email_recipients: [...(settings.storageBackend.alert.emailRecipients || [])],
          smtp: {
            host: settings.storageBackend.alert.smtp.host,
            port: settings.storageBackend.alert.smtp.port,
            username: settings.storageBackend.alert.smtp.username,
            password: settings.storageBackend.alert.smtp.password,
            from: settings.storageBackend.alert.smtp.from,
            tls_mode: settings.storageBackend.alert.smtp.tlsMode
          }
        }
      },
      private_files: {
        enabled: settings.privateFiles.enabled,
        path: settings.privateFiles.path,
        quota_global: settings.privateQuotaGlobal,
        quota_per_user: settings.privateQuotaUser
      },
      temp_files: {
        enabled: settings.tempFiles.enabled,
        path: settings.tempFiles.path,
        default_expire_days: settings.tempFiles.defaultExpireDays,
        delete_on_download: settings.tempFiles.deleteOnDownload,
        quota_global: settings.tempFilesQuotaGlobal,
        quota_per_ip: settings.tempFilesQuotaPerIP
      },
      upload: {
        chunk_size: settings.upload.chunkSize,
        max_file_size: settings.upload.maxFileSize,
        url_upload: {
          enabled: settings.upload.urlUpload.enabled,
          allowed_ip_ranges: [...(settings.upload.urlUpload.allowedIPRanges || [])],
          insecure_skip_verify: settings.upload.urlUpload.insecureSkipVerify,
          max_concurrent: settings.upload.urlUpload.maxConcurrent,
          max_retries: settings.upload.urlUpload.maxRetries
        },
        rate_limit: {
          window_minutes: settings.upload.rateLimit.windowMinutes,
          max_requests: settings.upload.rateLimit.maxRequests
        }
      },
      download: {
        rate_limit: {
          window_minutes: settings.download.rateLimit.windowMinutes,
          max_requests: settings.download.rateLimit.maxRequests,
          lock_after: settings.download.rateLimit.lockAfter,
          lock_minutes: settings.download.rateLimit.lockMinutes
        }
      },
      preview: buildPreviewPayload(),
      open_api: {
        enabled: settings.openApi.enabled,
        ip_access_mode: settings.openApi.ipAccessMode,
        ip_whitelist: settings.openApi.ipWhitelist.split('\n').filter(ip => ip.trim()),
        ip_blacklist: settings.openApi.ipBlacklist.split('\n').filter(ip => ip.trim()),
        domain_whitelist: settings.openApi.domainWhitelist.split('\n').filter(d => d.trim()),
        rate_limit_enabled: settings.openApi.rateLimitEnabled,
        requests_per_minute: settings.openApi.requestsPerMinute
      },
      security: {
        trust_proxy: settings.security.trustProxy,
        allowed_origins: settings.security.allowedOrigins
      },
      auth: {
        ldap: {
          enabled: settings.auth.ldap.enabled,
          host: settings.auth.ldap.host,
          port: settings.auth.ldap.port,
          use_ssl: settings.auth.ldap.useSSL,
          base_dn: settings.auth.ldap.baseDN,
          bind_dn: settings.auth.ldap.bindDN,
          bind_password: settings.auth.ldap.bindPassword,
          user_filter: settings.auth.ldap.userFilter,
          sync_interval: settings.auth.ldap.syncInterval,
          auto_create_user: settings.auth.ldap.autoCreateUser,
          insecure_skip_verify: settings.auth.ldap.insecureSkipVerify
        },
        oauth2: {
          enabled: settings.auth.oauth2.enabled,
          provider_name: settings.auth.oauth2.providerName,
          client_id: settings.auth.oauth2.clientId,
          client_secret: settings.auth.oauth2.clientSecret,
          auth_url: settings.auth.oauth2.authUrl,
          token_url: settings.auth.oauth2.tokenUrl,
          user_info_url: settings.auth.oauth2.userInfoUrl,
          issuer: settings.auth.oauth2.issuer,
          scopes: settings.auth.oauth2.scopes,
          redirect_url: settings.auth.oauth2.redirectUrl,
          username_claim: settings.auth.oauth2.usernameClaim,
          display_name_claim: settings.auth.oauth2.displayNameClaim,
          email_claim: settings.auth.oauth2.emailClaim,
          auto_create_user: settings.auth.oauth2.autoCreateUser,
          insecure_skip_verify: settings.auth.oauth2.insecureSkipVerify
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
        // 保存后刷新 store：使用受保护的完整配置接口（/options 只含登录页最小字段）
        const optionsRes = await SystemApi.getConfig()
        if (optionsRes.data) {
          store.setConfig({
            appName: optionsRes.data.app?.name || '',
            privateStorageEnabled: optionsRes.data.private_storage?.enabled ?? false,
            tempFilesEnabled: optionsRes.data.temp_files?.enabled ?? true,
            ftpEnabled: optionsRes.data.ftp?.enabled ?? false,
            ftpPort: optionsRes.data.ftp?.port ?? null,
            webdavEnabled: optionsRes.data.webdav?.enabled ?? false,
            openApiEnabled: optionsRes.data.open_api?.enabled ?? false,
            urlUploadEnabled: optionsRes.data.upload?.url_upload?.enabled ?? false,
            allowRegistration: optionsRes.data.account?.allow_registration ?? false,
            requireLogin: optionsRes.data.account?.require_login ?? false,
            captchaMode: optionsRes.data.account?.captcha?.mode ?? 'disabled',
            captchaTriggerAfter: optionsRes.data.account?.captcha?.trigger_after ?? 3,
            anonymousUsername: optionsRes.data.webdav?.public_username || optionsRes.data.account?.anonymous?.username || ''
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

// 从变更明细跳转到对应设置分区，并收起抽屉
const handleNavigateToPanel = (panel) => {
  if (!panel) return
  activePanel.value = panel
  showChanges.value = false
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
        height: 32px;
        line-height: 32px;
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
    padding: 10px;
    background: @bg-color-secondary;
    border-radius: @border-radius-lg;
    position: sticky;
    bottom: 0;
    z-index: 100;
    box-shadow: @shadow-md;
  }
}

@media @tablet {
  .settings-view {
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
