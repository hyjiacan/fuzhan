import { formatSize } from './useSettingsUtils'

// 设置项中文标签：后端 snake_case 路径 -> 展示名称（含中间容器节点）。
// 变更明细的原始路径来自前端内部 camelCase 的 settings 对象，
// 查表前统一经 toSnakePath 归一化，保证标签与后端契约键一致。
const FIELD_LABELS = {
  app_name: '应用名称',

  account: '账户',
  'account.anonymous': '匿名账户',
  'account.anonymous.username': '公开用户名',
  'account.reserved_usernames': '保留用户名',
  'account.allow_registration': '允许自行注册',
  'account.require_login': '强制登录',
  'account.captcha': '登录验证码',
  'account.captcha.mode': '验证码模式',
  'account.captcha.trigger_after': '失败触发阈值',

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
  'server.ftp.passive_port_start': '被动端口起始',
  'server.ftp.passive_port_end': '被动端口结束',
  'server.ftps': 'FTPS',
  'server.ftps.enabled': '启用 FTPS',
  'server.ftps.port': 'FTPS 端口',
  'server.webdav': 'WebDAV',
  'server.webdav.enabled': '启用 WebDAV',
  'server.tls': 'TLS 证书',
  'server.tls.cert_file': '证书文件',
  'server.tls.key_file': '密钥文件',

  database: '数据库',
  'database.driver': '数据库类型',
  'database.dsn': '数据库连接串',
  'database.mysql_host': 'MySQL 主机地址',
  'database.mysql_port': 'MySQL 端口',
  'database.mysql_user': 'MySQL 用户名',
  'database.mysql_password': 'MySQL 密码',
  'database.mysql_database': 'MySQL 数据库名',
  'database.postgres_host': 'PostgreSQL 主机地址',
  'database.postgres_port': 'PostgreSQL 端口',
  'database.postgres_user': 'PostgreSQL 用户名',
  'database.postgres_password': 'PostgreSQL 密码',
  'database.postgres_database': 'PostgreSQL 数据库名',

  root_dirs: '共享目录',
  'root_dirs.path': '目录路径',
  'root_dirs.name': '显示名称',
  'root_dirs.upload_enabled': '允许上传',

  allowed_extensions: '允许的扩展名',

  storage_backend: '存储后端',
  'storage_backend.backend': '存储类型',
  'storage_backend.s3': 'S3/MinIO',
  'storage_backend.s3.endpoint': '服务端点',
  'storage_backend.s3.access_key': 'AccessKey',
  'storage_backend.s3.secret_key': 'SecretKey',
  'storage_backend.s3.bucket': 'Bucket',
  'storage_backend.s3.region': '区域',
  'storage_backend.s3.use_ssl': '使用 HTTPS',
  'storage_backend.oss': '阿里云 OSS',
  'storage_backend.oss.endpoint': '服务端点',
  'storage_backend.oss.access_key': 'AccessKey',
  'storage_backend.oss.secret_key': 'SecretKey',
  'storage_backend.oss.bucket': 'Bucket',
  'storage_backend.webdav': 'WebDAV',
  'storage_backend.webdav.url': '服务地址',
  'storage_backend.webdav.username': '用户名',
  'storage_backend.webdav.password': '密码',
  'storage_backend.alert': '存储告警',
  'storage_backend.alert.enabled': '启用存储告警',
  'storage_backend.alert.threshold_percent': '告警阈值(%)',
  'storage_backend.alert.check_interval_minutes': '检测间隔(分钟)',
  'storage_backend.alert.cooldown_minutes': '重复告警间隔(分钟)',
  'storage_backend.alert.web_enabled': 'Web 消息通知',
  'storage_backend.alert.webhook_enabled': 'Webhook 推送',
  'storage_backend.alert.email_enabled': '邮件通知',
  'storage_backend.alert.email_recipients': '邮件收件人',
  'storage_backend.alert.smtp': 'SMTP',
  'storage_backend.alert.smtp.host': 'SMTP 服务器',
  'storage_backend.alert.smtp.port': 'SMTP 端口',
  'storage_backend.alert.smtp.username': 'SMTP 用户名',
  'storage_backend.alert.smtp.password': 'SMTP 密码',
  'storage_backend.alert.smtp.from': '发件人',
  'storage_backend.alert.smtp.tls_mode': '加密方式',

  private_files: '私有文件',
  'private_files.enabled': '启用私有文件',
  'private_files.path': '存储路径',
  private_quota_global: '私有文件全局配额',
  private_quota_user: '私有文件用户配额',

  temp_files: '临时文件',
  'temp_files.enabled': '启用临时文件',
  'temp_files.path': '存储路径',
  'temp_files.default_expire_days': '默认过期天数',
  'temp_files.delete_on_download': '下载后删除',
  temp_files_quota_global: '临时文件全局配额',
  temp_files_quota_per_ip: '临时文件 IP 配额',

  upload: '上传配置',
  'upload.chunk_size': '分片大小',
  'upload.max_file_size': '最大文件大小',
  'upload.url_upload': 'URL 上传',
  'upload.url_upload.enabled': '启用 URL 上传',
  'upload.url_upload.insecure_skip_verify': '跳过证书验证',
  'upload.url_upload.allowed_ip_ranges': '允许 IP 网段',
  'upload.url_upload.max_concurrent': '并行上传数',
  'upload.url_upload.max_retries': '自动重试次数',
  'upload.rate_limit': '上传频率限制',
  'upload.rate_limit.window_minutes': '窗口(分钟)',
  'upload.rate_limit.max_requests': '最大请求数',

  download: '下载限制',
  'download.rate_limit': '下载频率限制',
  'download.rate_limit.window_minutes': '窗口(分钟)',
  'download.rate_limit.max_requests': '最大请求数',
  'download.rate_limit.lock_after': '失效阈值',
  'download.rate_limit.lock_minutes': '锁定(分钟)',

  preview: '预览配置',
  'preview.enabled': '全局预览开关',
  'preview.groups': '预览类型分组',
  'preview.groups.image': '图片分组',
  'preview.groups.text': '文本分组',
  'preview.groups.audio': '音频分组',
  'preview.groups.video': '视频分组',
  'preview.groups.document': '文档分组',
  'preview.groups.custom': '自定义分组',

  open_api: '访问控制',
  'open_api.enabled': '启用 OpenAPI',
  'open_api.ip_access_mode': '访问模式',
  'open_api.ip_whitelist': 'IP 白名单',
  'open_api.ip_blacklist': 'IP 黑名单',
  'open_api.domain_whitelist': '域名白名单',
  'open_api.rate_limit_enabled': '频率限制',
  'open_api.requests_per_minute': '请求频率(次/分钟)',

  security: '安全配置',
  'security.trust_proxy': '信任代理头',
  'security.allowed_origins': 'CORS 来源白名单',

  auth: '身份认证',
  'auth.ldap': 'LDAP',
  'auth.ldap.enabled': '启用 LDAP',
  'auth.ldap.host': '服务器地址',
  'auth.ldap.port': '端口',
  'auth.ldap.use_ssl': '使用 LDAPS',
  'auth.ldap.base_dn': 'Base DN',
  'auth.ldap.bind_dn': '绑定 DN',
  'auth.ldap.bind_password': '绑定密码',
  'auth.ldap.user_filter': '用户过滤器',
  'auth.ldap.username_attribute': '用户名属性',
  'auth.ldap.sync_interval': '目录同步间隔(秒)',
  'auth.ldap.auto_create_user': '自动创建本地用户',
  'auth.ldap.insecure_skip_verify': '跳过证书校验',
  'auth.oauth2': 'OAuth2 / OIDC',
  'auth.oauth2.enabled': '启用外部认证',
  'auth.oauth2.provider_name': '提供方名称',
  'auth.oauth2.client_id': 'Client ID',
  'auth.oauth2.client_secret': 'Client Secret',
  'auth.oauth2.auth_url': '授权端点',
  'auth.oauth2.token_url': '令牌端点',
  'auth.oauth2.user_info_url': '用户信息端点',
  'auth.oauth2.issuer': 'OIDC Issuer',
  'auth.oauth2.scopes': 'Scopes',
  'auth.oauth2.redirect_url': '回调地址',
  'auth.oauth2.username_claim': '用户名取值字段',
  'auth.oauth2.display_name_claim': '显示名取值字段',
  'auth.oauth2.email_claim': '邮箱取值字段',
  'auth.oauth2.auto_create_user': '自动创建本地用户',
  'auth.oauth2.insecure_skip_verify': '跳过证书校验'
}

// 顶层设置节（snake_case）-> 展示分组名
const SECTION_NAMES = {
  app_name: '应用信息',
  account: '账户',
  server: '服务配置',
  database: '数据库',
  root_dirs: '共享目录',
  allowed_extensions: '访问控制',
  storage_backend: '存储后端',
  private_files: '私有文件',
  private_quota_global: '私有文件',
  private_quota_user: '私有文件',
  temp_files: '临时文件',
  temp_files_quota_global: '临时文件',
  temp_files_quota_per_ip: '临时文件',
  upload: '上传配置',
  download: '下载限制',
  preview: '预览配置',
  open_api: '访问控制',
  security: '安全配置',
  auth: '身份认证'
}

// 顶层设置节（snake_case）-> 设置页侧栏面板 key，用于从变更明细跳转到对应分区
const SECTION_PANELS = {
  app_name: 'appInfo',
  account: 'service',
  server: 'service',
  database: 'database',
  root_dirs: 'appInfo',
  allowed_extensions: 'access',
  storage_backend: 'storage',
  private_files: 'filesQuota',
  private_quota_global: 'filesQuota',
  private_quota_user: 'filesQuota',
  temp_files: 'filesQuota',
  temp_files_quota_global: 'filesQuota',
  temp_files_quota_per_ip: 'filesQuota',
  upload: 'upload',
  download: 'access',
  preview: 'preview',
  open_api: 'access',
  security: 'access',
  auth: 'auth'
}

// 以字节为单位的字段，按人类可读大小展示
const SIZE_FIELDS = new Set([
  'upload.chunk_size',
  'upload.max_file_size',
  'preview.groups.text.max_inline_size',
  'preview.groups.text.text_chunk_size',
  'private_quota_global',
  'private_quota_user',
  'temp_files_quota_global',
  'temp_files_quota_per_ip'
])

// 敏感字段，仅以掩码展示，避免明文回显
const SECRET_FIELDS = new Set([
  'database.mysql_password',
  'database.postgres_password',
  'auth.ldap.bind_password',
  'auth.oauth2.client_secret',
  'storage_backend.s3.secret_key',
  'storage_backend.oss.secret_key',
  'storage_backend.webdav.password',
  'storage_backend.alert.smtp.password'
])

// 枚举值中文名
const VALUE_LABELS = {
  'database.driver': { sqlite: 'SQLite', mysql: 'MySQL', postgres: 'PostgreSQL' },
  'open_api.ip_access_mode': { allow: '白名单模式', deny: '黑名单模式', none: '不限制' },
  'storage_backend.backend': { local: '本地磁盘', s3: 'S3', minio: 'MinIO', oss: '阿里云 OSS', webdav: 'WebDAV' },
  'storage_backend.alert.smtp.tls_mode': { none: '不加密', starttls: 'STARTTLS', ssl: 'SSL/TLS' },
  'account.captcha.mode': { disabled: '关闭', always: '始终开启', after_n: '失败后开启' }
}

// 未单独登记的叶子字段通用中文名（如预览分组内的 enabled/mimes/exts）
const LEAF_LABELS = {
  enabled: '启用',
  mimes: 'MIME 类型',
  exts: '文件扩展名',
  max_inline_size: '最大内联大小',
  text_chunk_size: '文本分块大小'
}

const isPlainObject = (v) => v !== null && typeof v === 'object' && !Array.isArray(v)

// 去掉数组下标，得到可用于查表的路径（root_dirs[0].path -> root_dirs.path）
const stripIndex = (path) => path.replace(/\[\d+\]/g, '')

// camelCase 片段转后端 snake_case（maxInlineSize -> max_inline_size，
// allowedIPRanges -> allowed_ip_ranges）；已是 snake 或纯小写的片段原样返回，
// 避免影响 account/server 等顶层键。
const camelToSnake = (key) => key
  .replace(/([A-Z]+)([A-Z][a-z])/g, '$1_$2')
  .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
  .toLowerCase()

// 路径整体归一化为后端 snake_case（rootDirs[0].uploadEnabled -> root_dirs[0].upload_enabled）
const toSnakePath = (path) => path
  .split('.')
  .map(seg => seg.replace(/^([^[\]]*)/, (m) => camelToSnake(m)))
  .join('.')

// 由路径推导展示标签：rootDirs[0].path -> 共享目录 #1 · 目录路径
const labelFor = (path) => {
  const parts = []
  let lookup = ''
  for (const seg of path.split('.')) {
    const m = seg.match(/^([^[\]]*)(?:\[(\d+)\])?$/)
    const key = m ? camelToSnake(m[1]) : camelToSnake(seg)
    const idx = m && m[2] !== undefined ? Number(m[2]) : null
    lookup = lookup ? `${lookup}.${key}` : key
    let label = FIELD_LABELS[lookup]
    if (!label) label = LEAF_LABELS[key] || key
    if (idx !== null) label = `${label} #${idx + 1}`
    parts.push(label)
  }
  return parts.join(' · ')
}

// 将原始值格式化为可读文本（路径统一按 snake_case 查表）
const formatValue = (path, val) => {
  const basePath = toSnakePath(stripIndex(path))

  if (SECRET_FIELDS.has(basePath)) {
    return val ? '••••••' : '（空）'
  }
  if (val === undefined || val === null || val === '') return '（空）'
  if (typeof val === 'boolean') return val ? '开启' : '关闭'
  if (Array.isArray(val)) return val.length ? val.join('、') : '（空）'
  if (isPlainObject(val)) {
    if (basePath === 'root_dirs') {
      const name = val.name || val.path || '未命名目录'
      return val.uploadEnabled === false ? `${name}（禁止上传）` : name
    }
    return JSON.stringify(val)
  }

  const enumMap = VALUE_LABELS[basePath]
  if (enumMap && enumMap[val] !== undefined) return enumMap[val]

  if (SIZE_FIELDS.has(basePath)) {
    if (basePath === 'upload.max_file_size' && Number(val) === 0) return '无限制'
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
    const topKey = camelToSnake(top)
    const name = SECTION_NAMES[topKey] || top
    if (!index.has(name)) {
      const group = { name, key: topKey, panel: SECTION_PANELS[topKey] || '', items: [] }
      index.set(name, group)
      groups.push(group)
    }
    index.get(name).items.push(change)
  }
  return groups
}
