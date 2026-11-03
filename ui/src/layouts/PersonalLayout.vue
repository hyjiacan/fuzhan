<template>
  <el-container class="personal-layout app-layout">
    <!-- Sidebar -->
    <el-aside :width="collapsed ? '64px' : '200px'" class="layout-sider">
      <!-- User info at top of sidebar -->
      <div class="sider-header" @click="collapsed = !collapsed" :title="collapsed ? '展开侧边栏' : '折叠侧边栏'">
        <span class="sider-brand-dot" :class="{ 'is-collapsed': collapsed }"></span>
        <transition name="fade">
          <span v-if="!collapsed" class="sider-title">个人中心</span>
        </transition>
        <el-icon v-if="collapsed" :size="20"><User /></el-icon>
      </div>

      <el-menu
        :collapse="collapsed"
        :collapse-transition="false"
        :default-active="activeKey"
        router
        class="layout-menu"
      >
        <el-menu-item v-for="opt in menuOptions" :key="opt.key" :index="opt.key">
          <el-icon><component :is="opt.icon" /></el-icon>
          <template #title>{{ opt.label }}</template>
        </el-menu-item>
      </el-menu>
    </el-aside>

    <!-- Content -->
    <el-main class="layout-content">
      <div class="layout-router">
        <router-view v-slot="{ Component }">
          <transition name="page-fade" mode="out-in">
            <component :is="Component" />
          </transition>
        </router-view>
      </div>
    </el-main>
  </el-container>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { User, InfoFilled, Coin, Folder, Clock, List } from '@element-plus/icons-vue'
import store from '@/store'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)

// 私有存储仅在启用时显示「私有文件」入口
const privateStorageEnabled = computed(() => store.state.config.privateStorageEnabled !== false)

const menuOptions = computed(() => {
  const options = [
    { label: '信息', key: '/user/info', icon: InfoFilled },
    { label: '存储', key: '/user/storage', icon: Coin },
    { label: '登录记录', key: '/user/login-logs', icon: Clock },
    { label: '操作记录', key: '/user/records', icon: List }
  ]
  if (privateStorageEnabled.value) {
    options.splice(2, 0, { label: '私有文件', key: '/user/files', icon: Folder })
  }
  return options
})

// Active key matches current route
const activeKey = computed(() => {
  const path = route.path
  for (const opt of menuOptions.value) {
    if (path.startsWith(opt.key)) {
      return opt.key
    }
  }
  return '/user/info'
})

const handleMenuSelect = (key) => {
  router.push(key)
}
</script>