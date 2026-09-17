<template>
  <el-config-provider :locale="zhCn">
    <AppShell />
    <ToastContainer />
    <GlobalLoadingBar />
  </el-config-provider>
</template>

<script setup>
import { onMounted } from 'vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import '@/styles/global.less'
import AppShell from '@/components/common/AppShell.vue'
import ToastContainer from '@/components/ToastContainer.vue'
import GlobalLoadingBar from '@/components/common/GlobalLoadingBar.vue'
import store from '@/store'
import { setGlobalErrorHandler, SystemApi } from '@/api/index.js'
import { useToast } from '@/composables/useToast.js'
import { useLiteModeSuggestion } from '@/composables/useLiteModeSuggestion'
import { safeStorage } from '@/utils/storage'
import { useTheme } from '@/composables/useTheme'

// 初始化主题（配合 index.html 首屏脚本，跟随系统 + 手动切换 + 持久化）
const { init: initTheme } = useTheme()
initTheme()

// 页面宽度偏好
const STORAGE_KEY = 'page-width-preference'
const pageWidth = ref('100%')

const loadPageWidth = () => {
  const saved = safeStorage.get(STORAGE_KEY)
  if (saved) {
    pageWidth.value = saved
    document.body.style.setProperty('--app-width', saved)
  }
}

// 监听 localStorage 变化（跨标签页同步）
window.addEventListener('storage', (e) => {
  if (e.key === STORAGE_KEY && e.newValue) {
    pageWidth.value = e.newValue
    document.body.style.setProperty('--app-width', e.newValue)
  }
})

// 设置全局错误处理器
const toast = useToast()
setGlobalErrorHandler((error) => {
  toast.fromError(error)
})

// 健康检查
const checkHealth = async () => {
  try {
    const res = await SystemApi.health()
    if (res.success && res.data && res.data.status !== 'healthy') {
      const checks = res.data.checks || {}

      if (checks.database?.status !== 'ok') {
        toast.warning('数据库: ' + (checks.database?.message || '连接失败'))
      }
      if (checks.storage?.status !== 'ok') {
        toast.warning('存储: ' + (checks.storage?.message || '不可用'))
      }
    }
  } catch (e) {
    toast.error('无法连接到服务器')
  }
}

onMounted(() => {
  // 页面卡顿检测：提示用户是否切换到简洁模式（选择存 localStorage）
  useLiteModeSuggestion().start(10000)

  // 健康检查延迟到浏览器空闲时执行（非首屏关键请求），避免与首屏关键请求争抢带宽。
  // requestIdleCallback 不支持的浏览器回退到 8 秒后执行。
  const runHealthCheck = () => checkHealth()
  if ('requestIdleCallback' in window) {
    window.requestIdleCallback(runHealthCheck, { timeout: 8000 })
  } else {
    setTimeout(runHealthCheck, 8000)
  }
})

loadPageWidth()
</script>

<style lang="less">
@import '@/styles/variables.less';

html,
body {
  width: 100%;
  height: 100%;
  margin: 0;
  padding: 0;
}

body {
  --app-width: 100%;
}

/* element-plus 主色主题覆盖（可读主色加深以满足 WCAG AA：白色文字对比度 ≥4.5:1） */
:root {
  --el-color-primary: #b25a00;
  --el-color-primary-light-3: #c98c4d;
  --el-color-primary-light-5: #d8ad80;
  --el-color-primary-light-7: #e8ceb3;
  --el-color-primary-light-8: #f0decc;
  --el-color-primary-light-9: #f7efe6;
  --el-color-primary-dark-2: #8f4800;
}

/* 主按钮：hover/active 加深而非变浅，保证白色文字在 AA 对比度线上 */
.el-button--primary {
  --el-button-bg-color: #b25a00;
  --el-button-border-color: #b25a00;
  --el-button-hover-bg-color: #9c4f00;
  --el-button-hover-border-color: #9c4f00;
  --el-button-active-bg-color: #7a3c00;
  --el-button-active-border-color: #7a3c00;
  --el-button-text-color: #ffffff;
  --el-button-hover-text-color: #ffffff;
  --el-button-active-text-color: #ffffff;
}
</style>
