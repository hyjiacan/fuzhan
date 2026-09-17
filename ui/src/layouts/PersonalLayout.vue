<template>
  <el-container class="personal-layout">
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
        <transition name="page-fade" mode="out-in">
          <router-view />
        </transition>
      </div>
    </el-main>
  </el-container>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { User, InfoFilled, Coin, Folder, Lock } from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)

const menuOptions = [
  { label: '信息', key: '/user/info', icon: InfoFilled },
  { label: '存储', key: '/user/storage', icon: Coin },
  { label: '私有文件', key: '/user/files', icon: Folder },
  { label: '修改密码', key: '/user/password', icon: Lock }
]

// Active key matches current route
const activeKey = computed(() => {
  const path = route.path
  for (const opt of menuOptions) {
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

<style lang="less" scoped>
@import '@/styles/variables.less';

.personal-layout {
  height: 100%;

  .layout-sider {
    background: @bg-color;
    height: 100%;
    border-right: 1px solid @border-color-light;
    transition: width @transition-normal;
    overflow-x: hidden;
    display: flex;
    flex-direction: column;

    .sider-header {
      flex: 0 0 auto;
      padding: 16px;
      border-bottom: 1px solid @border-color-light;
      font-weight: 600;
      font-size: @font-size-lg;
      color: @text-color;
      display: flex;
      align-items: center;
      gap: 8px;
      cursor: pointer;
      user-select: none;
      white-space: nowrap;
      overflow: hidden;
      transition: background-color @transition-fast;

      &:hover {
        background-color: var(--el-fill-color-light);
      }

      .sider-brand-dot {
        flex: 0 0 auto;
        width: 10px;
        height: 10px;
        border-radius: 50%;
        background: @primary-accent;
        box-shadow: 0 0 6px fade(@primary-accent, 60%);
        transition: transform @transition-smooth;

        &.is-collapsed {
          transform: scale(1.15);
        }
      }

      .sider-title {
        overflow: hidden;
        text-overflow: ellipsis;
      }
    }

    .layout-menu {
      flex: 1 1 auto;
      overflow-y: auto;
      overflow-x: hidden;
      padding: 8px 0 16px;
    }
  }

  .layout-menu {
    border-right: none;
  }

  .layout-content {
    background: @bg-color-secondary;
    height: 100%;
    overflow: auto;
    padding: 0;

    .layout-router {
      height: 100%;
      min-height: 0;
      display: flex;
      flex-direction: column;

      & > * {
        flex: 1 1 auto;
        min-height: 0;
        display: flex;
        flex-direction: column;
      }
    }
  }
}
</style>