import { formatSize } from './useSettingsUtils'

// 设置项中文标签：settings 路径 -> 展示名称（含中间容器节点）
const FIELD_LABELS = {
  appName: '应用名称',

  account: '账户',
  'account.anonymous': '匿名账户',
  'account.anonymous.username': '公开用户名',
  'account.reservedUsernames': '保留用户名',

  server: '服务配置',
  'server.host': '监听地址',
  'server.http': 'HTTP',
  'server.http.enabled': '启用 HTTP',
  'server.http.port': 'HTTP 端口',
  'server.https': 'HTTPS',
  'server.https.enabled': '启用 HTTPS',
  'server.https.port': 'HTTPS 端口',
  'server.ftp': 'FTP',
  'server.ftp.enabled': '启用 FTP',
  'server.ftp.port': 'FTP 端口',
  'server.ftp.passivePortStart': '被动端口起始',
  'server.ftp.passivePortEnd': '被动端口结束',
  'server.ftps': 'FTPS',
  'server.ftps.enabled': '启用 FTPS',
  'server.ftps.port': 'FTPS 端口',
  'server.webdav': 'WebDAV',
  'server.webdav.enabled': '启用 WebDAV',
  'server.tls': 'TLS 证书',
  'server.tls.certFile': '证书文件',
  'server.tls.keyFile': '密钥文件',

  database: '数据库',
  'database.driver': '数据库类型',
  'database.dsn': '数据库连接串',
  'database.mysqlHost': 'MySQL 主机地址',
  'database.mysqlPort': 'MySQL 端口',
  'database.mysqlUser': 'MySQL 用户名',
  'database.mysqlPassword': 'MySQL 密码',
  'database.mysqlDatabase': 'MySQL 数据库名',
  'database.postgresHost': 'PostgreSQL 主机地址',
  'database.postgresPort': 'PostgreSQL 端口',
  'database.postgresUser': 'PostgreSQL 用户名',
  'database.postgresPassword': 'PostgreSQL 密码',
  'database.postgresDatabase': 'PostgreSQL 数据库名',

  rootDirs: '共享目录',
  'rootDirs.path': '目录路径',
  'rootDirs.name': '显示名称',
  'rootDirs.uploadEnabled': '允许上传',

  allowedExtensions: '允许的扩展名',

  privateFiles: '私有文件',
  'privateFiles.enabled': '启用私有文件',
  'privateFiles.path': '存储路径',
  privateQuotaGlobal: '私有文件全局配额',
  privateQuotaUser: '私有文件用户配额',

  tempFiles: '临时文件',
  'tempFiles.enabled': '启用临时文件',
  'tempFiles.path': '存储路径',
  'tempFiles.defaultExpireDays': '默认过期天数',
  'tempFiles.deleteOnDownload': '下载后删除',
  tempFilesQuotaGlobal: '临时文件全局配额',
  tempFilesQuotaPerIP: '临时文件 IP 配额',

  upload: '上传配置',
  'upload.chunkSize': '分片大小',
  'upload.maxFileSize': '最大文件大小',
  'upload.urlUpload': 'URL 上传',
  'upload.urlUpload.enabled': '启用 URL 上传',
  'upload.urlUpload.insecureSkipVerify': '跳过证书验证',
  'upload.urlUpload.allowedIPRanges': '允许 IP 网段',

  download: '下载限制',
  'download.rateLimit': '下载频率限制',
  'download.rateLimit.windowMinutes': '窗口(分钟)',
  'download.rateLimit.maxRequests': '最大请求数',
  'download.rateLimit.lockAfter': '失效阈值',
  'download.rateLimit.lockMinutes': '锁定(分钟)',

  preview: '预览配置',
  'preview.allowMimes': 'MIME 类型',
  'preview.allowExts': '文件扩展名',
  'preview.maxInlineSize': '最大内联大小',
  'preview.textChunkSize': '文本分块大小',

  mimeTable: 'MIME 映射',

  openApi: '访问控制',
  'openApi.enabled': '启用 OpenAPI',
  'openApi.ipAccessMode': '访问模式',
  'openApi.ipWhitelist': 'IP 白名单',
  'openApi.ipBlacklist': 'IP 黑名单',
  'openApi.rateLimitEnabled': '频率限制',
  'openApi.requestsPerMinute': '请求频率(次/分钟)',

  scanStartDelaySeconds: '扫描启动延迟',
  scanCronExpression: '定时扫描间隔',
  searchReconcileCronExpression: '检索索引对齐间隔',

  security: '安全配置',
  'security.trustProxy': '信任代理头',
  'security.allowedOrigins': 'CORS 来源白名单',

  resource: '资源监控',
  'resource.enabled': '启用监控',
  'resource.samplingInterval': '实时采样间隔',
  'resource.collectInterval': '历史采集间隔',
  'resource.retentionDays': '历史保留天数',

  auth: '身份认证',
  'auth.ldap': 'LDAP',
  'auth.ldap.enabled': '启用 LDAP',
  'auth.ldap.host': '服务器地址',
  'auth.ldap.port': '端口',
  'auth.ldap.useSSL': '使用 LDAPS',
  'auth.ldap.baseDN': 'Base DN',
  'auth.ldap.bindDN': '绑定 DN',
  'auth.ldap.bindPassword': '绑定密码',
  'auth.ldap.userFilter': '用户过滤器',
  'auth.ldap.syncInterval': '目录同步间隔(秒)',
  'auth.ldap.autoCreateUser': '自动创建本地用户',
  'auth.ldap.insecureSkipVerify': '跳过证书校验',
  'auth.oauth2': 'OAuth2 / OIDC',
  'auth.oauth2.enabled': '启用外部认证',
  'auth.oauth2.providerName': '提供方名称',
  'auth.oauth2.clientId': 'Client ID',
  'auth.oauth2.clientSecret': 'Client Secret',
  'auth.oauth2.authUrl': '授权端点',
  'auth.oauth2.tokenUrl': '令牌端点',
  'auth.oauth2.userInfoUrl': '用户信息端点',
  'auth.oauth2.issuer': 'OIDC Issuer',
  'auth.oauth2.scopes': 'Scopes',
  'auth.oauth2.redirectUrl': '回调地址',
  'auth.oauth2.usernameClaim': '用户名取值字段',
  'auth.oauth2.displayNameClaim': '显示名取值字段',
  'auth.oauth2.emailClaim': '邮箱取值字段',
  'auth.oauth2.autoCreateUser': '自动创建本地用户',
  'auth.oauth2.insecureSkipVerify': '跳过证书校验'
}

// 顶层设置节 -> 展示分组名
const SECTION_NAMES = {
  appName: '应用信息',
  account: '账户',
  server: '服务配置',
  database: '数据库',
  rootDirs: '共享目录',
  allowedExtensions: '访问控制',
  privateFiles: '私有文件',
  privateQuotaGlobal: '私有文件',
  privateQuotaUser: '私有文件',
  tempFiles: '临时文件',
  tempFilesQuotaGlobal: '临时文件',
  tempFilesQuotaPerIP: '临时文件',
  upload: '上传配置',
  download: '下载限制',
  preview: '预览配置',
  mimeTable: 'MIME 表',
  openApi: '访问控制',
  scanStartDelaySeconds: '存储索引',
  scanCronExpression: '存储索引',
  searchReconcileCronExpression: '存储索引',
  security: '安全配置',
  resource: '资源监控',
  auth: '身份认证'
}

// 顶层设置节 -> 设置页侧栏面板 key，用于从变更明细跳转到对应分区
const SECTION_PANELS = {
  appName: 'appInfo',
  account: 'service',
  server: 'service',
  database: 'database',
  rootDirs: 'appInfo',
  allowedExtensions: 'access',
  privateFiles: 'filesQuota',
  privateQuotaGlobal: 'filesQuota',
  privateQuotaUser: 'filesQuota',
  tempFiles: 'filesQuota',
  tempFilesQuotaGlobal: 'filesQuota',
  tempFilesQuotaPerIP: 'filesQuota',
  upload: 'upload',
  download: 'access',
  preview: 'preview',
  mimeTable: 'mime',
  openApi: 'access',
  scanStartDelaySeconds: 'storage',
  scanCronExpression: 'storage',
  searchReconcileCronExpression: 'storage',
  security: 'access',
  resource: 'resource',
  auth: 'auth'
}

// 以字节为单位的字段，按人类可读大小展示
const SIZE_FIELDS = new Set([
  'upload.chunkSize',
  'upload.maxFileSize',
  'preview.maxInlineSize',
  'preview.textChunkSize',
  'privateQuotaGlobal',
  'privateQuotaUser',
  'tempFilesQuotaGlobal',
  'tempFilesQuotaPerIP'
])

// 敏感字段，仅以掩码展示，避免明文回显
const SECRET_FIELDS = new Set([
  'database.mysqlPassword',
  'database.postgresPassword',
  'auth.ldap.bindPassword',
  'auth.oauth2.clientSecret'
])

// 枚举值中文名
const VALUE_LABELS = {
  'database.driver': { sqlite: 'SQLite', mysql: 'MySQL', postgres: 'PostgreSQL' },
  'openApi.ipAccessMode': { allow: '白名单模式', deny: '黑名单模式', none: '不限制' }
}

const isPlainObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)

// 去掉数组下标，得到可用于查表的路径（rootDirs[0].path -> rootDirs.path）
const stripIndex = (path) => path.replace(/\[\d+\]/g, '')

// 由路径推导展示标签：rootDirs[0].path -> 共享目录 #1 · 目录路径
const labelFor = (path) => {
  const parts = []
  let lookup = ''
  for (const seg of path.split('.')) {
    const m = seg.match(/^([^[\]]*)(?:\[(\d+)\])?$/)
    const key = m ? m[1] : seg
    const idx = m && m[2] !== undefined ? Number(m[2]) : null
    lookup = lookup ? `${lookup}.${key}` : key
    let label = FIELD_LABELS[lookup]
    if (!label) {
      label = lookup.startsWith('mimeTable.') ? `.${key}` : key
    }
    if (idx !== null) label = `${label} #${idx + 1}`
    parts.push(label)
  }
  return parts.join(' · ')
}

// 将原始值格式化为可读文本
const formatValue = (path, val) => {
  const basePath = stripIndex(path)

  if (SECRET_FIELDS.has(basePath)) {
    return val ? '••••••' : '（空）'
  }
  if (val === undefined || val === null || val === '') return '（空）'
  if (typeof val === 'boolean') return val ? '开启' : '关闭'
  if (Array.isArray(val)) return val.length ? val.join('、') : '（空）'
  if (isPlainObject(val)) {
    if (basePath === 'rootDirs') {
      const name = val.name || val.path || '未命名目录'
      return val.uploadEnabled === false ? `${name}（禁止上传）` : name
    }
    return JSON.stringify(val)
  }

  const enumMap = VALUE_LABELS[basePath]
  if (enumMap && enumMap[val] !== undefined) return enumMap[val]

  if (SIZE_FIELDS.has(basePath)) {
    if (basePath === 'upload.maxFileSize' && Number(val) === 0) return '无限制'
    return formatSize(Number(val))
  }

  return String(val).replace(/\n/g, '、')
}

// 逐字段对比当前设置与基线快照，返回发生变化的条目
export const diffSettings = (current, base) => {
  const changes = []

  const push = (path, oldVal, newVal) => {
    changes.push({
      path,
      label: labelFor(path),
      oldText: formatValue(path, oldVal),
      newText: formatValue(path, newVal)
    })
  }

  const walk = (cur, old, path) => {
    if (isPlainObject(cur) && isPlainObject(old)) {
      const keys = new Set([...Object.keys(old), ...Object.keys(cur)])
      for (const k of keys) walk(cur[k], old[k], path ? `${path}.${k}` : k)
      return
    }
    if (Array.isArray(cur) || Array.isArray(old)) {
      const a = Array.isArray(cur) ? cur : []
      const b = Array.isArray(old) ? old : []
      if (JSON.stringify(a) === JSON.stringify(b)) return
      // 对象数组按下标逐项对比，普通数组整体对比
      if (a.some(isPlainObject) || b.some(isPlainObject)) {
        const len = Math.max(a.length, b.length)
        for (let i = 0; i < len; i++) walk(a[i], b[i], `${path}[${i}]`)
        return
      }
      push(path, b, a)
      return
    }
    if (JSON.stringify(cur) !== JSON.stringify(old)) push(path, old, cur)
  }

  walk(current, base, '')
  return changes
}

// 按顶层设置节分组，便于在抽屉中分区展示；同时带出该分区对应的设置面板 key
export const groupChanges = (changes) => {
  const groups = []
  const index = new Map()
  for (const change of changes) {
    const top = change.path.split('.')[0].replace(/\[\d+\]$/, '')
    const name = SECTION_NAMES[top] || top
    if (!index.has(name)) {
      const group = { name, key: top, panel: SECTION_PANELS[top] || '', items: [] }
      index.set(name, group)
      groups.push(group)
    }
    index.get(name).items.push(change)
  }
  return groups
}