import { reactive, readonly } from 'vue'
import { useToast } from '@/composables/useToast'
import { FileApi, AdminApi, SearchApi } from '@/api'
import { compareFileNames } from '@/utils'

// 检查是否有保存的登录状态
const savedToken = localStorage.getItem('token')
const savedUsername = localStorage.getItem('username')

// 解码 JWT payload（仅 base64 解码，不验证签名）
function decodeJwtPayload(token) {
  try {
    const parts = token.split('.')
    if (parts.length !== 3) return null
    const decoded = atob(parts[1].replace(/-/g, '+').replace(/_/g, '/'))
    return JSON.parse(decoded)
  } catch {
    return null
  }
}

// Toast 实例
const toast = useToast()

// 应用状态
const state = reactive({
  // 用户信息
  user: {
    authenticated: !!savedToken,
    name: savedUsername || '',
    avatar: '',
    token: savedToken || ''
  },

  // 系统配置
  config: {
    appName: '',
    version: '1.0.0',
    privateStorageEnabled: false,
    tempFilesEnabled: true,
    ftpEnabled: false,
    ftpPort: 2121,
    webdavEnabled: false,
    openApiEnabled: false,
    anonymousUsername: 'public'
  },

  // 当前浏览路径
  currentPath: '/',

  // 文件列表
  fileList: [],

  // 加载状态
  loading: false,

  // 面包屑导航
  breadcrumb: [],

  // 消息提示（保留兼容性，逐步迁移）
  message: {
    text: '',
    type: '' // info, success, warning, error
  },

  // 搜索状态
  searchState: {
    isSearching: false,
    isCompleted: false,
    query: '',
    searchTime: 0
  },

  // 通知
  notifications: [],

  // 是否有活跃的 URL 下载任务（控制通知轮询开关）
  activeUrlTasks: false
})

// 状态变更方法
const mutations = {
  // 设置用户信息
  setUser(user) {
    state.user = { ...state.user, ...user }
  },

  // 设置认证状态
  setAuthenticated(authenticated) {
    state.user.authenticated = authenticated
  },

  // 设置应用配置
  setConfig(config) {
    state.config = { ...state.config, ...config }
  },

  // 设置文件列表
  setLoading(loading) {
    state.loading = loading
  },

  setFileList(files) {
    if (!Array.isArray(files)) {
      files = []
    }
    // 排序：文件夹在前，文件在后，按名称排序（英文在中文前）
    state.fileList = files.sort((a, b) => {
      const aIsDir = a.type === 'dir' || a.type === 'directory'
      const bIsDir = b.type === 'dir' || b.type === 'directory'
      if (aIsDir && !bIsDir) return -1
      if (!aIsDir && bIsDir) return 1
      return compareFileNames(a.name, b.name)
    })
  },

  // 设置当前路径
  setCurrentPath(path) {
    // 标准化路径分隔符为正斜杠
    const normalizedPath = path.replace(/\\/g, '/')
    state.currentPath = normalizedPath
    // 更新页面标题
    const displayPath = normalizedPath.replace(/^\//, '')
    const dirName = displayPath ? displayPath.split('/').pop() : state.config.appName
    document.title = displayPath ? `${dirName} - ${state.config.appName}` : state.config.appName
  },

  // 添加文件到列表
  addFile(file) {
    state.fileList.push(file)
  },

  // 更新文件
  updateFile(oldName, newFile) {
    const index = state.fileList.findIndex(f => f.name === oldName)
    if (index !== -1) {
      state.fileList[index] = newFile
    }
  },

  // 删除文件
  removeFile(fileName) {
    const index = state.fileList.findIndex(f => f.name === fileName)
    if (index !== -1) {
      state.fileList.splice(index, 1)
    }
  },

  // 更新文件备注（同时可缓存 recordId 用于后续编辑）
  setFileNotes(filePath, notes, recordId = null) {
    const index = state.fileList.findIndex(f => f.path === filePath)
    if (index !== -1) {
      const update = { ...state.fileList[index], notes }
      if (recordId != null) {
        update.recordId = recordId
      }
      state.fileList[index] = update
    }
  },

  // 设置面包屑导航
  setBreadcrumb(breadcrumb) {
    state.breadcrumb = breadcrumb
  },

  // 设置消息提示（使用 Toast 显示）
  setMessage(text, type = 'info') {
    state.message.text = text
    state.message.type = type
    // 使用 Toast 显示
    toast.show({ message: text, type })
  },

  // 清除消息提示
  clearMessage() {
    state.message.text = ''
    state.message.type = ''
  },

  // 设置搜索状态
  setSearchState(searching, completed = false, query = '', searchTime = 0) {
    state.searchState.isSearching = searching
    state.searchState.isCompleted = completed
    state.searchState.query = query
    state.searchState.searchTime = searchTime
  },

  // 清除搜索状态
  clearSearchState() {
    state.searchState.isSearching = false
    state.searchState.isCompleted = false
    state.searchState.query = ''
    state.searchState.searchTime = 0
  },

  // 设置活跃任务状态
  setActiveUrlTasks(active) {
    state.activeUrlTasks = active
  },

  // 设置通知列表
  setNotifications(notifications) {
    state.notifications = notifications
  },

  // 移除单个通知
  removeNotification(id) {
    const index = state.notifications.findIndex(n => n.id === id)
    if (index !== -1) {
      state.notifications.splice(index, 1)
    }
  },

  // 清除通知
  clearNotifications() {
    state.notifications = []
  }
}

// 异步操作方法
const actions = {
  // 初始化应用
  async initialize() {
    try {
      // 设置默认面包屑
      mutations.setBreadcrumb([{ name: '文件', path: '/' }])
    } catch (error) {
      console.error('初始化应用失败:', error)
    }
  },

  // 验证登录状态（由 main.js 启动时请求服务器验证，此处仅做本地检查）
  async validateAuth() {
    const token = localStorage.getItem('token')
    if (!token) {
      mutations.setAuthenticated(false)
      return false
    }

    // 本地解析 JWT payload 检查 exp（快速路径，格式显然过期的不再请求后端）
    const payload = decodeJwtPayload(token)
    if (payload && payload.exp) {
      if (Date.now() >= payload.exp * 1000) {
        mutations.setAuthenticated(false)
        localStorage.removeItem('token')
        localStorage.removeItem('uuid')
        localStorage.removeItem('username')
        localStorage.removeItem('userRole')
        return false
      }
    }

    // main.js 启动时已验证服务器端 token 有效性
    return true
  },

  // 加载文件列表
  async loadFileList(path = '') {
    try {
      mutations.setLoading(true)
      // 转换路径格式：空或 "/" 转为空传给 API，API 会列出所有根目录
      const apiPath = path === '/' ? '' : path

      const result = await FileApi.list(apiPath)

      if (result.success && result.data) {
        mutations.setFileList(result.data)
        mutations.setCurrentPath(path)
        mutations.clearMessage()
      } else {
        toast.error(result.message || '加载文件列表失败')
      }

      // 更新面包屑
      if (path === '' || path === '/') {
        mutations.setBreadcrumb([{ name: '文件', path: '/' }])
      } else {
        // 构建面包屑：文件 > 一级 > 二级 > ... > 当前目录
        const parts = path.replace(/^\//, '').split('/')
        const breadcrumbItems = [{ name: '文件', path: '/' }]
        for (let i = 0; i < parts.length; i++) {
          const itemPath = '/' + parts.slice(0, i + 1).join('/')
          breadcrumbItems.push({ name: parts[i], path: itemPath })
        }
        mutations.setBreadcrumb(breadcrumbItems)
      }
    } catch (error) {
      toast.error('加载文件列表失败')
    } finally {
      mutations.setLoading(false)
    }
  },

  // 搜索文件
  async searchFiles(query) {
    if (!query.trim()) {
      toast.warning('请输入搜索关键词')
      return
    }

    const startTime = Date.now()

    mutations.setLoading(true)
    mutations.setFileList([])
    mutations.setSearchState(true, false, query)

    try {
      const result = await SearchApi.search(query)

      if (result.success && result.data) {
        mutations.setFileList(result.data)
        mutations.setSearchState(false, true, query, Date.now() - startTime)
      } else {
        toast.error(result.message || '搜索失败')
        mutations.setSearchState(false, false, query)
      }
    } catch (error) {
      toast.error('搜索失败')
      mutations.setSearchState(false, false, query)
    } finally {
      mutations.setLoading(false)
    }
  },

  // 管理页面搜索（不记录历史）
  async adminSearchFiles(query) {
    if (!query.trim()) {
      toast.warning('请输入搜索关键词')
      return
    }

    const startTime = Date.now()

    mutations.setLoading(true)
    mutations.setFileList([])
    mutations.setSearchState(true, false, query)

    try {
      const result = await AdminApi.search(query)

      if (result.success && result.data) {
        mutations.setFileList(result.data)
        mutations.setSearchState(false, true, query, Date.now() - startTime)
      } else {
        toast.error(result.message || '搜索失败')
        mutations.setSearchState(false, false, query)
      }
    } catch (error) {
      toast.error('搜索失败')
      mutations.setSearchState(false, false, query)
    } finally {
      mutations.setLoading(false)
    }
  }
}

// 创建actions对象
const storeActions = { ...actions }

// 导出只读状态和方法
export default {
  state: readonly(state),
  ...mutations,
  actions: storeActions
}