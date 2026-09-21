<template>
  <header class="app-header">
    <div class="header-content">
      <!-- Logo -->
      <router-link to="/" class="logo-link">
        <img src="/assets/icons/logo.svg" class="app-logo" alt="logo" />
        <span class="app-name">{{ appName }}</span>
      </router-link>

      <!-- Hamburger toggle (mobile) -->
      <button class="nav-toggle" @click="mobileMenuOpen = !mobileMenuOpen" :aria-label="mobileMenuOpen ? '关闭菜单' : '打开菜单'">
        <span class="hamburger-line" :class="{ open: mobileMenuOpen }"></span>
        <span class="hamburger-line" :class="{ open: mobileMenuOpen }"></span>
        <span class="hamburger-line" :class="{ open: mobileMenuOpen }"></span>
      </button>

      <!-- Navigation -->
      <nav class="nav-menu" :class="{ 'mobile-open': mobileMenuOpen }">
        <router-link
          v-for="item in visibleNavItems"
          :key="item.path"
          :to="item.path"
          class="nav-item"
          :class="{ active: isActive(item.path) }"
          @click="mobileMenuOpen = false"
        >
          {{ item.name }}
        </router-link>
        <!-- 移动端登录/用户入口 -->
        <div class="nav-mobile-auth">
          <ThemeToggle class="header-theme-toggle" />
          <template v-if="authState.isLoggedIn">
            <span class="username">{{ authState.username }}</span>
            <el-button link size="small" class="logout-btn" @click="handleLogout">退出</el-button>
          </template>
          <template v-else>
            <a class="login-link" href="javascript:void(0)" @click="showLoginModal = true">登录</a>
          </template>
        </div>
      </nav>

      <!-- Desktop: Right side Login/User -->
      <div class="header-actions">
        <ThemeToggle class="header-theme-toggle" />
        <template v-if="authState.isLoggedIn">
          <span class="username">{{ authState.username }}</span>
          <el-button link size="small" class="logout-btn" @click="handleLogout">退出</el-button>
        </template>
        <template v-else>
          <a class="login-link" href="javascript:void(0)" @click="showLoginModal = true">登录</a>
        </template>
      </div>
    </div>
  </header>

  <!-- 登录弹框 -->
  <el-dialog v-model="showLoginModal" class="auth-dialog" width="420px">
    <template #header>
      <div class="auth-dialog-header">
        <img src="/assets/icons/logo.svg" class="auth-dialog-logo" alt="浮栈" />
        <div class="auth-dialog-heading">
          <h3 class="auth-dialog-title">欢迎回来</h3>
          <p class="auth-dialog-subtitle">登录 {{ appName }}，继续你的文件旅程</p>
        </div>
      </div>
    </template>

    <el-form ref="loginFormRef" :model="loginForm" :rules="loginRules" label-position="top">
      <el-form-item label="用户名" prop="username">
        <el-input
          v-model="loginForm.username"
          :maxlength="64"
          size="large"
          placeholder="请输入用户名"
          type="text"
          autocomplete="username"
          @keydown.enter="handleLogin"
        >
          <template #prefix><el-icon><User /></el-icon></template>
        </el-input>
      </el-form-item>
      <el-form-item label="密码" prop="password">
        <el-input
          v-model="loginForm.password"
          :maxlength="128"
          size="large"
          type="password"
          show-password
          placeholder="请输入密码"
          autocomplete="current-password"
          @keydown.enter="handleLogin"
        >
          <template #prefix><el-icon><Lock /></el-icon></template>
        </el-input>
      </el-form-item>
    </el-form>

    <template #footer>
      <div class="auth-footer">
        <el-button type="primary" size="large" :loading="loggingIn" class="auth-submit" @click="handleLogin">
          登录
        </el-button>
        <div v-if="privateStorageEnabled" class="auth-switch">
          <span class="auth-switch-text">还没有账号？</span>
          <el-button link type="primary" class="switch-link" @click="switchToRegister">立即注册</el-button>
        </div>
      </div>
    </template>
  </el-dialog>

  <!-- 注册弹框 -->
  <el-dialog v-model="showRegisterModal" class="auth-dialog" width="420px">
    <template #header>
      <div class="auth-dialog-header">
        <img src="/assets/icons/logo.svg" class="auth-dialog-logo" alt="浮栈" />
        <div class="auth-dialog-heading">
          <h3 class="auth-dialog-title">创建账号</h3>
          <p class="auth-dialog-subtitle">加入 {{ appName }}，让文件有处可栖</p>
        </div>
      </div>
    </template>

    <el-form ref="registerFormRef" :model="registerForm" :rules="registerRules" label-position="top">
      <el-form-item label="用户名" prop="username">
        <el-input
          v-model="registerForm.username"
          :maxlength="64"
          size="large"
          placeholder="请输入用户名"
          autocomplete="username"
          @keydown.enter="handleRegister"
        >
          <template #prefix><el-icon><User /></el-icon></template>
        </el-input>
      </el-form-item>
      <el-form-item label="密码" prop="password">
        <el-input
          v-model="registerForm.password"
          :maxlength="128"
          size="large"
          type="password"
          show-password
          placeholder="请输入密码"
          autocomplete="new-password"
          @keydown.enter="handleRegister"
        >
          <template #prefix><el-icon><Lock /></el-icon></template>
        </el-input>
      </el-form-item>
      <el-form-item label="确认密码" prop="confirmPassword">
        <el-input
          v-model="registerForm.confirmPassword"
          :maxlength="128"
          size="large"
          type="password"
          show-password
          placeholder="请再次输入密码"
          autocomplete="new-password"
          @keydown.enter="handleRegister"
        >
          <template #prefix><el-icon><Lock /></el-icon></template>
        </el-input>
      </el-form-item>
    </el-form>

    <template #footer>
      <div class="auth-footer">
        <el-button type="primary" size="large" :loading="registering" class="auth-submit" @click="handleRegister">
          注册
        </el-button>
        <div class="auth-switch">
          <span class="auth-switch-text">已有账号？</span>
          <el-button link type="primary" class="switch-link" @click="switchToLogin">去登录</el-button>
        </div>
      </div>
    </template>
  </el-dialog>

  <!-- 安全事件强提醒（管理员登录后存在未读风险事件时弹出） -->
  <el-dialog
    v-model="securityAlertVisible"
    title="安全事件提醒"
    width="620px"
    append-to-body
    :close-on-click-modal="false"
    class="security-alert-dialog"
  >
    <div class="security-alert-body">
      <el-alert
        type="error"
        :closable="false"
        show-icon
        class="security-alert-summary"
      >
        <template #title>
          检测到 <b>{{ securitySummary.high }}</b> 条高危、共 <b>{{ securitySummary.total }}</b> 条未读风险事件，
          可能包含针对本系统的攻击 / 越权 / 异常请求，请尽快处理。
        </template>
      </el-alert>

      <template v-if="securityRecent.length > 0">
        <div class="security-alert-title">最近未读事件（高危优先）</div>
        <div class="security-alert-list">
          <div v-for="ev in securityRecent" :key="ev.id" class="security-alert-item">
            <el-tag :type="severityTagType(ev.severity)" size="small" effect="light">{{ severityLabel(ev.severity) }}</el-tag>
            <span class="item-type">{{ eventTypeLabel(ev.eventType) }}</span>
            <span class="item-ip mono">{{ ev.clientIP || '-' }}</span>
            <span class="item-target">{{ ev.target || ev.path || '-' }}</span>
            <span class="item-time">{{ formatTime(ev.createdAt) }}</span>
          </div>
        </div>
      </template>
    </div>
    <template #footer>
      <div class="security-alert-actions">
        <el-button @click="securityMarkAllRead">
          全部标记已读
        </el-button>
        <el-button type="primary" @click="goSecurityPage">
          前往查看
        </el-button>
      </div>
    </template>
  </el-dialog>

</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { ElMessage } from 'element-plus'
import { User, Lock } from '@element-plus/icons-vue'
import { AuthApi, NotificationApi, SecurityApi } from '@/api'
import { showLoginDialogEvent, showRegisterDialogEvent } from '@/router'
import store from '@/store'
import ThemeToggle from './ThemeToggle.vue'

const router = useRouter()
const route = useRoute()

// Config
const appName = computed(() => store.state.config.appName)

// 私有存储是否启用
const privateStorageEnabled = computed(() => store.state.config.privateStorageEnabled !== false)

// Auth state - reactive to localStorage changes
const authState = reactive({
  isLoggedIn: false,
  username: '',
  isAdmin: false
})

// Login modal
const showLoginModal = ref(false)
const mobileMenuOpen = ref(false)
const loggingIn = ref(false)
const loginFormRef = ref(null)
const loginForm = reactive({
  username: '',
  password: ''
})
const loginRules = {
  username: { required: true, message: '请输入用户名', trigger: 'blur' },
  password: { required: true, message: '请输入密码', trigger: 'blur' }
}

// Register modal
const showRegisterModal = ref(false)
const registering = ref(false)
const registerFormRef = ref(null)
const registerForm = reactive({
  username: '',
  password: '',
  confirmPassword: ''
})
const registerRules = {
  username: { required: true, message: '请输入用户名', trigger: 'blur' },
  password: { required: true, message: '请输入密码', trigger: 'blur' },
  confirmPassword: [
    { required: true, message: '请再次输入密码', trigger: 'blur' },
    {
      validator: (rule, value) => value === registerForm.password,
      message: '两次输入的密码不一致',
      trigger: 'blur'
    }
  ]
}

// Update auth state from localStorage
const updateAuthState = () => {
  authState.isLoggedIn = AuthApi.isLoggedIn()
  authState.username = localStorage.getItem('username') || '用户'
  authState.isAdmin = localStorage.getItem('userRole') === 'admin'
}

// 登录弹框事件处理
const handleShowLoginDialog = () => {
  updateAuthState()
  if (!authState.isLoggedIn) {
    showLoginModal.value = true
  }
}

// 注册弹框事件处理
const handleShowRegisterDialog = () => {
  updateAuthState()
  if (!authState.isLoggedIn && privateStorageEnabled.value) {
    showRegisterModal.value = true
  }
}

// 切换到注册弹框
const switchToRegister = () => {
  showLoginModal.value = false
  showRegisterModal.value = true
}

// 切换到登录弹框
const switchToLogin = () => {
  showRegisterModal.value = false
  showLoginModal.value = true
}

// 监听登录弹框事件
onMounted(() => {
  updateAuthState()
  showLoginDialogEvent.addEventListener('show', handleShowLoginDialog)
  showRegisterDialogEvent.addEventListener('show', handleShowRegisterDialog)
  // 启动时后端校验 token 失效被清除后，刷新本地登录态与菜单
  window.addEventListener('auth:changed', handleAuthChanged)
})

onUnmounted(() => {
  showLoginDialogEvent.removeEventListener('show', handleShowLoginDialog)
  showRegisterDialogEvent.removeEventListener('show', handleShowRegisterDialog)
  window.removeEventListener('auth:changed', handleAuthChanged)
})

// token 被清除（如启动校验失效）时刷新登录态，隐藏用户/管理菜单
const handleAuthChanged = () => {
  updateAuthState()
}

// Navigation items from router meta
const visibleNavItems = computed(() => {
  const items = []

  // 公共页面：始终显示
  const publicPages = [
    { path: '/files', name: '文件' },
    { path: '/recent', name: '最近' },
    { path: '/hot', name: '热门' },
    { path: '/temp', name: '临时' }
  ]
  for (const item of publicPages) {
    if (item.path === '/temp' && !store.state.config.tempFilesEnabled) continue
    items.push(item)
  }

  // 登录后额外显示角色入口
  if (authState.isLoggedIn) {
    if (authState.isAdmin) {
      items.push({ path: '/admin', name: '管理' })
    } else {
      items.push({ path: '/user', name: '个人' })
    }
  }

  return items
})

// Methods
const isActive = (path) => {
  if (path === '/files') {
    return route.path === '/files' || route.path.startsWith('/files/')
  }
  if (path === '/user') {
    return route.path.startsWith('/user')
  }
  if (path === '/admin') {
    return route.path.startsWith('/admin')
  }
  return route.path.startsWith(path)
}

// 安全事件强提醒状态
const securityAlertVisible = ref(false)
const securitySummary = reactive({ high: 0, total: 0 })
const securityRecent = ref([])
const severityMeta = { high: { label: '高危', type: 'danger' }, medium: { label: '中危', type: 'warning' }, low: { label: '低危', type: 'info' } }
const eventTypeMeta = {
  'auth.failed_login': '登录/注册失败',
  'auth.locked': '登录被锁定',
  'auth.unauthorized': '认证失败(401)',
  'access.forbidden': '越权访问(403)',
  'rate.limited': '触发限流(429)',
  'code.bruteforce': '口令/分享码暴破'
}
const severityLabel = (s) => severityMeta[s]?.label || s || '未知'
const severityTagType = (s) => severityMeta[s]?.type || 'info'
const eventTypeLabel = (t) => eventTypeMeta[t] || t || '-'
const formatTime = (iso) => {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString()
  } catch {
    return iso
  }
}

// 管理员登录后查询未读风险事件，存在则强提醒
const checkSecurityAlert = async () => {
  try {
    const res = await SecurityApi.unreadSummary()
    if (!res.success || !res.data) return
    securitySummary.high = res.data.high || 0
    securitySummary.total = res.data.total || 0
    securityRecent.value = res.data.recent || []
    if (securitySummary.total > 0) {
      securityAlertVisible.value = true
    }
  } catch (e) {
    // 提醒拉取失败不阻塞登录
  }
}

const securityMarkAllRead = async () => {
  try {
    await SecurityApi.markAllRead()
  } catch (e) {
    // 忽略
  }
  securitySummary.high = 0
  securitySummary.total = 0
  securityRecent.value = []
  securityAlertVisible.value = false
}

const goSecurityPage = () => {
  securityAlertVisible.value = false
  router.push('/admin/security')
}

const handleLogin = async () => {
  if (!loginFormRef.value) return

  loginFormRef.value.validate(async (valid) => {
    if (!valid) return

    loggingIn.value = true
    try {
      const data = await AuthApi.login(loginForm.username, loginForm.password)

      if (data.success) {
        AuthApi.setAuth(data.data.token, data.data.uuid, data.data.username)

        // 获取用户信息并保存 role
        try {
          const userData = await AuthApi.getUserInfo()
          if (userData.success && userData.data) {
            localStorage.setItem('userRole', userData.data.role || 'user')
          }
        } catch (e) {
          console.error('获取用户信息失败', e)
        }

        // 登录后合并匿名通知到当前用户
        try {
          NotificationApi.merge()
        } catch (e) {
          // 合并失败不影响登录流程
        }

        ElMessage.success('登录成功')
        showLoginModal.value = false
        loginForm.username = ''
        loginForm.password = ''
        updateAuthState()

        // 管理员登录：若存在未读风险事件，弹出强提醒
        if (localStorage.getItem('userRole') === 'admin') {
          checkSecurityAlert()
        }
      } else {
        ElMessage.error(data.message || '登录失败')
      }
    } catch (error) {
      ElMessage.error('登录失败：' + error.message)
    } finally {
      loggingIn.value = false
    }
  })
}

const handleRegister = async () => {
  if (!registerFormRef.value) return

  registerFormRef.value.validate(async (valid) => {
    if (!valid) return

    registering.value = true
    try {
      const data = await AuthApi.register(registerForm.username, registerForm.password)

      if (data.success) {
        ElMessage.success('注册成功，请登录')

        // 自动填充登录表单
        loginForm.username = registerForm.username
        loginForm.password = ''

        // 关闭注册弹框，打开登录弹框
        showRegisterModal.value = false
        registerForm.username = ''
        registerForm.password = ''
        registerForm.confirmPassword = ''
        showLoginModal.value = true
      } else {
        ElMessage.error(data.message || '注册失败')
      }
    } catch (error) {
      ElMessage.error('注册失败：' + error.message)
    } finally {
      registering.value = false
    }
  })
}

const handleLogout = () => {
  AuthApi.clearAuth()
  localStorage.removeItem('userRole')
  updateAuthState()
  router.push({ path: '/', query: {} })
}
</script>

<script>
import { computed } from 'vue'
export default {
  name: 'AppHeader'
}
</script>

<style lang="less">
@import '@/styles/variables.less';

.app-header {
  height: @header-height;
  background: @bg-color-dark;
  border-bottom: 1px solid @bg-color-dark-hover;
  position: sticky;
  top: 0;
  z-index: @zindex-sticky;
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.12);
}

.header-content {
  display: flex;
  align-items: center;
  height: 100%;
  padding: 0 20px;
  gap: 32px;
}

.logo-link {
  display: flex;
  align-items: center;
  gap: 8px;
  text-decoration: none;
  color: #fff;
  flex-shrink: 0;

  .app-logo {
      width: 32px;
      height: 32px;
      transition: transform 0.2s ease;

      &:hover {
        animation: logoFloat 0.6s ease-in-out infinite alternate;
      }
    }

    @keyframes logoFloat {
      from {
        transform: translateY(0);
      }
      to {
        transform: translateY(-6px);
      }
    }

  .app-name {
    font-size: @font-size-xl;
    font-weight: 600;
  }
}

/* 汉堡菜单按钮 */
.nav-toggle {
  display: none;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  width: 36px;
  height: 36px;
  background: transparent;
  border: 1px solid rgba(255, 255, 255, 0.3);
  border-radius: 4px;
  cursor: pointer;
  gap: 4px;
  padding: 6px;
  margin-left: auto;
  z-index: 1001;

  .hamburger-line {
    display: block;
    width: 20px;
    height: 2px;
    background: rgba(255, 255, 255, 0.8);
    border-radius: 2px;
    transition: all 0.3s ease;

    &.open:nth-child(1) {
      transform: rotate(45deg) translate(4px, 4px);
    }
    &.open:nth-child(2) {
      opacity: 0;
    }
    &.open:nth-child(3) {
      transform: rotate(-45deg) translate(4px, -4px);
    }
  }
}

.nav-menu {
  display: flex;
  align-items: center;
  gap: 4px;
  flex: 1;

  .nav-mobile-auth {
    display: none;
  }
}

@media @tablet {
  .header-content {
    gap: 16px;
    padding: 0 12px;
  }

  .nav-toggle {
    display: flex;
  }

  .header-actions {
    display: none !important;
  }

  .nav-menu {
    position: fixed;
    top: @header-height;
    left: 0;
    right: 0;
    bottom: 0;
    flex-direction: column;
    align-items: stretch;
    background: @bg-color-dark;
    padding: 8px 0;
    gap: 0;
    transform: translateX(-100%);
    transition: transform 0.3s ease;
    z-index: 1000;
    overflow-y: auto;

    &.mobile-open {
      transform: translateX(0);
    }

    .nav-item {
      padding: 14px 20px;
      border-bottom: 1px solid rgba(255, 255, 255, 0.05);
      border-radius: 0;
      transition: background-color 0.2s, color 0.2s;

      &:hover {
        transform: none;
      }

      &.active {
        background: @primary-color;
        box-shadow: none;
        border-left: 3px solid @primary-accent;
      }
    }

    .nav-mobile-auth {
      display: flex;
      align-items: center;
      gap: 12px;
      padding: 14px 20px;
      border-top: 1px solid rgba(255, 255, 255, 0.1);
      margin-top: auto;

      .username {
        color: rgba(255, 255, 255, 0.8);
        font-size: @font-size-base;
      }
    }
  }
}

@media @desktop {
  .nav-toggle {
    display: none;
  }
}

.nav-item {
  display: flex;
  align-items: center;
  padding: 6px 16px;
  color: rgba(255, 255, 255, 0.7);
  text-decoration: none;
  border-radius: 999px;
  transition: background-color 0.2s, color 0.2s, transform 0.2s, box-shadow 0.2s;
  font-size: @font-size-base;
  position: relative;

  &:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.12);
    transform: translateY(-1px);
  }

  &.active {
    color: #fff;
    background: @primary-color;
    box-shadow: 0 2px 10px fade(@primary-color, 40%);
    font-weight: 500;
  }
}

.nav-dropdown-trigger {
  cursor: pointer;
}

.username {
  color: rgba(255, 255, 255, 0.8);
  font-size: @font-size-base;
}

.logout-btn {
  color: rgba(255, 255, 255, 0.7);

  &:hover {
    color: #fff;
  }
}

.login-link {
  color: rgba(255, 255, 255, 0.7);
  font-size: @font-size-base;
  text-decoration: none;
  cursor: pointer;
  transition: color 0.2s;

  &:hover {
    color: #fff;
    text-decoration: underline;
  }
}

.upload-status-btn {
  color: rgba(255, 255, 255, 0.75);
  margin-right: 8px;
  border-radius: 6px;
  transition: color 0.2s, background-color 0.2s, box-shadow 0.2s;
  gap: 6px;

  &:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.2) !important;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.4);
  }
}

.auth-dialog {
  border-radius: @border-radius-lg;
  overflow: hidden;
  box-shadow: @shadow-lg;

  .el-dialog__header {
    padding: 28px 24px 0;
  }

  .el-dialog__body {
    padding: 22px 24px 6px;
  }

  .el-dialog__footer {
    padding: 12px 24px 24px;
  }

  .el-dialog__headerbtn {
    top: 20px;
    right: 20px;
  }

  .auth-dialog-header {
    display: flex;
    align-items: center;
    gap: 14px;
    padding-right: 28px;

    .auth-dialog-logo {
      width: 44px;
      height: 44px;
      border-radius: 12px;
      flex-shrink: 0;
      box-shadow: @shadow-sm;
    }

    .auth-dialog-heading {
      min-width: 0;

      .auth-dialog-title {
        margin: 0;
        font-size: 20px;
        line-height: 1.3;
        font-weight: 700;
        color: @text-color;
      }

      .auth-dialog-subtitle {
        margin: 3px 0 0;
        font-size: @font-size-sm;
        color: @text-color-placeholder;
        white-space: nowrap;
        overflow: hidden;
        text-overflow: ellipsis;
      }
    }
  }

  .el-form-item {
    margin-bottom: 18px;
  }
}

.auth-footer {
  display: flex;
  flex-direction: column;
  align-items: stretch;
  gap: 16px;

  .auth-submit {
    width: 100%;
    font-weight: 600;
    letter-spacing: 0.5px;
    box-shadow: @button-hover-shadow;
    transition: transform @transition-fast, box-shadow @transition-fast;

    &:hover:not(.is-loading):not(.is-disabled) {
      transform: @hover-lift;
      box-shadow: 0 8px 20px fade(@primary-accent, 40%);
    }

    &:active:not(.is-loading):not(.is-disabled) {
      transform: @active-scale;
    }
  }

  .auth-switch {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 2px;

    .auth-switch-text {
      font-size: @font-size-sm;
      color: @text-color-secondary;
    }

    .switch-link {
      font-weight: 600;
      font-size: @font-size-sm;
    }
  }
}

// 安全事件强提醒弹框
.security-alert-dialog {
  .security-alert-body {
    padding-top: 4px;

    .security-alert-summary {
      margin-bottom: 12px;
      border-radius: 6px;
    }

    .security-alert-title {
      font-size: 13px;
      font-weight: 600;
      margin-bottom: 8px;
      color: @text-color;
    }

    .security-alert-list {
      max-height: 320px;
      overflow: auto;
      border: 1px solid @border-color-light;
      border-radius: 6px;
      padding: 4px 0;
    }

    .security-alert-item {
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 7px 12px;
      font-size: 13px;

      & + .security-alert-item {
        border-top: 1px solid @border-color-light;
      }

      .item-type {
        flex: 0 0 auto;
        color: @text-color;
      }

      .item-ip {
        flex: 0 0 auto;
        color: @text-color-secondary;
      }

      .item-target {
        flex: 1 1 auto;
        overflow: hidden;
        text-overflow: ellipsis;
        white-space: nowrap;
        color: @text-color;
      }

      .item-time {
        flex: 0 0 auto;
        color: @text-color-placeholder;
        font-size: 12px;
      }
    }
  }

  .security-alert-actions {
    display: flex;
    justify-content: flex-end;
    gap: 10px;
  }
}

.mono {
  font-family: monospace;
}
</style>
