<template>
  <div class="oauth2-callback">
    <el-card class="callback-card" shadow="never">
      <div class="callback-body">
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
import { ElMessage } from 'element-plus'
import { Loading, CircleCheckFilled, CircleCloseFilled } from '@element-plus/icons-vue'
import { AuthApi, NotificationApi } from '@/api'

const route = useRoute()
const router = useRouter()

const status = ref('pending')
const title = ref('正在完成登录')
const description = ref('正在校验外部认证信息，请稍候…')

const backToLogin = () => {
  router.replace({ path: '/files' })
}

onMounted(async () => {
  const error = route.query.error
  if (error) {
    status.value = 'error'
    title.value = '外部认证失败'
    description.value = String(error)
    return
  }

  const token = route.query.token
  const uuid = route.query.uuid
  const username = route.query.username
  if (!token || !uuid || !username) {
    status.value = 'error'
    title.value = '外部认证失败'
    description.value = '回调参数缺失，无法完成登录'
    return
  }

  AuthApi.setAuth(String(token), String(uuid), String(username))
  try {
    const userData = await AuthApi.getUserInfo()
    if (userData.success && userData.data) {
      localStorage.setItem('userRole', userData.data.role || 'user')
    }
  } catch (e) {
    // 角色信息获取失败不阻断登录
  }

  try {
    NotificationApi.merge()
  } catch (e) {
    // 通知合并失败不影响登录
  }

  // 通知全局刷新登录态与菜单
  window.dispatchEvent(new CustomEvent('auth:changed'))

  status.value = 'success'
  title.value = '登录成功'
  description.value = `欢迎回来，${username}`

  ElMessage.success('登录成功')
  const redirect = typeof route.query.redirect === 'string' ? route.query.redirect : '/files'
  setTimeout(() => {
    router.replace(redirect.startsWith('/') ? redirect : '/files')
  }, 600)
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

  .callback-icon {
    font-size: 42px;
    color: @text-color-secondary;

    &.success { color: @success-color; }
    &.error { color: @error-color; }
    &.spin { animation: oauth2-spin 1s linear infinite; }
  }

  .callback-title {
    margin: 4px 0 0;
    font-size: @font-size-lg;
    font-weight: 600;
  }

  .callback-desc {
    margin: 0;
    color: @text-color-secondary;
    font-size: @font-size-sm;
    word-break: break-all;
  }
}

@keyframes oauth2-spin {
  from { transform: rotate(0deg); }
  to { transform: rotate(360deg); }
}
</style>