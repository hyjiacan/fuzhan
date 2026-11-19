<template>
  <div class="oauth2-callback">
    <el-card class="callback-card" shadow="never">
      <div v-if="status === 'conflict'" class="conflict-body">
        <el-icon class="callback-icon warning"><WarningFilled /></el-icon>
        <h3 class="callback-title">发现同名本地账号：{{ conflict.username }}</h3>
        <p class="callback-desc">
          你正在使用 {{ conflict.provider_name || conflict.provider_id || '外部认证源' }} 登录。若这是你的账号，输入本地密码完成绑定；否则可使用独立账号登录。
        </p>

        <el-alert
          v-if="conflictError"
          :title="conflictError"
          type="error"
          :closable="false"
          show-icon
          class="conflict-error"
        />

        <el-form class="conflict-form" label-position="top" @submit.prevent="handleBind">
          <el-form-item label="本地密码">
            <el-input
              v-model="bindPassword"
              type="password"
              show-password
              autocomplete="current-password"
              placeholder="请输入同名本地账号的密码"
              :disabled="binding || creating"
            />
          </el-form-item>
          <el-button
            type="primary"
            class="conflict-submit"
            :loading="binding"
            :disabled="creating"
            @click="handleBind"
          >
            绑定到同名账号
          </el-button>
        </el-form>

        <el-divider>或</el-divider>

        <el-button
          class="conflict-submit"
          :loading="creating"
          :disabled="binding"
          @click="handleCreateAccount"
        >
          使用独立账号登录
        </el-button>
        <p class="callback-desc conflict-tip">独立账号与同名本地账号互不影响，登录后可在个人设置中查看。</p>
      </div>

      <div v-else class="callback-body">
        <el-icon v-if="status === 'pending'" class="callback-icon spin"><Loading /></el-icon>
        <el-icon v-else-if="status === 'success'" class="callback-icon success"><CircleCheckFilled /></el-icon>
        <el-icon v-else class="callback-icon error"><CircleCloseFilled /></el-icon>
        <h3 class="callback-title">{{ title }}</h3>
        <p class="callback-desc">{{ description }}</p>
        <el-button v-if="status === 'error'" type="primary" @click="backToLogin">返回登录</el-button>
      </div>
    </el-card>
  </div>
</template>

<script setup>
import { ref, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import { Loading, CircleCheckFilled, CircleCloseFilled, WarningFilled } from '@element-plus/icons-vue'
import { AuthApi, NotificationApi } from '@/api'
import { UserUtils } from '@/utils'
import { executeLoginDownload, isPageRedirect } from '@/utils/download'
import { promptIdentityNotice } from '@/utils/identityNotice'
import { ErrorCode, formatErrorMessage } from '@/utils/error'

const route = useRoute()
const router = useRouter()

const status = ref('pending')
const title = ref('正在完成登录')
const description = ref('正在校验外部认证信息，请稍候…')

// 同名冲突待确认：username 为同名本地账号，provider_name 为外部认证源名称
const conflict = ref(null)
const bindPassword = ref('')
const binding = ref(false)
const creating = ref(false)
const conflictError = ref('')

// 后端契约：票据过期 400、本地密码错误 401、管理员账号 403、尝试过多 429。
// 优先展示后端消息，缺失时按错误码回退；注意 429 的响应 code 也是 1004，与 400 共用回退文案
const CONFLICT_ERROR_FALLBACKS = {
  [ErrorCode.VALIDATION_ERROR]: '确认已过期或尝试过于频繁，请返回登录页重新认证',
  [ErrorCode.UNAUTHORIZED]: '本地密码错误，请重新输入',
  [ErrorCode.PERMISSION_DENIED]: '管理员账号不支持自助处理，请联系管理员'
}

const resolveConflictError = (error) => {
  const fallback = CONFLICT_ERROR_FALLBACKS[error?.code] || '操作失败，请稍后重试'
  return formatErrorMessage(error, fallback) || fallback
}

const backToLogin = () => {
  router.replace({ path: '/files' })
}

// 冲突处理后后端只返回会话概要，补查一次用户信息以拿到角色等完整字段（失败不阻断登录）
const resolveSessionUser = async (session) => {
  try {
    const res = await AuthApi.getUserInfo()
    if (res.success && res.data) return res.data
  } catch (e) {
    // 忽略，退回会话概要
  }
  // 会话概要不含角色：沿用绑定前的本地角色，避免管理员被降级为普通用户
  return { ...session, role: session.role || localStorage.getItem('userRole') || 'user' }
}

// 登录成功后的统一收尾：同步本地登录态、合并通知、按 redirect 跳转
const finishLogin = async (userData, successDesc) => {
  AuthApi.restoreSession(userData)
  localStorage.setItem('userRole', userData.role || 'user')

  try {
    await NotificationApi.merge()
  } catch (e) {
    // 通知合并失败不影响登录
  }

  // 通知全局刷新登录态与菜单
  window.dispatchEvent(new CustomEvent('auth:changed'))

  // 管理员完成外部身份绑定后，首次登录提示一次
  promptIdentityNotice(userData.identity_notice_pending, userData.identity_notice_provider)

  status.value = 'success'
  title.value = '登录成功'
  description.value = successDesc

  ElMessage.success({ message: '登录成功', duration: 1000 })
  const redirect = typeof route.query.redirect === 'string' && route.query.redirect.startsWith('/')
    ? route.query.redirect
    : '/files'
  setTimeout(async () => {
    // 后端直出页面整页跳转回原地址
    if (isPageRedirect(redirect)) {
      window.location.href = redirect
      return
    }
    // 下载类 redirect（后端 302 携带）：换一次性票据后整页跳转执行下载，
    // hash 路由会吞掉非 hash 路径，不能用 router.replace
    if (await executeLoginDownload(redirect)) return
    router.replace(redirect)
  }, 600)
}

// 使用本地密码确认绑定到同名账号
const handleBind = async () => {
  if (binding.value || creating.value) return
  if (!bindPassword.value) {
    ElMessage.warning('请输入本地密码')
    return
  }

  conflictError.value = ''
  binding.value = true
  try {
    const res = await AuthApi.confirmIdentityBind(bindPassword.value)
    if (!res.success || !res.data) {
      conflictError.value = res.message || '绑定失败，请稍后重试'
      return
    }
    bindPassword.value = ''
    const userData = await resolveSessionUser(res.data)
    const label = UserUtils.displayLabel(userData) || userData.username || ''
    await finishLogin(userData, `欢迎回来，${label}`)
  } catch (error) {
    conflictError.value = resolveConflictError(error)
  } finally {
    binding.value = false
  }
}

// 使用独立账号登录：二次确认后由后端创建独立账号
const handleCreateAccount = async () => {
  if (binding.value || creating.value) return

  try {
    await ElMessageBox.confirm(
      '将创建一个新的独立账号，与同名本地账号互不影响。确定继续吗？',
      '使用独立账号登录',
      { type: 'warning', confirmButtonText: '创建并登录', cancelButtonText: '取消' }
    )
  } catch {
    return
  }

  conflictError.value = ''
  creating.value = true
  try {
    const res = await AuthApi.createIdentityAccount()
    if (!res.success || !res.data) {
      conflictError.value = res.message || '创建独立账号失败，请稍后重试'
      return
    }
    await finishLogin(await resolveSessionUser(res.data), `已创建独立账号：${res.data.username || ''}`)
  } catch (error) {
    conflictError.value = resolveConflictError(error)
  } finally {
    creating.value = false
  }
}

onMounted(async () => {
  const error = route.query.error
  if (error) {
    status.value = 'error'
    title.value = '外部认证失败'
    // 后端只回传通用提示，具体失败原因已记录在服务端日志中，此处不再展示原始内容
    description.value = '外部认证未完成，请返回登录页重试或联系管理员'
    return
  }

  // 先检查是否存在同名冲突待确认（此时尚未建立登录会话，/auth/user 不可用）。
  // 待确认票据由后端 HttpOnly Cookie 维护，前端不接触、不传递
  try {
    const confirmRes = await AuthApi.getIdentityConfirm()
    if (confirmRes?.success && confirmRes.data?.pending) {
      conflict.value = confirmRes.data
      status.value = 'conflict'
      return
    }
  } catch (e) {
    // 查询失败按无冲突处理，继续原有会话确认流程
  }

  // 凭证由后端回调直接写入 HttpOnly Cookie，URL 中不再携带令牌；
  // 以 /auth/user 能否通过来确认会话是否建立成功。
  let userData
  try {
    const res = await AuthApi.getUserInfo()
    if (!res.success || !res.data) throw new Error('登录状态无效')
    userData = res.data
  } catch (e) {
    status.value = 'error'
    title.value = '外部认证失败'
    description.value = '登录状态无效，请返回登录页重新认证'
    return
  }

  await finishLogin(userData, `欢迎回来，${UserUtils.displayLabel(userData) || ''}`)
})
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.oauth2-callback {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 60vh;
  padding: @container-padding;

  .callback-card {
    width: 380px;
    max-width: 100%;
  }

  .callback-body {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 16px 8px;
    text-align: center;
  }

  .conflict-body {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 16px 8px;
    text-align: center;
  }

  .callback-icon {
    font-size: 42px;
    color: @text-color-secondary;

    &.success { color: @success-color; }
    &.error { color: @error-color; }
    &.warning { color: @warning-color; }
    &.spin { animation: oauth2-spin 1s linear infinite; }
  }

  .callback-title {
    margin: 4px 0 0;
    font-size: @font-size-lg;
    font-weight: 600;
    word-break: break-all;
  }

  .callback-desc {
    margin: 0;
    color: @text-color-secondary;
    font-size: @font-size-sm;
    word-break: break-all;
  }

  .conflict-error {
    width: 100%;
    text-align: left;
  }

  .conflict-form {
    width: 100%;
    text-align: left;
  }

  .conflict-submit {
    width: 100%;
  }

  .conflict-tip {
    margin-top: 0;
  }
}

@keyframes oauth2-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>
