<template>
  <el-container class="admin-layout">
    <!-- Sidebar -->
    <el-aside :width="collapsed ? '64px' : '200px'" class="layout-sider">
      <div class="sider-header" @click="collapsed = !collapsed">
        <span v-if="!collapsed" class="sider-title">系统管理</span>
        <el-icon v-else :size="20"><Setting /></el-icon>
      </div>

      <el-menu
        :collapse="collapsed"
        :collapse-transition="false"
        :default-active="activeKey"
        router
        class="layout-menu"
      >
        <el-menu-item-group v-for="group in menuGroups" :key="group.title">
          <template #title>
            <span class="menu-group-title">{{ group.title }}</span>
          </template>
          <el-menu-item v-for="opt in group.items" :key="opt.key" :index="opt.key">
            <el-icon><component :is="opt.icon" /></el-icon>
            <template #title>{{ opt.label }}</template>
          </el-menu-item>
        </el-menu-item-group>
      </el-menu>
    </el-aside>

    <!-- Content -->
    <el-main class="layout-content">
      <router-view />
    </el-main>
  </el-container>
</template>

<script setup>
import { ref, computed } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  Setting, DataBoard, Folder, UploadFilled, List, CopyDocument, Clock, User, Key, Monitor, Odometer, TrendCharts
} from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)

const menuGroups = [
  {
    title: '概览',
    items: [
      { label: '看板', key: '/admin/dashboard', icon: DataBoard },
      { label: '资源监控', key: '/admin/resource', icon: Odometer },
      { label: '下载行为分析', key: '/admin/download-analytics', icon: TrendCharts },
      { label: '在线IP', key: '/admin/online', icon: Monitor }
    ]
  },
  {
    title: '内容',
    items: [
      { label: '文件管理', key: '/admin/files', icon: Folder },
      { label: '重复文件', key: '/admin/duplicates', icon: CopyDocument },
      { label: '记录管理', key: '/admin/records', icon: Clock }
    ]
  },
  {
    title: '上传任务',
    items: [
      { label: '上传管理', key: '/admin/uploads', icon: UploadFilled },
      { label: '任务管理', key: '/admin/tasks', icon: List }
    ]
  },
  {
    title: '系统',
    items: [
      { label: '用户管理', key: '/admin/users', icon: User },
      { label: 'OpenAPI', key: '/admin/openapi', icon: Key },
      { label: '设置', key: '/admin/settings', icon: Setting }
    ]
  }
]

// 扁平索引，供路由激活匹配
const menuOptions = menuGroups.flatMap(g => g.items)

// Active key matches current route
const activeKey = computed(() => {
  const path = route.path
  for (const opt of menuOptions) {
    if (path.startsWith(opt.key)) {
      return opt.key
    }
  }
  return '/admin/dashboard'
})

const handleMenuSelect = (key) => {
  router.push(key)
}
</script>

<style lang="less" scoped>
@import '@/styles/variables.less';

.admin-layout {
  height: 100%;

  .layout-sider {
    background: @bg-color;
    height: 100%;
    border-right: 1px solid @border-color-light;
    transition: width @transition-normal;

    .sider-header {
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
    }
  }

  .layout-menu {
    border-right: none;

    :deep(.el-menu-item-group__title) {
      padding: 14px 20px 6px;
      font-size: @font-size-xs;
      font-weight: 500;
      color: @text-color-placeholder;
      letter-spacing: 1px;
    }

    :deep(.el-menu-item-group:first-child .el-menu-item-group__title) {
      padding-top: 8px;
    }
  }

  .layout-content {
    background: @bg-color-secondary;
    height: 100%;
    overflow: auto;
    padding: 0;
  }
}
</style>