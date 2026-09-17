import { createApp } from 'vue'
import App from './App.vue'
import router, { setSetupRedirect } from './router'
import { loadPreviewConfig } from '@/config/preview'
import store from '@/store'
import { SetupApi, SystemApi, AuthApi, NotificationApi } from '@/api'

// ElMessage / ElMessageBox 为 JS 函数式调用，不会被 unplugin-vue-components 的
// ElementPlusResolver 自动注入样式，需在此按需导入，否则弹层无样式
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'

// Element Plus 官方深色主题 CSS 变量（html.dark 下生效）
import 'element-plus/theme-chalk/dark/css-vars.css'

const app = createApp(App)
app.use(router)

// 预加载公开配置和检查初始化状态
app.mount('#app')

// 导出初始化状态供其他组件使用，避免重复请求
export let isAppInitialized = false
export let initializationComplete = false

// 初始化 AnonymousID（首次访问页面时生成）
initAnonymousId()

// 通知轮询定时器（必须在 startNotificationPolling 调用前声明，避免 TDZ 错误）
let notificationPollTimer = null
let notificationPollInitTimer = null

// 加载公开配置到 store，并检查初始化状态
loadConfigAndCheckSetup()
// 启动时验证 token 服务器端有效性
validateTokenOnServer()
// 启动通知轮询
startNotificationPolling()

async function loadConfigAndCheckSetup() {
  try {
    // 并行请求配置和初始化状态
    const [optionsRes, statusRes] = await Promise.all([
      SystemApi.getOptions(),
      SetupApi.getStatus()
    ])

    // 处理配置
    if (optionsRes.data) {
      const d = optionsRes.data
      // 更新 store 中的配置
      store.setConfig({
        appName: d.app?.name || '',
        version: d.app?.version || '1.0.0',
        privateStorageEnabled: d.privateStorage?.enabled ?? false,
        tempFilesEnabled: d.tempFiles?.enabled ?? true,
        ftpEnabled: d.ftp?.enabled ?? false,
        ftpPort: d.ftp?.port ?? 2121,
        webdavEnabled: d.webdav?.enabled ?? false,
        openApiEnabled: d.openApi?.enabled ?? false,
        anonymousUsername: d.account?.anonymous?.username || 'public'
      })
      // 更新页面标题
      if (d.app?.name) {
        document.title = d.app.name
      }
      // 加载预览配置（复用已获取的数据）
      loadPreviewConfig(optionsRes.data)
    }

    // 检查初始化状态
    if (statusRes.data && !statusRes.data.initialized) {
      setSetupRedirect(true)
    } else {
      isAppInitialized = true
    }
  } catch (e) {
    console.error('加载配置失败', e)
  } finally {
    initializationComplete = true
  }
}

// 初始化匿名身份标识
function initAnonymousId() {
  if (!localStorage.getItem('anonymous_id')) {
    const id = crypto.randomUUID ? crypto.randomUUID() : 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
      const r = (Math.random() * 16) | 0
      return (c === 'x' ? r : (r & 0x3) | 0x8).toString(16)
    })
    localStorage.setItem('anonymous_id', id)
  }
}

function startNotificationPolling() {
  // 首次轮询在 3 秒后
  notificationPollInitTimer = setTimeout(() => {
    pollNotifications()
    // 之后每 10 秒轮询
    notificationPollTimer = setInterval(pollNotifications, 10000)
  }, 3000)

  // 页面卸载时清理所有定时器
  window.addEventListener('beforeunload', cleanupNotificationPolling)
}

function cleanupNotificationPolling() {
  if (notificationPollInitTimer) {
    clearTimeout(notificationPollInitTimer)
    notificationPollInitTimer = null
  }
  if (notificationPollTimer) {
    clearInterval(notificationPollTimer)
    notificationPollTimer = null
  }
}

async function pollNotifications() {
  // 没有活跃下载任务时不轮询
  if (!store.state.activeUrlTasks) return

  try {
    const res = await NotificationApi.getNotifications()
    if (res.success && res.data && res.data.length > 0) {
      store.setNotifications(res.data)
      // 检查是否还有活跃任务（pending/downloading），没有则停止轮询
      const hasActive = res.data.some(item =>
        item.status === 'pending' || item.status === 'downloading'
      )
      if (!hasActive) {
        store.setActiveUrlTasks(false)
      }
    } else {
      // 没有待通知的任务，停止轮询
      store.setActiveUrlTasks(false)
    }
  } catch (e) {
    // 轮询失败静默处理，不打扰用户
  }
}

// 启动时验证服务器端 token 有效性，无效则静默清除
async function validateTokenOnServer() {
  const token = localStorage.getItem('token')
  if (!token) return

  try {
    const data = await AuthApi.getUserInfo()
    if (!data.success || !data.data) {
      // Token 无效或已过期，静默清除
      localStorage.removeItem('token')
      localStorage.removeItem('uuid')
      localStorage.removeItem('username')
      localStorage.removeItem('userRole')
      store.setAuthenticated(false)
    }
    // 若服务端刷新了 token，响应拦截器已自动保存新 token
  } catch (e) {
    // 网络错误时保持当前状态，不打扰用户
    console.error('验证token失败:', e)
  }
}
