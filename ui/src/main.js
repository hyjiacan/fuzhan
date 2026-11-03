import { createApp } from 'vue'
import App from './App.vue'
import router, { setSetupRedirect } from './router'
import { loadPreviewConfig } from '@/config/preview'
import store from '@/store'
import { SetupApi, SystemApi, AuthApi, NotificationApi } from '@/api'
import { AppError, ErrorCategory } from '@/utils/error'

// ElMessage / ElMessageBox 为 JS 函数式调用，不会被 unplugin-vue-components 的
// ElementPlusResolver 自动注入样式，需在此按需导入，否则弹层无样式
import 'element-plus/es/components/message/style/css'
import 'element-plus/es/components/message-box/style/css'

// Element Plus 官方深色主题 CSS 变量（html.dark 下生效）
import 'element-plus/theme-chalk/dark/css-vars.css'

const app = createApp(App)

// 导出初始化状态供其他组件使用，避免重复请求
export let isAppInitialized = false
export let initializationComplete = false
// 导出错误态，供 App.vue 渲染启动失败占位页
export let isStartupError = false

// 通知轮询定时器（模块级声明，供 startNotificationPolling / cleanupNotificationPolling 使用）
let notificationPollInitTimer = null
let notificationPollTimer = null

// 初始化 AnonymousID（首次访问页面时生成）
initAnonymousId()

// 先加载公开配置与初始化状态（在 mount 之前完成，确保首屏路由守卫能读到 requireLogin 等配置），
// 因此「强制登录」模式下可正确将未登录用户重定向到登录页。
// 登录态探测（HttpOnly Cookie）与配置并行，保证 mount 前恢复两端统一登录态。
loadConfigAndCheckSetup()

async function loadConfigAndCheckSetup() {
  try {
    // 挂载前并行请求配置、初始化状态与登录态探测
    const [optionsRes, statusRes] = await Promise.all([
      SystemApi.getOptions(),
      SetupApi.getStatus(),
      validateTokenOnServer()
    ])

    // 处理配置
    if (optionsRes.data) {
      const d = optionsRes.data
      // 更新 store 中的配置（仅写入后端返回的真实值，不引入前端兜底值）
      store.setConfig({
        appName: d.app?.name || '',
        version: d.app?.version || '',
        privateStorageEnabled: d.privateStorage?.enabled ?? false,
        tempFilesEnabled: d.tempFiles?.enabled ?? false,
        ftpEnabled: d.ftp?.enabled ?? false,
        ftpPort: d.ftp?.port ?? null,
        webdavEnabled: d.webdav?.enabled ?? false,
        openApiEnabled: d.openApi?.enabled ?? false,
        urlUploadEnabled: d.upload?.urlUpload?.enabled ?? false,
        allowRegistration: d.account?.allowRegistration ?? false,
        requireLogin: d.account?.requireLogin ?? false,
        anonymousUsername: d.webdav?.publicUsername || d.account?.anonymous?.username || '',
        captchaMode: d.account?.captcha?.mode ?? 'disabled',
        captchaTriggerAfter: d.account?.captcha?.triggerAfter ?? 3,
        // 服务端按当前 IP 判定的是否需要验证码，用作登录页初始值；验证码错误时再动态置位
        captchaRequired: d.account?.captcha?.required ?? false
      })
      store.setConfigLoaded(true)
      // 更新页面标题
      if (d.app?.name) {
        document.title = d.app.name
      }
      // 加载预览配置（复用已获取的数据）
      loadPreviewConfig(optionsRes.data)
    } else {
      // 后端未返回配置数据，视为配置加载失败
      throw new Error('配置数据为空')
    }

    // 检查初始化状态：只有后端明确返回「未初始化」（initialized === false）才跳转初始化页，
    // 其他情况（已初始化 / 字段缺失 / 请求异常）一律视为已初始化，避免误跳转
    if (statusRes.data && statusRes.data.initialized === false) {
      setSetupRedirect(true)
    } else {
      isAppInitialized = true
      // 配置加载成功后才启动通知轮询（后端不可用时轮询无意义）
      startNotificationPolling()
    }
  } catch (e) {
    // 配置/初始化请求失败：后端不可用，整体进入异常态，不再用默认配置挂载正常应用
    console.error('加载配置或初始化状态失败:', e)
    isStartupError = true
    store.setStartupError(true)
  } finally {
    // 初始化检查完成标记（HomeView 等据此避免重复请求 setup/status）
    initializationComplete = true
    // 配置加载完成后才安装并挂载应用：`app.use(router)` 会触发首次路由导航，
    // 此时 store 中已写入真实的 requireLogin 等配置，登录门禁守卫才能正确将
    // 未登录用户（含访问站点根路径 /）重定向到登录页。
    app.use(router)
    app.mount('#app')
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
  // 没有活跃上传任务时不轮询
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

// 启动时验证服务器端登录态（HttpOnly Cookie 承载），并恢复前端登录状态。
// 与 lite 共享同一 Cookie 会话：lite 端登录后打开 Vue 页也能感知登录。
async function validateTokenOnServer() {
  try {
    const data = await AuthApi.getUserInfo()
    if (data.success && data.data) {
      // 后端以 Cookie 放行 → 恢复前端登录态（lite 端 Cookie 登录后打开 Vue 页的场景）
      const userData = data.data
      AuthApi.restoreSession({ uuid: userData.uuid, username: userData.username })
      store.setAuthenticated(true)
      if (userData.role) {
        localStorage.setItem('userRole', userData.role)
      }
      window.dispatchEvent(new CustomEvent('auth:changed'))
    } else {
      clearStaleAuth()
    }
    // 若服务端刷新了 token，response_wrapper 中间件会同步更新 Cookie，
    // 无需前端额外处理 local token
  } catch (e) {
    // 认证错误（401/403，无 Cookie 或失效）：清除本地残留的登录态。
    // 拦截器拒绝的是 AppError（无 response 字段），按 category === 'auth' 判断。
    if (e instanceof AppError && e.category === ErrorCategory.AUTH) {
      clearStaleAuth()
      return
    }
    // 网络错误（无响应）：后端不可用，保持当前状态，不打扰用户
    if (e && e.response) {
      // 后端已响应但既非认证错误也非 2xx：记录但不打扰用户
      console.error('验证登录态失败:', e)
    } else {
      console.error('验证登录态失败（网络错误）:', e)
    }
  }
}

// 清除本地失效身份信息，并通知界面刷新登录态（不弹提示）
function clearStaleAuth() {
  AuthApi.clearAuth()
  localStorage.removeItem('userRole')
  store.setAuthenticated(false)
  window.dispatchEvent(new CustomEvent('auth:changed'))
}
