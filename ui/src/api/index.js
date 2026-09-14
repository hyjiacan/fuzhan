import axios from 'axios'
import { ElMessage } from 'element-plus'
import { createErrorFromResponse, createNetworkError } from '../utils/error.js'
import { beginRequest, endRequest } from '../utils/requestLoading.js'

const API_BASE = '/api/v1'

// 创建 axios 实例
const request = axios.create({
  baseURL: API_BASE,
  timeout: 30000
})

// 写入类请求的 success 反馈（去重，避免与组件内已有的同文案提示重复）
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

// 请求拦截器：添加认证信息和匿名标识
request.interceptors.request.use(
  config => {
    beginRequest()
    const token = localStorage.getItem('token')
    if (token) {
      config.headers.Authorization = `Bearer ${token}`
    }
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
    // 检查响应头中是否有刷新后的 token
    const refreshedToken = response.headers['x-refreshed-token']
    if (refreshedToken) {
      localStorage.setItem('token', refreshedToken)
    }
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
        // Token 过期或无效，清除认证信息
        localStorage.removeItem('token')
        localStorage.removeItem('uuid')
        localStorage.removeItem('username')
        localStorage.removeItem('userRole')
      } else if (error.response.status === 403) {
        // 无权限，跳转到文件页面
        window.location.hash = '#/files'
      }
      appError = createErrorFromResponse(error.response)
    } else {
      // 网络错误（无响应）
      appError = createNetworkError(error)
    }

    // 调用全局错误处理器
    if (globalErrorHandler) {
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

  setAuth(token, uuid, username) {
    localStorage.setItem('token', token)
    localStorage.setItem('uuid', uuid)
    localStorage.setItem('username', username)
  },

  clearAuth() {
    localStorage.removeItem('token')
    localStorage.removeItem('uuid')
    localStorage.removeItem('username')
    localStorage.removeItem('userRole')
  },

  getUserInfo() {
    return request.get('/auth/user')
  },

  register(username, password) {
    return request.post('/auth/register', { username, password })
  },

  login(username, password) {
    return request.post('/auth/login', { username, password })
  },

  changePassword(oldPassword, newPassword) {
    return request.put('/auth/password', { oldPassword, newPassword })
  }
}

// ========== 配置 API ==========
export const ConfigApi = {
  get() {
    return request.get('/config')
  },

  save(config) {
    return request.post('/config', config)
  }
}

// ========== 服务器资源监控 API ==========
export const ResourceApi = {
  snapshot() {
    return request.get('/admin/resource/snapshot')
  },

  history(scope, range) {
    return request.get('/admin/resource/history', { params: { scope, range } })
  }
}

// ========== 文件 API ==========
export const FileApi = {
  list(path = '') {
    return request.get('/files/list', { params: { path } })
  },

  recent(page = 1, pageSize = 20, action = 'upload') {
    return request.get('/files/recent', { params: { page, pageSize, action } })
  },

  recentCarousel() {
    return request.get('/files/recent/carousel')
  },

  getInfo(path) {
    return request.get('/get_file_info', { params: { path } })
  },

  preview(path) {
    const encodedPath = path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
    return request.get(`/files/preview/${encodedPath}`)
  },

  previewChunk(path, chunkIndex) {
    const encodedPath = path.split('/').filter(Boolean).map(p => encodeURIComponent(p)).join('/')
    return request.get(`/files/preview-chunk/${encodedPath}`, { params: { chunk: chunkIndex } })
  }
}

// ========== 上传 API ==========
export const UploadApi = {
  getUrlInfo(url) {
    return request.post('/uploads/url_info', { url })
  },

  uploadFromUrl(url, filename, dir = '', storageType = '') {
    const data = { url, filename, dir }
    if (storageType) {
      data.storageType = storageType
    }
    return request.post('/uploads/url', data)
  },

  uploadFromUrlLocal(url, filename, dir, rootName) {
    return request.post('/uploads/url', { url, filename, dir, rootName })
  },

  // 分片上传
  session: {
    create(data) {
      return request.post('/uploads/session', data)
    },
    get(id) {
      return request.get(`/uploads/session/${id}`)
    },
    resume(id) {
      return request.post(`/uploads/session/${id}/resume`)
    },
    cancel(id) {
      return request.delete(`/uploads/session/${id}`)
    }
  },

  chunk: {
    upload(formData) {
      return request.post('/uploads/chunk', formData, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })
    }
  },

  finalize(uploadId) {
    return request.post('/uploads/finalize', { uploadId })
  },

  // URL 下载任务进度查询
  getURLTask(taskId) {
    return request.get(`/uploads/url-task/${taskId}`)
  },

  // 当前用户上传状态/会话
  listSessions(type, page = 1, pageSize = 50) {
    return request.get('/uploads/sessions', { params: { type, page, pageSize } })
  },

  listURLTasks(type, page = 1, pageSize = 50) {
    return request.get('/uploads/url-tasks', { params: { type, page, pageSize } })
  },

  cancelURLTask(taskId) {
    return request.post(`/uploads/url-tasks/${taskId}/cancel`)
  },

  retryURLTask(taskId) {
    return request.post(`/uploads/url-tasks/${taskId}/retry`)
  },

  deleteURLTask(taskId) {
    return request.delete(`/uploads/url-tasks/${taskId}`)
  },
}

// ========== 文件索引 API ==========
export const IndexApi = {
  // 查询索引记录
  listRecords(params = {}) {
    return request.get('/admin/index/records', { params })
  },

  // 获取索引统计
  getStats() {
    return request.get('/admin/index/stats')
  },

  // 触发全量扫描
  triggerScan() {
    return request.post('/admin/index/scan')
  },

  // 触发全量扫描（所有 scope）
  triggerFullScan() {
    return request.post('/admin/index/scan/trigger')
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
    return request.put(`/admin/index/records/${id}/notes`, { notes })
  },
  // 搜索文件记录（用于 autocomplete）
  searchRecords(query) {
    return request.get('/files/search-records', { params: { q: query } })
  },

  // 删除索引记录
  deleteRecord(id) {
    return request.delete(`/admin/index/records/${id}`)
  },

  // 查询重复文件
  listDuplicates(params = {}) {
    return request.get('/admin/index/duplicates', { params })
  },

  // 保留指定文件，自动删除同哈希的其他重复文件
  keepDuplicate(id) {
    return request.post(`/admin/index/duplicates/${id}/keep`)
  }
}

// ========== 文件依赖 API ==========
export const DependencyApi = {
  create(fileRecordId, dependsOnId, relation, description) {
    return request.post('/admin/index/dependencies', {
      fileRecordId, dependsOnId, relation, description
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
      fileRecordId, dependsOnId, relation, description
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
    return request.get('/search-suggest', { params: { q: query, limit } })
  },
  // 文件名检索：拼写纠错（输入错误时给推荐）
  spellcheck(query, limit = 5) {
    return request.get('/search-spellcheck', { params: { q: query, limit } })
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
    return request.put(`/files/records/${recordId}/notes`, { notes })
  },
  // 搜索文件记录（用于 autocomplete）
  searchFiles(query) {
    return request.get('/files/search-records', { params: { q: query } })
  },
  // 根据路径查找文件记录
  findRecord(fileName, rootName, filePath) {
    return request.get('/files/record', { params: { fileName, rootName, filePath } })
  }
}

// ========== 管理员 API ==========
export const AdminApi = {
  getUsers() {
    return request.get('/admin/users')
  },

  resetPassword(uuid, newPassword) {
    return request.put(`/admin/users/${uuid}/reset-password`, { newPassword })
  },

  setUserDisabled(uuid, disabled) {
    return request.put(`/admin/users/${uuid}/disabled`, { disabled })
  },

  deleteUser(uuid) {
    return request.delete(`/admin/users/${uuid}`)
  },

  getSessions() {
    return request.get('/admin/sessions')
  },

  cleanupSessions(sessionIds) {
    return request.post('/admin/sessions/cleanup', { sessionIds })
  },

  // 当前在线 IP（与登录无关，依据最近请求判定）
  getOnlineIps() {
    return request.get('/admin/online-ips')
  },

  // 文件管理
  listFiles(path = '') {
    return request.get('/admin/files/list', { params: { path } })
  },

  move(oldPath, newPath) {
    return request.post('/admin/files/move', { oldPath, newPath })
  },

  delete(path) {
    return request.delete('/admin/files', { params: { path } })
  },

  // URL 下载任务管理
  getURLTasks(page = 1, pageSize = 20, status = '') {
    const params = { page, pageSize }
    if (status) params.status = status
    return request.get('/admin/url-tasks', { params })
  },

  retryURLTask(id) {
    return request.post(`/admin/url-tasks/${id}/retry`)
  },

  deleteURLTask(id) {
    return request.delete(`/admin/url-tasks/${id}`)
  },

  // 任务管理
  getTasks() {
    return request.get('/admin/tasks')
  },

  getTaskHistory(type = '', page = 1, pageSize = 20) {
    const params = { page, pageSize }
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
    return request.post('/admin/records/clear', { action })
  },

  // 删除单条操作记录
  deleteRecord(id, action) {
    return request.post('/admin/records/delete', { id, action })
  }
}

// ========== 私有存储 API ==========
export const PrivateApi = {
  list(dir = '') {
    return request.get('/private/files', { params: { path: dir || undefined } })
  },

  upload(file, { expireDate } = {}) {
    const formData = new FormData()
    formData.append('file', file)
    if (expireDate) {
      formData.append('expireDate', expireDate)
    }
    return request.post('/private/upload', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
  },

  delete(code) {
    return request.delete(`/private/files/${code}`)
  },

  getQuota() {
    return request.get('/private/quota')
  },

  // 分片上传
  session: {
    create(data) {
      return request.post('/private/uploads/session', data)
    },
    get(id) {
      return request.get(`/private/uploads/session/${id}`)
    },
    resume(id) {
      return request.post(`/private/uploads/session/${id}/resume`)
    },
    cancel(id) {
      return request.delete(`/private/uploads/session/${id}`)
    }
  },

  chunk: {
    upload(formData) {
      return request.post('/private/uploads/chunk', formData, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })
    }
  },

  finalize(uploadId) {
    return request.post('/private/uploads/finalize', { uploadId })
  },

  download(code) {
    return `${API_BASE}/private/files/${code}/download`
  }
}

// ========== 临时文件 API (基于IP，无需认证) ==========
export const TempApi = {
  list(dir = '') {
    return request.get('/temp/list', { params: { path: dir || undefined } })
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
    return request.delete(`/temp/${code}`)
  },

  // 分片上传
  session: {
    create(data) {
      return request.post('/temp/upload/session', data)
    },
    get(id) {
      return request.get(`/temp/upload/session/${id}`)
    },
    resume(id) {
      return request.post(`/temp/upload/session/${id}/resume`)
    },
    cancel(id) {
      return request.delete(`/temp/upload/session/${id}`)
    }
  },

  chunk: {
    upload(formData) {
      return request.post('/temp/upload/chunk', formData, {
        headers: { 'Content-Type': 'multipart/form-data' }
      })
    }
  },

  finalize(uploadId) {
    return request.post('/temp/upload/finalize', { uploadId })
  }
}

// ========== 设置 API ==========
export const SetupApi = {
  getStatus() {
    return request.get('/setup/status')
  },

  save(config) {
    return request.post('/setup/save', config)
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
  getOptions() {
    return request.get('/options')
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
    return request.post('/admin/upload-cert', formData, {
      headers: { 'Content-Type': 'multipart/form-data' }
    })
  },

  // 上传 TLS 密钥
  uploadKey(file) {
    const formData = new FormData()
    formData.append('file', file)
    return request.post('/admin/upload-key', formData, {
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
    return request.get('/monitor/hot-downloads', { params: { page, pageSize } })
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
    return request.get('/admin/api-keys', { params: { page, pageSize } })
  },

  get(id) {
    return request.get(`/admin/api-keys/${id}`)
  },

  create(data) {
    return request.post('/admin/api-keys', data)
  },

  updateStatus(id, status) {
    return request.put(`/admin/api-keys/${id}/status`, { status })
  },

  delete(id) {
    return request.delete(`/admin/api-keys/${id}`)
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
  NotificationApi,
  ApiKeyApi
}