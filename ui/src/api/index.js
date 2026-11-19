import axios from 'axios'
// 按路径引入命令式组件，避免从 element-plus 总入口引入（总入口在打包时可能拖入未用组件）
import { ElMessage } from 'element-plus/es/components/message/index'
import { createErrorFromResponse, createNetworkError } from '../utils/error.js'
import { beginRequest, endRequest } from '../utils/requestLoading.js'

const API_BASE = '/api/v1/web'

// 创建 axios 实例
const request = axios.create({
  baseURL: API_BASE,
  timeout: 30000,
  // 与后端同源请求自动携带 HttpOnly 登录 Cookie（Vue 与 lite 共享同一会话凭证）。
  // 生产/开发代理均为同源，不依赖 CORS 凭证跨域；显式声明保证与后端 Cookie 会话链路正确。
  withCredentials: true
})

// 写入类请求的 success 反馈。组件内已有显式提示的调用方通过 skipSuccessToast 跳过，
// 避免与组件文案重复；未显式提示的操作（如后台任务）由这里统一兜底。
const MUTATING_METHODS = ['post', 'put', 'patch', 'delete']
let lastSuccessMsg = ''
let lastSuccessTime = 0

// 提示写入类操作成功。忽略无 message 的响应；短时间内的相同文案只提示一次。
function notifyWriteSuccess(method, config, response) {
  if (!MUTATING_METHODS.includes((method || '').toLowerCase())) return
  if (config && config.skipSuccessToast) return
  if (!response || response.success !== true) return
  const msg = response.message
  if (!msg) return
  const now = Date.now()
  if (msg === lastSuccessMsg && now - lastSuccessTime < 1600) return
  lastSuccessMsg = msg
  lastSuccessTime = now
  ElMessage.success(msg)
}

// 导出 request 实例供需要动态 URL 的组件使用（如 UploadManager）
export { request }

// 将文件/目录路径编码为 URL 路径段；根目录返回带结尾斜杠的形式（如 /files/list/）
function fileUrl(base, path = '') {
  const seg = String(path)
    .split('/')
    .filter(Boolean)
    .map(encodeURIComponent)
    .join('/')
  return seg ? `${base}/${seg}` : `${base}/`
}

// 将前端 camelCase 查询参数转换为后端 snake_case（仅用于透传 params 的接口）
function snakeParams(obj = {}) {
  const out = {}
  for (const [k, v] of Object.entries(obj)) {
    if (v === undefined || v === null || v === '') continue
    const key = k === 'q' ? 'query' : k.replace(/[A-Z]/g, m => '_' + m.toLowerCase())
    out[key] = v
  }
  return out
}

// 错误处理回调（可在应用初始化时设置）
let globalErrorHandler = null

export function setGlobalErrorHandler(handler) {
  globalErrorHandler = handler
}

// 获取或生成 AnonymousID
function getAnonymousId() {
  let id = localStorage.getItem('anonymous_id')
  if (!id) {
    id = crypto.randomUUID ? crypto.randomUUID() : 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
      const r = (Math.random() * 16) | 0
      return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16)
    })
    localStorage.setItem('anonymous_id', id)
  }
  return id
}

// 请求拦截器：添加匿名标识。
// 认证凭证统一由后端 HttpOnly Cookie 承载（同源请求自动携带），不再手动附加 Bearer 头，
// 使 Vue 与 lite 两端共享同一登录状态。
request.interceptors.request.use(
  config => {
    beginRequest()
    // 所有请求都携带 AnonymousID
    config.headers['X-Anonymous-ID'] = getAnonymousId()
    return config
  },
  error => {
    endRequest()
    return Promise.reject(error)
  }
)

// 响应拦截器：处理错误
request.interceptors.response.use(
  response => {
    endRequest()
    // 令牌刷新由后端直接写回 HttpOnly Cookie，响应体/响应头都不再回传令牌
    // 写入类操作成功时给出反馈
    notifyWriteSuccess(response.config?.method, response.config, response.data)
    return response.data
  },
  error => {
    endRequest()
    // 区分网络错误和业务错误
    let appError
    if (error.response) {
      // 服务器返回了错误响应
      if (error.response.status === 401) {
        // Token 过期或无效，清除认证信息并派发登录态变更，
        // 由路由守卫在「强制登录」模式下统一重定向到登录页。
        // 少数接口把 401 用作业务错误（如目录密码错误），可通过 skipAuthSideEffects 保留当前登录态
        if (!error.config?.skipAuthSideEffects) {
          localStorage.removeItem('token')
          localStorage.removeItem('uuid')
          localStorage.removeItem('username')
          localStorage.removeItem('displayName')
          localStorage.removeItem('nickname')
          localStorage.removeItem('userRole')
          window.dispatchEvent(new CustomEvent('auth:changed'))
        }
      } else if (error.response.status === 403) {
        // 无权限，跳转到文件页面；少数接口把 403 用作业务错误（如冲突确认中的管理员账号），
        // 可通过 skipAuthSideEffects 避免打断当前页面流程
        if (!error.config?.skipAuthSideEffects) {
          window.location.hash = '#/files'
        }
      }
      appError = createErrorFromResponse(error.response)
    } else {
      // 网络错误（无响应）
      appError = createNetworkError(error)
    }

    // 401 不弹全局提示，交给路由守卫跳转（避免匿名请求失败时反复弹出「未登录」）；
    // 组件内已在页面内展示错误文案的接口可通过 skipGlobalError 跳过全局提示，避免重复提示
    const silentAuthError = error.response?.status === 401
    if (globalErrorHandler && !silentAuthError && !error.config?.skipGlobalError) {
      globalErrorHandler(appError, error)
    }

    return Promise.reject(appError)
  }
)

// ========== 认证 API ==========
export const AuthApi = {
  getToken() {
    return localStorage.getItem('token')
  },

  isLoggedIn() {
    return !!this.getToken()
  },

  // 用服务端用户信息恢复前端登录态。凭证只存在于后端 HttpOnly Cookie，
  // 前端脚本读不到令牌，登录成功与否以 /auth/user 是否通过为准。
  // 本地 token 仅作为「已登录」界面标记的哨兵值，不会作为请求认证凭证使用。
  restoreSession(userData = {}) {
    localStorage.setItem('token', '__cookie__')
    if (userData.uuid) localStorage.setItem('uuid', userData.uuid)
    if (userData.username) localStorage.setItem('username', userData.username)
    localStorage.setItem('displayName', userData.display_name || userData.displayName || '')
    localStorage.setItem('nickname', userData.nickname || '')
  },

  // 登出：清除后端 HttpOnly Cookie（前端无法直接删除，必须请求后端）。
  // Cookie 清除后，响应拦截器收到 401 会自动清理本地状态；此处也返回成功由调用方清理。
  logout() {
    // Cookie 为 HttpOnly，脚本无法删除；请求后端清除，并静默处理失败
    return request.post('/auth/logout', null, { skipSuccessToast: true })
  },

  clearAuth() {
    localStorage.removeItem('token')
    localStorage.removeItem('uuid')
    localStorage.removeItem('username')
    localStorage.removeItem('displayName')
    localStorage.removeItem('nickname')
    localStorage.removeItem('userRole')
  },

  getUserInfo() {
    return request.get('/auth/user')
  },

  // 获取图形验证码：返回 { captcha_id, image }，image 为 data:image/png;base64,... data URI
  getCaptcha() {
    return request.get('/auth/captcha')
  },

  // 注册/登录：验证码为可选字段（缺省空串，向后兼容不展示验证码的旧流程）。
  // 一次性验证码校验失败后必须重新 getCaptcha 刷新
  register(username, password, captchaId = '', captchaAnswer = '') {
    // 注册反馈由组件内显式提示（含后续引导文案），跳过拦截器自动 toast 避免重复
    return request.post('/auth/register', { username, password, captcha_id: captchaId, captcha_answer: captchaAnswer }, { skipSuccessToast: true })
  },

  login(username, password, captchaId = '', captchaAnswer = '') {
    // 登录成功的反馈由组件内显式提示，跳过拦截器自动 toast 避免重复
    return request.post('/auth/login', { username, password, captcha_id: captchaId, captcha_answer: captchaAnswer }, { skipSuccessToast: true })
  },

  changePassword(oldPassword, newPassword) {
    return request.put('/auth/password', { old_password: oldPassword, new_password: newPassword }, { skipSuccessToast: true })
  },

  // 更新个人资料（姓名、昵称）；用户名不可修改
  updateProfile(displayName, nickname) {
    return request.put('/auth/profile', { display_name: displayName, nickname }, { skipSuccessToast: true })
  },

  // 注销账号：需再次输入当前密码确认；成功后服务端会清除该用户的私有/临时文件
  deleteAccount(password) {
    return request.delete('/auth/account', { data: { password }, skipSuccessToast: true })
  },

  // 外部 OAuth2/OIDC：登录页查询是否启用（不返回提供方等实现细节）
  oauth2Status() {
    return request.get('/auth/oauth2/status')
  },

  // 外部 OAuth2/OIDC：获取授权跳转地址（由前端执行跳转，便于统一处理错误）
  oauth2Authorize() {
    return request.get('/auth/oauth2/authorize')
  },

  // 外部认证同名冲突：查询当前回调是否处于待确认状态。
  // 待确认票据由后端 HttpOnly Cookie 维护，前端不接触、不传递。
  getIdentityConfirm() {
    return request.get('/auth/identity-confirm', { skipSuccessToast: true })
  },

  // 同名冲突：用本地密码确认绑定到同名本地账号；成功后返回登录会话数据。
  // 错误由确认页在页面内展示（el-alert），跳过全局提示避免重复
  confirmIdentityBind(password) {
    return request.post('/auth/identity-confirm/bind', { password }, { skipSuccessToast: true, skipAuthSideEffects: true, skipGlobalError: true })
  },

  // 同名冲突：改用独立账号登录（后端创建独立账号并建立会话）
  createIdentityAccount() {
    return request.post('/auth/identity-confirm/create-account', {}, { skipSuccessToast: true, skipAuthSideEffects: true, skipGlobalError: true })
  }
}

// ========== 配置 API ==========
export const ConfigApi = {
  get() {
    return request.get('/config')
  },

  save(config) {
    return request.post('/config', config, { skipSuccessToast: true })
  },

  // 连接测试的成功/失败提示由调用方组件自行展示，跳过拦截器自动 toast，避免重复提示
  testLdap(ldapConfig) {
    return request.post('/config/ldap/test', ldapConfig, { skipSuccessToast: true })
  },

  testOAuth2(oauth2Config) {
    return request.post('/config/oauth2/test', oauth2Config, { skipSuccessToast: true })
  },

  testWebhook(webhookConfig) {
    return request.post('/config/webhook/test', webhookConfig, { skipSuccessToast: true })
  },

  webhookEvents() {
    return request.get('/config/webhook/events')
  },

  // 本机磁盘分区列表（含总容量/剩余容量），供共享目录选择存储分区
  partitions() {
    return request.get('/config/partitions')
  }
}

// ========== 服务器资源监控 API ==========
export const ResourceApi = {
  snapshot() {
    return request.get('/admin/resource/snapshot')
  },

  history(scope, range) {
    return request.get('/admin/resource/history', { params: snakeParams({ scope, range }) })
  }
}

// ========== 文件 API ==========
export const FileApi = {
  list(path = '') {
    return request.get(fileUrl('/files/list', path))
  },

  recent(page = 1, pageSize = 20, action = 'upload') {
    return request.get('/files/recent', { params: { page, page_size: pageSize, action } })
  },

  recentCarousel() {
    return request.get('/files/recent/carousel')
  },

  preview(path) {
    const encodedPath = path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
    return request.get(`/files/preview/${encodedPath}`)
  },

  previewChunk(path, chunkIndex) {
    const encodedPath = path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
    return request.get(`/files/preview-chunk/${encodedPath}`, { params: { chunk: chunkIndex } })
  },

  // 计算已上传文件校验值（由服务器对已存储文件计算；payload 含 storage_type 及对应定位字段与 algorithms）
  computeChecksum(payload) {
    return request.post('/files/checksum', payload, { skipSuccessToast: true })
  },

  // 公开文件自助管理：权限由后端按「管理员 / 上传者IP / 目录开关」判定，前端仅按 can_* 字段展示入口
  rename(path, newName) {
    return request.post('/files/rename', { path, new_name: newName }, { skipSuccessToast: true })
  },

  move(oldPath, newPath) {
    return request.post('/files/move', { path: oldPath, target_path: newPath }, { skipSuccessToast: true })
  },

  delete(path) {
    return request.delete('/files', { params: { path }, skipSuccessToast: true })
  }
}

// ========== 上传 API ==========
export const UploadApi = {
  getUrlInfo(url) {
    return request.post('/uploads/url-info', { url })
  },

  uploadFromUrl(url, filename, path = '', storageType = '') {
    const data = { url, filename, path }
    if (storageType) {
      data.storage_type = storageType
    }
    return request.post('/uploads/url-tasks', data)
  },

  uploadFromUrlLocal(url, filename, path) {
    return request.post('/uploads/url-tasks', { url, filename, path })
  },

  // 批量探测 URL：返回文件名/大小、目标是否已存在、该 URL 是否已成功上传过
  probeUrls(urls, path = '', storageType = '') {
    return request.post('/uploads/url-tasks/probe', { urls, path, storage_type: storageType }, { skipSuccessToast: true })
  },

  // 分片上传
  session: {
    create(data) {
      return request.post('/uploads/sessions', data)
    },
    get(id) {
      return request.get(`/uploads/sessions/${id}`)
    },
    resume(id) {
      return request.post(`/uploads/sessions/${id}/resume`)
    },
    cancel(id) {
      return request.delete(`/uploads/sessions/${id}`, { skipSuccessToast: true })
    }
  },

  chunk: {
    upload(formData) {
      return request.post('/uploads/chunks', formData, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })
    }
  },

  finalize(uploadId) {
    return request.post('/uploads/finalize', { upload_id: uploadId })
  },

  // URL 上传任务进度查询
  getURLTask(taskId) {
    return request.get(`/uploads/url-tasks/${taskId}`)
  },

  // 当前用户上传状态/会话
  listSessions(type, page = 1, pageSize = 50) {
    return request.get('/uploads/sessions', { params: { type, page, page_size: pageSize } })
  },

  listURLTasks(type, page = 1, pageSize = 50) {
    return request.get('/uploads/url-tasks', { params: { type, page, page_size: pageSize } })
  },

  cancelURLTask(taskId) {
    return request.post(`/uploads/url-tasks/${taskId}/cancel`, null, { skipSuccessToast: true })
  },

  retryURLTask(taskId) {
    return request.post(`/uploads/url-tasks/${taskId}/retry`, null, { skipSuccessToast: true })
  },

  // 计算已完成 URL 上传任务的校验值（algorithms: md5/sha1/sha256 的任意子集）
  computeChecksum(taskId, algorithms) {
    return request.post(`/uploads/url-tasks/${taskId}/checksum`, { algorithms }, { skipSuccessToast: true })
  },
}

// ========== 文件索引 API ==========
export const IndexApi = {
  // 查询索引记录
  listRecords(params = {}) {
    return request.get('/admin/index/records', { params: snakeParams(params) })
  },

  // 获取索引统计
  getStats() {
    return request.get('/admin/index/stats')
  },

  // 触发全量扫描
  triggerScan() {
    return request.post('/admin/index/scan', null, { skipSuccessToast: true })
  },

  // 触发全量扫描（所有 scope）
  triggerFullScan() {
    return request.post('/admin/index/scan/trigger', null, { skipSuccessToast: true })
  },

  // 获取扫描进度
  getScanProgress() {
    return request.get('/admin/index/scan/progress')
  },

  // 获取扫描状态（公开接口，供 Footer 使用）
  getScanStatus() {
    return request.get('/files/scan/status')
  },

  // 触发一致性校验
  triggerCheck() {
    return request.post('/admin/index/check')
  },

  // 更新备注
  updateNotes(id, notes) {
    return request.put(`/admin/index/records/${id}/notes`, { notes }, { skipSuccessToast: true })
  },
  // 搜索文件记录（用于 autocomplete）
  searchRecords(query) {
    return request.get('/files/search-records', { params: { query } })
  },

  // 删除索引记录
  deleteRecord(id) {
    return request.delete(`/admin/index/records/${id}`)
  },

  // 查询重复文件
  listDuplicates(params = {}) {
    return request.get('/admin/index/duplicates', { params: snakeParams(params) })
  },

  // 保留指定文件，自动删除同哈希的其他重复文件
  keepDuplicate(id) {
    return request.post(`/admin/index/duplicates/${id}/keep`, null, { skipSuccessToast: true })
  }
}

// ========== 文件依赖 API ==========
export const DependencyApi = {
  create(fileRecordId, dependsOnId, relation, description) {
    return request.post('/admin/index/dependencies', {
      file_record_id: fileRecordId, depends_on_id: dependsOnId, relation, description
    })
  },
  delete(id) {
    return request.delete(`/admin/index/dependencies/${id}`)
  },
  getByRecord(recordId) {
    return request.get(`/admin/index/records/${recordId}/dependencies`)
  },
  getTree(recordId) {
    return request.get(`/files/depends/${recordId}`)
  },
  // 公开创建（无需 admin）
  createPublic(fileRecordId, dependsOnId, relation, description) {
    return request.post('/files/dependencies', {
      file_record_id: fileRecordId, depends_on_id: dependsOnId, relation, description
    })
  },
  // 公开删除（无需 admin）
  deletePublic(id) {
    return request.delete(`/files/dependencies/${id}`)
  }
}

// ========== 搜索 API ==========
export const SearchApi = {
  search(query) {
    return request.get(`/search/${encodeURIComponent(query)}`)
  },
  // 文件名检索：自动补全（搜索框实时联想）
  autocomplete(query, limit = 10) {
    return request.get('/search-suggest', { params: { query, limit } })
  },
  // 文件名检索：拼写纠错（输入错误时给推荐）
  spellcheck(query, limit = 5) {
    return request.get('/search-spellcheck', { params: { query, limit } })
  },
  // 文件名检索：相关词推荐（输入空格后给出下一段候选）
  related(query, limit = 10) {
    return request.get('/search-related', { params: { query, limit } })
  }
}

// ========== 文件记录备注 API ==========
export const FileRecordApi = {
  // 公开获取备注
  getNotes(recordId) {
    return request.get(`/files/records/${recordId}/notes`)
  },
  // 公开更新备注
  updateNotes(recordId, notes) {
    return request.put(`/files/records/${recordId}/notes`, { notes }, { skipSuccessToast: true })
  },
  // 搜索文件记录（用于 autocomplete）
  searchFiles(query) {
    return request.get('/files/search-records', { params: { query } })
  },
  // 根据路径查找文件记录
  findRecord(fileName, rootName, filePath) {
    const rel = filePath.startsWith('/') ? filePath : `/${filePath}`
    return request.get(fileUrl('/files/record', `/${rootName}${rel}`))
  }
}

// ========== 管理员 API ==========
export const AdminApi = {
  // filters 支持 source / createdVia / identityState（camelCase 自动转 snake_case 传给后端）
  getUsers(page = 1, pageSize = 20, query = '', filters = {}) {
    return request.get('/admin/users', { params: { page, page_size: pageSize, query, ...snakeParams(filters) } })
  },

  createUser(username, displayName, password, role = 'user') {
    return request.post('/admin/users', { username, display_name: displayName, password, role }, { skipSuccessToast: true })
  },

  resetPassword(uuid, newPassword) {
    return request.put(`/admin/users/${uuid}/reset-password`, { new_password: newPassword }, { skipSuccessToast: true })
  },

  // 转为本地账号：写入新密码并删除该用户的全部外部身份绑定（auth_source 变为 local）
  convertToLocal(uuid, newPassword) {
    return request.post(`/admin/users/${uuid}/convert-local`, { new_password: newPassword }, { skipSuccessToast: true })
  },

  // 绑定外部身份：provider 由后端按当前启用的认证源决定，前端只提交认证类型与外部标识
  bindAuthIdentity(uuid, authType, externalId, externalName = '') {
    const payload = { auth_type: authType, external_id: externalId }
    // external_name 为可选字段，仅在填写时提交
    if (externalName) payload.external_name = externalName
    return request.post(`/admin/users/${uuid}/auth-bindings`, payload, { skipSuccessToast: true })
  },

  setUserDisabled(uuid, disabled) {
    return request.put(`/admin/users/${uuid}/disabled`, { disabled }, { skipSuccessToast: true })
  },

  // 身份冲突（外部认证同名账号）：默认只查待处理，params 支持 status / page / page_size
  getIdentityConflicts(params = {}) {
    return request.get('/admin/identity-conflicts', { params: snakeParams(params) })
  },

  // 批量绑定到同名本地账号：会清空这些账号的本地密码
  batchBindIdentityConflicts(ids) {
    return request.post('/admin/identity-conflicts/batch-bind', { ids }, { skipSuccessToast: true })
  },

  // 批量忽略身份冲突：仅影响待处理记录
  batchIgnoreIdentityConflicts(ids) {
    return request.post('/admin/identity-conflicts/batch-ignore', { ids }, { skipSuccessToast: true })
  },

  deleteUser(uuid) {
    return request.delete(`/admin/users/${uuid}`, { skipSuccessToast: true })
  },

  // 批量删除用户：单次上限 100，逐条返回 {uuid, username, status, message}
  batchDeleteUsers(uuids) {
    return request.post('/admin/users/batch-delete', { uuids }, { skipSuccessToast: true })
  },

  // 批量禁用用户：管理员账号与已禁用账号会被跳过
  batchDisableUsers(uuids) {
    return request.post('/admin/users/batch-disable', { uuids }, { skipSuccessToast: true })
  },

  // 批量转为本地账号：统一写入同一新密码，已是本地账号的会被跳过
  batchConvertToLocal(uuids, newPassword) {
    return request.post('/admin/users/batch-convert-local', { uuids, new_password: newPassword }, { skipSuccessToast: true })
  },

  getSessions(page = 1, pageSize = 20) {
    return request.get('/admin/sessions', { params: { page, page_size: pageSize } })
  },

  cleanupSessions(sessionIds) {
    return request.post('/admin/sessions/cleanup', { session_ids: sessionIds }, { skipSuccessToast: true })
  },

  // 一键清理全部僵尸（过期/待处理/上传中）会话——由后端重新查询决定清理哪些
  cleanupAllZombieSessions() {
    return request.post('/admin/sessions/cleanup-all', null, { skipSuccessToast: true })
  },

  // 当前在线 IP（与登录无关，依据最近请求判定）——统一分页模式
  getOnlineIps(page = 1, pageSize = 20) {
    return request.get('/admin/online-ips', { params: { page, page_size: pageSize } })
  },

  // 文件管理
  listFiles(path = '') {
    return request.get(fileUrl('/admin/files/list', path))
  },

  move(oldPath, newPath) {
    return request.post('/admin/files/move', { path: oldPath, target_path: newPath }, { skipSuccessToast: true })
  },

  // 某文件的下载记录明细（分页）；record_id 用于跟随移动/重命名后的身份关联
  listDownloadRecords({ path = '', recordId = 0, page = 1, pageSize = 10 } = {}) {
    const params = { path, page, page_size: pageSize }
    if (recordId) params.record_id = recordId
    return request.get('/admin/files/download-records', { params })
  },

  delete(path) {
    return request.delete('/admin/files', { params: { path }, skipSuccessToast: true })
  },

  // URL 上传任务管理
  getURLTasks(page = 1, pageSize = 20, status = '') {
    const params = { page, page_size: pageSize }
    if (status) params.status = status
    return request.get('/admin/url-tasks', { params })
  },

  retryURLTask(id) {
    return request.post(`/admin/url-tasks/${id}/retry`, null, { skipSuccessToast: true })
  },

  deleteURLTask(id) {
    return request.delete(`/admin/url-tasks/${id}`, { skipSuccessToast: true })
  },

  // 系统作业
  getTasks() {
    return request.get('/admin/tasks')
  },

  getTaskHistory(type = '', page = 1, pageSize = 20) {
    const params = { page, page_size: pageSize }
    if (type) params.type = type
    return request.get('/admin/tasks/history', { params })
  },

  getTask(id) {
    return request.get(`/admin/tasks/${id}`)
  },

  cancelTask(id) {
    return request.post(`/admin/tasks/${id}/cancel`)
  },

  // 搜索
  search(query) {
    return request.get(`/admin/search/${encodeURIComponent(query)}`)
  },

  // 清空操作记录
  clearRecords(action) {
    return request.post('/admin/records/clear', { action }, { skipSuccessToast: true })
  },

  // 删除单条操作记录
  deleteRecord(id, action) {
    return request.post('/admin/records/delete', { id, action }, { skipSuccessToast: true })
  },

  // 日志查看
  getLogFiles() {
    return request.get('/admin/logs/files')
  },

  // 日志内容：支持 offset 增量游标与 before 向前翻页
  getLogsTail(params) {
    return request.get('/admin/logs/tail', { params })
  }
}

// ========== 私有存储 API ==========
export const PrivateApi = {
  list(dir = '') {
    return request.get(fileUrl('/private/files', dir))
  },

  delete(code) {
    return request.delete(`/private/files/${code}`, { skipSuccessToast: true })
  },

  getQuota() {
    return request.get('/private/quota')
  },

  // 分片上传
  session: {
    create(data) {
      return request.post('/private/uploads/sessions', data)
    },
    get(id) {
      return request.get(`/private/uploads/sessions/${id}`)
    },
    resume(id) {
      return request.post(`/private/uploads/sessions/${id}/resume`)
    },
    cancel(id) {
      return request.delete(`/private/uploads/sessions/${id}`)
    }
  },

  chunk: {
    upload(formData) {
      return request.post('/private/uploads/chunks', formData, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })
    }
  },

  finalize(uploadId) {
    return request.post('/private/uploads/finalize', { upload_id: uploadId })
  },

  download(code) {
    // 私有文件下载走免认证的分享码接口（window.open 无法携带 Bearer 令牌）
    return `/share/${code}`
  }
}

// ========== 临时文件 API (基于IP，无需认证) ==========
export const TempApi = {
  list(dir = '') {
    return request.get(fileUrl('/temp/list', dir))
  },

  getQuota() {
    return request.get('/temp/quota')
  },

  getClientIP() {
    return request.get('/temp/client-ip')
  },

  getInfo(code) {
    return request.get(`/temp/${code}`)
  },

  download(code) {
    return `${API_BASE}/temp/${code}/download`
  },

  delete(code) {
    return request.delete(`/temp/${code}`, { skipSuccessToast: true })
  },

  // 分片上传
  session: {
    create(data) {
      return request.post('/temp/uploads/sessions', data)
    },
    get(id) {
      return request.get(`/temp/uploads/sessions/${id}`)
    },
    resume(id) {
      return request.post(`/temp/uploads/sessions/${id}/resume`)
    },
    cancel(id) {
      return request.delete(`/temp/uploads/sessions/${id}`)
    }
  },

  chunk: {
    upload(formData) {
      return request.post('/temp/uploads/chunks', formData, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })
    }
  },

  finalize(uploadId) {
    return request.post('/temp/uploads/finalize', { upload_id: uploadId })
  }
}

// ========== 设置 API ==========
export const SetupApi = {
  getStatus() {
    return request.get('/setup/status')
  },

  save(config) {
    return request.post('/setup/save', config, { skipSuccessToast: true })
  },

  validateDir(path) {
    return request.post('/setup/validate-dir', { path })
  },

  getNetworkInterfaces() {
    return request.get('/setup/network/interfaces')
  },

  getDefaultConfig() {
    return request.get('/setup/default-config')
  }
}

// ========== 系统 API ==========
export const SystemApi = {
  // 公开的最小配置（登录页所需）：站点名、账号开关、验证码状态、登录态判定
  getOptions() {
    return request.get('/options')
  },

  // 完整运行时配置（业务配置 + 共享目录）：未开启强制登录时匿名可访问，
  // 开启强制登录后需登录（由后端 LoginGate 拦截）。
  // 与管理侧的 /config（管理配置）不同，此接口面向 Web 界面所有访问者。
  getConfig() {
    return request.get('/runtime-config')
  },

  health() {
    return request.get('/health')
  },

  // Open API 统计
  getOpenAPIStats() {
    return request.get('/admin/open-api/stats')
  },

  // 上传 TLS 证书
  uploadCert(file) {
    const formData = new FormData()
    formData.append('file', file)
    return request.post('/admin/tls/certificate', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
  },

  // 上传 TLS 密钥
  uploadKey(file) {
    const formData = new FormData()
    formData.append('file', file)
    return request.post('/admin/tls/private-key', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
  },

  // 获取本机网络接口列表（初始化后使用，替代仅初始化阶段可用的 setup 接口）
  getNetworkInterfaces() {
    return request.get('/admin/network-interfaces')
  }
}

// ========== 监测 API ==========
export const MonitorApi = {
  getStorage() {
    return request.get('/monitor/storage')
  },

  getAccess() {
    return request.get('/monitor/access')
  },

  getKeywords(limit) {
    return request.get('/monitor/keywords', { params: { limit } })
  },

  getRecentKeywords() {
    return request.get('/monitor/recent')
  },

  getRankings(limit) {
    return request.get('/monitor/rankings', { params: { limit } })
  },

  getHotDownloads(page = 1, pageSize = 20) {
    return request.get('/monitor/hot-downloads', { params: { page, page_size: pageSize } })
  }
}

// ========== 行为分析 API ==========
export const BehaviorAnalyticsApi = {
  getSummary(params) {
    return request.get('/admin/behavior-analytics/summary', { params: snakeParams(params) })
  },
  getTrend(params) {
    return request.get('/admin/behavior-analytics/trend', { params: snakeParams(params) })
  },
  getTopFiles(params) {
    return request.get('/admin/behavior-analytics/top-files', { params: snakeParams(params) })
  },
  getSources(params) {
    return request.get('/admin/behavior-analytics/sources', { params: snakeParams(params) })
  },
  getFailures(params) {
    return request.get('/admin/behavior-analytics/failures', { params: snakeParams(params) })
  },
  getFileDetail(params) {
    return request.get('/admin/behavior-analytics/file', { params: snakeParams(params) })
  },
  getHeatmap(params) {
    return request.get('/admin/behavior-analytics/heatmap', { params: snakeParams(params) })
  },
  getAggregate(params) {
    return request.get('/admin/behavior-analytics/aggregate', { params: snakeParams(params) })
  },
  getLifecycle(params) {
    return request.get('/admin/behavior-analytics/lifecycle', { params: snakeParams(params) })
  }
}

// ========== 通知 API ==========
export const NotificationApi = {
  getNotifications() {
    return request.get('/notifications')
  },

  markRead(ids) {
    return request.post('/notifications/read', { ids })
  },

  merge() {
    return request.post('/notifications/merge')
  }
}

// ========== API Key 管理 API ==========
export const ApiKeyApi = {
  list(page = 1, pageSize = 20) {
    return request.get('/admin/api-keys', { params: { page, page_size: pageSize } })
  },

  get(id) {
    return request.get(`/admin/api-keys/${id}`)
  },

  create(data) {
    return request.post('/admin/api-keys', data, { skipSuccessToast: true })
  },

  updateStatus(id, status) {
    return request.put(`/admin/api-keys/${id}/status`, { status }, { skipSuccessToast: true })
  },

  delete(id) {
    return request.delete(`/admin/api-keys/${id}`, { skipSuccessToast: true })
  },

  // 可授予的 OpenAPI 接口权限目录（按 group 分组渲染）
  listScopes() {
    return request.get('/admin/open-api/scopes')
  }
}

// ========== 个人中心 API ==========
export const PersonalApi = {
  // 个人登录记录；filters 支持 method/status/keyword/from/to
  getLoginLogs(page = 1, pageSize = 20, filters = {}) {
    return request.get('/user/login-logs', { params: { page, page_size: pageSize, ...snakeParams(filters) } })
  },

  // 个人操作记录；filters 支持 action/source_type/status/keyword/from/to
  getRecords(page = 1, pageSize = 20, filters = {}) {
    return request.get('/user/records', { params: { page, page_size: pageSize, ...snakeParams(filters) } })
  },

  // 个人上传文件统计（公共文件）
  getStats() {
    return request.get('/user/stats')
  },

  // 绑定 LDAP 身份：提交目录密码完成绑定，绑定后该账号仅通过 LDAP 登录（本地密码被清除）。
  // 目录密码错误时后端返回 401，不能当作登录态失效处理，故跳过 401 的本地登录态清理；
  // 错误由设置页在卡片内展示，跳过全局提示避免重复
  bindLdapIdentity(password) {
    return request.post('/user/auth-bindings/ldap', { password }, { skipSuccessToast: true, skipAuthSideEffects: true, skipGlobalError: true })
  }
}

// ========== 安全事件 API ==========
export const SecurityApi = {
  // 分页查询安全事件（支持 severity/event_type/client_ip/identity/result/guard/unread/from/to/query 过滤）
  list(params = {}) {
    return request.get('/admin/security/events', { params: snakeParams(params) })
  },

  // 未读汇总（管理端入口红点与登录强提醒的数据源）
  unreadSummary() {
    return request.get('/admin/security/events/unread-summary')
  },

  // 标记已读：传入 { ids } 或 { all: true }
  markRead(ids) {
    return request.post('/admin/security/events/read', { ids }, { skipSuccessToast: true })
  },

  // 全部标记已读
  markAllRead() {
    return request.post('/admin/security/events/read', { all: true }, { skipSuccessToast: true })
  },

  // 基础统计（严重级别分布 / 近7日趋势 / TOP IP / TOP 类型 / 近期锁定）
  stats() {
    return request.get('/admin/security/events/stats')
  }
}

// ========== 存储告警消息 API（仅管理员） ==========
export const AlertApi = {
  listMessages(params = {}) {
    return request.get('/admin/alert-messages', { params: snakeParams(params) })
  },
  unreadCount() {
    return request.get('/admin/alert-messages/unread-count')
  },
  markRead(ids) {
    return request.post('/admin/alert-messages/read', { ids }, { skipSuccessToast: true })
  },
  markAllRead() {
    return request.post('/admin/alert-messages/read', { all: true }, { skipSuccessToast: true })
  }
}

export default {
  AuthApi,
  ConfigApi,
  FileApi,
  UploadApi,
  AdminApi,
  PrivateApi,
  TempApi,
  SetupApi,
  SystemApi,
  MonitorApi,
  BehaviorAnalyticsApi,
  NotificationApi,
  ApiKeyApi,
  SecurityApi,
  AlertApi,
  PersonalApi
}
