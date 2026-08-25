<template>
  <el-config-provider :locale="zhCn">
    <AppShell />
    <ToastContainer />
  </el-config-provider>
</template>

<script setup>
import { onMounted } from 'vue'
import zhCn from 'element-plus/es/locale/lang/zh-cn'
import '@/styles/global.less'
import AppShell from '@/components/common/AppShell.vue'
import ToastContainer from '@/components/ToastContainer.vue'
import store from '@/store'
import { setGlobalErrorHandler, SystemApi } from '@/api/index.js'
import { useToast } from '@/composables/useToast.js'

// 页面宽度偏好
const STORAGE_KEY = 'page-width-preference'
const pageWidth = ref('100%')

const loadPageWidth = () => {
  const saved = localStorage.getItem(STORAGE_KEY)
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
  // 检查本地存储的 token 是否有效
  const token = localStorage.getItem('token')
  if (token) {
    store.actions.validateAuth()
  }

  setTimeout(() => {
    // Setup 页面不需要健康检查 — AppShell 内部已处理
    checkHealth()
  }, 3000);
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

/* element-plus 主色主题覆盖（原 naive-ui 橙色主题） */
:root {
  --el-color-primary: #ffa500;
  --el-color-primary-light-3: #ffb733;
  --el-color-primary-light-5: #ffca73;
  --el-color-primary-light-7: #ffdda3;
  --el-color-primary-light-8: #ffe8c6;
  --el-color-primary-light-9: #fff4e4;
  --el-color-primary-dark-2: #e69500;
}
</style>
