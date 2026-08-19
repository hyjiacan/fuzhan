<template>
  <n-layout-header class="app-header">
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
          <template v-if="authState.isLoggedIn">
            <span class="username">{{ authState.username }}</span>
            <n-button quaternary size="small" class="logout-btn" @click="handleLogout">退出</n-button>
          </template>
          <template v-else>
            <n-button type="primary" size="small" @click="showLoginModal = true">登录</n-button>
          </template>
        </div>
      </nav>

      <!-- Desktop: Right side Login/User -->
      <div class="header-actions">
        <n-button quaternary size="small" class="upload-manager-btn" @click="showUploadManager = true">
          <template #icon>
            <n-icon><UploadIcon /></n-icon>
          </template>
          上传管理
        </n-button>
        <template v-if="authState.isLoggedIn">
          <span class="username">{{ authState.username }}</span>
          <n-button quaternary size="small" class="logout-btn" @click="handleLogout">退出</n-button>
        </template>
        <template v-else>
          <n-button type="primary" size="small" @click="showLoginModal = true">登录</n-button>
        </template>
      </div>
    </div>
  </n-layout-header>

  <!-- 登录弹框 -->
  <n-modal v-model:show="showLoginModal" preset="card" title="登录" style="width: 400px">
    <n-form ref="loginFormRef" :model="loginForm" :rules="loginRules">
      <n-form-item path="username" label="用户名">
        <n-input
          v-model:value="loginForm.username"
          :maxlength="64"
          placeholder="请输入用户名"
          @keydown.enter="handleLogin"
        />
      </n-form-item>
      <n-form-item path="password" label="密码">
        <n-input
          v-model:value="loginForm.password"
          :maxlength="128"
          type="password"
          placeholder="请输入密码"
          autocomplete="current-password"
          @keydown.enter="handleLogin"
        />
      </n-form-item>
    </n-form>
    <template #footer>
      <div class="auth-footer">
        <n-button type="primary" block :loading="loggingIn" @click="handleLogin">
          登录
        </n-button>
        <n-button text v-if="privateStorageEnabled" @click="switchToRegister" class="switch-link">
          还没有账号？去注册
        </n-button>
      </div>
    </template>
  </n-modal>

  <!-- 注册弹框 -->
  <n-modal v-model:show="showRegisterModal" preset="card" title="注册" style="width: 400px">
    <n-form ref="registerFormRef" :model="registerForm" :rules="registerRules">
      <n-form-item path="username" label="用户名">
        <n-input
          v-model:value="registerForm.username"
          :maxlength="64"
          placeholder="请输入用户名"
          autocomplete="username"
          @keydown.enter="handleRegister"
        />
      </n-form-item>
      <n-form-item path="password" label="密码">
        <n-input
          v-model:value="registerForm.password"
          :maxlength="128"
          type="password"
          placeholder="请输入密码"
          autocomplete="new-password"
          @keydown.enter="handleRegister"
        />
      </n-form-item>
      <n-form-item path="confirmPassword" label="确认密码">
        <n-input
          v-model:value="registerForm.confirmPassword"
          :maxlength="128"
          type="password"
          placeholder="请再次输入密码"
          autocomplete="new-password"
          @keydown.enter="handleRegister"
        />
      </n-form-item>
	    </n-form>
	    <template #footer>
	      <div class="auth-footer">
	        <n-button type="primary" block :loading="registering" @click="handleRegister">
	          注册
	        </n-button>
	        <n-button text @click="switchToLogin" class="switch-link">
	          已有账号？去登录
	        </n-button>
	      </div>
	    </template>
	  </n-modal>

  <!-- 上传管理弹框 -->
  <UploadManagerDialog v-model:show="showUploadManager" />
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted, h } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { NLayoutHeader, NButton, NModal, NForm, NFormItem, NInput, NIcon, useMessage } from 'naive-ui'
import { AuthApi, NotificationApi } from '@/api'
import { showLoginDialogEvent, showRegisterDialogEvent } from '@/router'
import store from '@/store'
import UploadManagerDialog from '@/components/upload/UploadManagerDialog.vue'

const router = useRouter()
const route = useRoute()
const message = useMessage()

// Upload manager dialog
const showUploadManager = ref(false)

// Upload icon
const UploadIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor', width: 18, height: 18 }, [
  h('path', { d: 'M9 16h6v-6h4l-7-7-7 7h4zm-4 2h14v2H5z' })
])

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
})

onUnmounted(() => {
  showLoginDialogEvent.removeEventListener('show', handleShowLoginDialog)
  showRegisterDialogEvent.removeEventListener('show', handleShowRegisterDialog)
})

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

const handleLogin = async () => {
  if (!loginFormRef.value) return

  loginFormRef.value.validate(async (errors) => {
    if (errors) return

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

        message.success('登录成功')
        showLoginModal.value = false
        loginForm.username = ''
        loginForm.password = ''
        updateAuthState()
      } else {
        message.error(data.message || '登录失败')
      }
    } catch (error) {
      message.error('登录失败：' + error.message)
    } finally {
      loggingIn.value = false
    }
  })
}

const handleRegister = async () => {
  if (!registerFormRef.value) return

  registerFormRef.value.validate(async (errors) => {
    if (errors) return

    registering.value = true
    try {
      const data = await AuthApi.register(registerForm.username, registerForm.password)

      if (data.success) {
        message.success('注册成功，请登录')

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
        message.error(data.message || '注册失败')
      }
    } catch (error) {
      message.error('注册失败：' + error.message)
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
  padding: 8px 16px;
  color: rgba(255, 255, 255, 0.7);
  text-decoration: none;
  border-radius: @border-radius-sm;
  transition: all 0.2s;
  font-size: @font-size-base;

  &:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.1);
  }

  &.active {
    color: #fff;
    background: rgba(255, 102, 0, 0.3);
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

.upload-manager-btn {
  color: rgba(255, 255, 255, 0.75);
  margin-right: 8px;
  border-radius: 6px;
  transition: color 0.2s, background-color 0.2s, box-shadow 0.2s;

  &:hover {
    color: #fff;
    background: rgba(255, 255, 255, 0.2) !important;
    box-shadow: inset 0 0 0 1px rgba(255, 255, 255, 0.4);
  }
}
</style>
