<template>
  <div class="login-page">
    <div class="login-card">
      <div class="login-header">
        <img src="/assets/icons/logo.svg" class="login-logo" alt="浮栈" />
        <h3 class="login-title">{{ isRegisterMode ? '创建账号' : '欢迎回来' }}</h3>
        <p class="login-subtitle">
          {{ isRegisterMode ? `加入 ${appName}，让文件有处可栖` : `登录 ${appName}，继续你的文件旅程` }}
        </p>
      </div>

      <!-- 登录表单 -->
      <el-form
        v-if="!isRegisterMode"
        ref="loginFormRef"
        :model="loginForm"
        :rules="loginRules"
        label-position="top"
        @keydown.enter="handleLogin"
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="loginForm.username"
            :maxlength="64"
            size="large"
            placeholder="请输入用户名"
            autocomplete="username"
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
          >
            <template #prefix><el-icon><Lock /></el-icon></template>
          </el-input>
        </el-form-item>
        <!-- 图形验证码：required 时展示（服务端按 IP 判定或验证码校验失败后强制） -->
        <CaptchaInput
          :visible="loginCaptchaRequired"
          :refresh-nonce="loginRefreshNonce"
          @change="setLoginCaptcha"
        />
      </el-form>

      <!-- 注册表单 -->
      <el-form
        v-else
        ref="registerFormRef"
        :model="registerForm"
        :rules="registerRules"
        label-position="top"
        @keydown.enter="handleRegister"
      >
        <el-form-item label="用户名" prop="username">
          <el-input
            v-model="registerForm.username"
            :maxlength="64"
            size="large"
            placeholder="请输入用户名"
            autocomplete="username"
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
          >
            <template #prefix><el-icon><Lock /></el-icon></template>
          </el-input>
        </el-form-item>
        <!-- 注册侧验证码，与登录侧独立维护，切换模式时不串值 -->
        <CaptchaInput
          :visible="registerCaptchaRequired"
          :refresh-nonce="registerRefreshNonce"
          @change="setRegisterCaptcha"
        />
      </el-form>

      <!-- 外部统一认证入口（OAuth2/OIDC，由后端配置决定是否展示） -->
      <div v-if="oauth2Enabled && !isRegisterMode" class="auth-external-block">
        <el-divider class="auth-divider">或</el-divider>
        <el-button
          size="large"
          class="auth-external"
          :loading="oauth2Redirecting"
          @click="handleOAuth2Login"
        >
          <el-icon v-if="!oauth2Redirecting" class="auth-external-icon"><Key /></el-icon>
          使用 {{ oauth2ProviderName }} 登录
        </el-button>
      </div>

      <div class="login-footer">
        <el-button
          type="primary"
          size="large"
          :loading="submitting"
          class="login-submit"
          @click="isRegisterMode ? handleRegister() : handleLogin()"
        >
          {{ isRegisterMode ? '注册' : '登录' }}
        </el-button>

        <div v-if="isRegisterMode" class="auth-switch">
          <span class="auth-switch-text">已有账号？</span>
          <el-button link type="primary" class="switch-link" @click="isRegisterMode = false">
            去登录
          </el-button>
        </div>
        <div v-else-if="allowRegistration" class="auth-switch">
          <span class="auth-switch-text">还没有账号？</span>
          <el-button link type="primary" class="switch-link" @click="isRegisterMode = true">
            立即注册
          </el-button>
        </div>

        <!-- 非强制登录模式：匿名访客可返回继续浏览 -->
        <div v-if="!requireLogin" class="auth-switch auth-anon">
          <el-button link type="info" class="switch-link" @click="backToBrowse">
            先不登录，匿名浏览
          </el-button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage } from 'element-plus'
import { User, Lock, Key } from '@element-plus/icons-vue'
import { AuthApi, NotificationApi } from '@/api'
import { executeLoginDownload } from '@/utils/download'
import CaptchaInput from '@/components/auth/CaptchaInput.vue'
import store from '@/store'

const route = useRoute()
const router = useRouter()

const appName = computed(() => store.state.config.appName || '浮栈')
const allowRegistration = computed(() => store.state.config.allowRegistration === true)
const requireLogin = computed(() => store.state.config.requireLogin === true)

// 服务端按当前 IP 判定的初始「是否需要验证码」
const captchaRequiredByServer = computed(() => store.state.config.captchaRequired === true)

const isRegisterMode = ref(false)
const submitting = ref(false)
const loginFormRef = ref(null)
const registerFormRef = ref(null)
const loginForm = reactive({ username: '', password: '' })
const registerForm = reactive({ username: '', password: '', confirmPassword: '' })

// 登录/注册侧验证码各自独立维护：是否需要展示、当前 captchaId 与答案。
// 初始值取服务端 required 判定；账号切换（登录<->注册）互不干扰
const loginCaptchaRequired = ref(captchaRequiredByServer.value)
const loginCaptcha = reactive({ id: '', answer: '' })
const registerCaptchaRequired = ref(captchaRequiredByServer.value)
const registerCaptcha = reactive({ id: '', answer: '' })
// 强制刷新信号：验证码被服务端销毁（captcha_invalid）后递增，让 CaptchaInput 重新拉图
const loginRefreshNonce = ref(0)
const registerRefreshNonce = ref(0)

const setLoginCaptcha = (cid, answer) => {
  loginCaptcha.id = cid
  loginCaptcha.answer = answer
}
const setRegisterCaptcha = (cid, answer) => {
  registerCaptcha.id = cid
  registerCaptcha.answer = answer
}
const loginRules = {
  username: { required: true, message: '请输入用户名', trigger: 'blur' },
  password: { required: true, message: '请输入密码', trigger: 'blur' }
}
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

// 外部 OAuth2/OIDC 认证入口
const oauth2Enabled = ref(false)
const oauth2ProviderName = ref('OAuth2')
const oauth2Redirecting = ref(false)

const loadOAuth2Status = async () => {
  try {
    const res = await AuthApi.oauth2Status()
    if (res.success && res.data) {
      oauth2Enabled.value = !!res.data.enabled
      oauth2ProviderName.value = res.data.providerName || 'OAuth2'
    }
  } catch (e) {
    oauth2Enabled.value = false
  }
}

const handleOAuth2Login = async () => {
  oauth2Redirecting.value = true
  try {
    const res = await AuthApi.oauth2Authorize()
    if (res.success && res.data?.authorizeUrl) {
      window.location.href = res.data.authorizeUrl
    } else {
      ElMessage.error(res.message || '无法发起外部认证')
      oauth2Redirecting.value = false
    }
  } catch (e) {
    ElMessage.error('无法发起外部认证：' + (e?.message || e))
    oauth2Redirecting.value = false
  }
}

const finishAuth = async () => {
  window.dispatchEvent(new CustomEvent('auth:changed'))
  ElMessage.success('登录成功')
  // 登录后回到发起页面（默认文件页）
  const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
    ? route.query.redirect
    : '/files'
  // 下载类 redirect（后端 302 携带）：换一次性票据后整页跳转执行下载，
  // hash 路由会吞掉非 hash 路径，不能用 router.replace
  if (await executeLoginDownload(redirect)) return
  router.replace(redirect)
}

// 判断错误是否为「验证码缺失/错误」：响应体 errors.code === 'captcha_invalid'，
// 或顶层错误消息包含「验证码」。此时需展示并刷新验证码，让用户重新输入后重试
const isCaptchaError = (error) => {
  if (!error) return false
  const codes = error.details?.errors
  if (codes?.code === 'captcha_invalid') return true
  const msg = String(error.message || '')
  return msg.includes('验证码') && (msg.includes('错误') || msg.includes('无效') || msg.includes('正确') || msg.includes('请输入'))
}

const handleLogin = async () => {
  // 去重守卫：回车/点击可能连续触发，验证码一次性，重复提交必要失败
  if (submitting.value) return
  if (!loginFormRef.value) return
  loginFormRef.value.validate(async (valid) => {
    if (!valid) return
    // 验证码已要求但尚未就绪（首次展示拉图未完成）时不发起提交
    if (loginCaptchaRequired.value && (!loginCaptcha.id || !loginCaptcha.answer)) {
      ElMessage.warning('请先输入验证码')
      return
    }
    submitting.value = true
    try {
      const data = await AuthApi.login(loginForm.username, loginForm.password, loginCaptcha.id, loginCaptcha.answer)
      if (data.success) {
        // 凭证由后端写入 HttpOnly Cookie，本地仅标记登录态
        AuthApi.restoreSession(data.data)
        try {
          const userData = await AuthApi.getUserInfo()
          if (userData.success && userData.data) {
            localStorage.setItem('userRole', userData.data.role || 'user')
          }
        } catch (e) {
          // 角色获取失败不阻断登录
        }
        try {
          await NotificationApi.merge()
        } catch (e) {
          // 合并失败不影响登录流程
        }
        finishAuth()
      } else {
        ElMessage.error(data.message || '登录失败')
      }
    } catch (error) {
      if (isCaptchaError(error)) {
        // 服务端要求验证码：展示并强制刷新验证码（旧 id 已被服务端销毁），输入内容等待重试
        loginCaptchaRequired.value = true
        loginRefreshNonce.value += 1
        ElMessage.warning('请输入验证码后重试')
        loginCaptcha.answer = ''
      } else {
        ElMessage.error('登录失败：' + error.message)
      }
    } finally {
      submitting.value = false
    }
  })
}

const handleRegister = async () => {
  // 去重守卫：回车/点击可能连续触发，验证码一次性，重复提交必要失败
  if (submitting.value) return
  if (!registerFormRef.value) return
  registerFormRef.value.validate(async (valid) => {
    if (!valid) return
    // 验证码已要求但尚未就绪（首次展示拉图未完成）时不发起提交
    if (registerCaptchaRequired.value && (!registerCaptcha.id || !registerCaptcha.answer)) {
      ElMessage.warning('请先输入验证码')
      return
    }
    submitting.value = true
    try {
      const data = await AuthApi.register(registerForm.username, registerForm.password, registerCaptcha.id, registerCaptcha.answer)
      if (data.success) {
        // 注册即登录（与轻量版一致）：服务端已签发 Cookie 会话，这里同步本地登录态
        AuthApi.restoreSession(data.data)
        try {
          const userData = await AuthApi.getUserInfo()
          if (userData.success && userData.data) {
            localStorage.setItem('userRole', userData.data.role || 'user')
          }
        } catch (e) {
          // 角色获取失败不阻断登录
        }
        ElMessage.success('注册成功，已自动登录')
        finishAuth()
      } else {
        ElMessage.error(data.message || '注册失败')
      }
    } catch (error) {
      if (isCaptchaError(error)) {
        // 服务端要求验证码：展示并强制刷新验证码（旧 id 已被服务端销毁），输入内容等待重试
        registerCaptchaRequired.value = true
        registerRefreshNonce.value += 1
        ElMessage.warning('请输入验证码后重试')
        registerCaptcha.answer = ''
      } else {
        ElMessage.error('注册失败：' + error.message)
      }
    } finally {
      submitting.value = false
    }
  })
}

const backToBrowse = () => {
  router.replace('/files')
}

onMounted(async () => {
  // 本地已标记登录 → 直接离开登录页
  if (AuthApi.isLoggedIn()) {
    leaveLoginPage()
    return
  }
  // localStorage 无登录态时，仍可能持有后端 HttpOnly Cookie 会话
  //（如 lite 端登录后打开 Vue 登录页的场景）。此时向服务端验证登录态，
  // 通过则恢复前端登录态并自动回跳，避免停留在登录页。
  try {
    const data = await AuthApi.getUserInfo()
    if (data.success && data.data) {
      const userData = data.data
      AuthApi.restoreSession({ uuid: userData.uuid, username: userData.username })
      store.setAuthenticated(true)
      if (userData.role) {
        localStorage.setItem('userRole', userData.role)
      }
      window.dispatchEvent(new CustomEvent('auth:changed'))
      leaveLoginPage()
      return
    }
  } catch (e) {
    // 无会话 / 会话失效 / 后端不可用：停留在登录页，不打断用户
  }
  loadOAuth2Status()
  // 聚焦用户名输入框
  loginFormRef.value?.focus?.()
})

// 已登录时离开登录页：优先执行下载类 redirect，否则回跳 redirect 指定的站内地址（默认文件页）
const leaveLoginPage = async () => {
  const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
    ? route.query.redirect
    : ''
  // executeLoginDownload 为 async（恒返回 Promise），必须 await 判断其结果，
  // 直接 if(...) 会把始终 truthy 的 Promise 当作已发下载 → 下面的 replace 永不执行
  if (await executeLoginDownload(redirect)) return
  router.replace(redirect || '/files')
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.login-page {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 100%;
  padding: @container-padding;

  .login-card {
    width: 400px;
    max-width: 100%;
    background: var(--el-bg-color);
    border: 1px solid var(--el-border-color-light);
    border-radius: 8px;
    padding: 32px 32px 24px;
    box-shadow: 0 6px 24px rgba(0, 0, 0, 0.08);
  }

  .login-header {
    text-align: center;
    margin-bottom: 24px;

    .login-logo {
      width: 48px;
      height: 48px;
      margin-bottom: 12px;
    }

    .login-title {
      margin: 0 0 6px;
      font-size: 20px;
      font-weight: 600;
    }

    .login-subtitle {
      margin: 0;
      color: @text-color-secondary;
      font-size: 13px;
    }
  }

  .auth-external-block {
    .auth-external {
      width: 100%;
      margin-top: 4px;
    }
    .auth-external-icon {
      margin-right: 6px;
    }
  }

  .login-footer {
    margin-top: 8px;

    .login-submit {
      width: 100%;
    }

    .auth-switch {
      margin-top: 12px;
      text-align: center;
      font-size: 13px;

      .auth-switch-text {
        color: @text-color-secondary;
      }

      &.auth-anon {
        margin-top: 8px;
      }
    }
  }
}
</style>
