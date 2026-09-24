<template>
  <el-container class="admin-layout">
    <!-- Sidebar -->
    <el-aside :width="collapsed ? '64px' : '200px'" class="layout-sider">
      <div class="sider-header" @click="collapsed = !collapsed" :title="collapsed ? '展开侧边栏' : '折叠侧边栏'">
        <span class="sider-brand-dot" :class="{ 'is-collapsed': collapsed }"></span>
        <transition name="fade">
          <span v-if="!collapsed" class="sider-title">系统管理</span>
        </transition>
        <el-icon v-if="collapsed" :size="20"><Setting /></el-icon>
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
            <template #title>
              <span class="menu-item-label">{{ opt.label }}</span>
              <el-badge
                v-if="opt.key === SECURITY_KEY && securityUnread > 0"
                :value="securityUnread > 99 ? '99+' : securityUnread"
                type="danger"
                class="menu-badge"
              />
            </template>
          </el-menu-item>
        </el-menu-item-group>
      </el-menu>
    </el-aside>

    <!-- Content -->
    <el-main class="layout-content">
      <div v-if="showWideTip" class="wide-tip">
        <el-icon class="wide-tip-icon"><Monitor /></el-icon>
        <span class="wide-tip-text">部分管理页表格在窄屏幕下可能显示不完整，建议使用更宽的屏幕以获得最佳体验</span>
        <el-icon class="wide-tip-close" @click="dismissWideTip" title="关闭提示"><Close /></el-icon>
      </div>
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
import { ref, computed, onMounted, onUnmounted, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { safeStorage } from '@/utils/storage'
import { SecurityApi } from '@/api'
import {
  Setting, DataBoard, Folder, UploadFilled, List, CopyDocument, Clock, User, Key, Monitor, Odometer, TrendCharts, Warning, Close
} from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)

// 窄屏宽屏提示（可关闭，持久化到本地）
const WIDE_TIP_KEY = 'admin_wide_tip_closed'
const showWideTip = ref(true)

onMounted(() => {
  showWideTip.value = !safeStorage.get(WIDE_TIP_KEY)
  loadSecurityUnread()
  securityTimer = setInterval(loadSecurityUnread, 60000)
})

onUnmounted(() => {
  if (securityTimer) clearInterval(securityTimer)
})

const dismissWideTip = () => {
  showWideTip.value = false
  safeStorage.set(WIDE_TIP_KEY, '1')
}

const menuGroups = [
  {
    title: '概览',
    items: [
      { label: '看板', key: '/admin/dashboard', icon: DataBoard },
      { label: '资源监控', key: '/admin/resource', icon: Odometer },
      { label: '行为分析', key: '/admin/behavior-analytics', icon: TrendCharts },
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
      { label: '安全事件', key: '/admin/security', icon: Warning },
      { label: 'OpenAPI', key: '/admin/openapi', icon: Key },
      { label: '设置', key: '/admin/settings', icon: Setting }
    ]
  }
]

// 扁平索引，供路由激活匹配
const menuOptions = menuGroups.flatMap(g => g.items)

// 安全事件菜单入口（用于红点标识）
const SECURITY_KEY = '/admin/security'
const securityUnread = ref(0)
let securityTimer = null

// 加载未读安全事件数（供侧边栏红点）
const loadSecurityUnread = async () => {
  try {
    const res = await SecurityApi.unreadSummary()
    if (res.success && res.data) {
      securityUnread.value = res.data.total || 0
    }
  } catch (e) {
    // 拉取失败时保留上次数值，静默处理
  }
}

// 进入安全事件页或标记已读后刷新，保证红点实时一致
watch(
  () => route.path,
  (p) => {
    if (p === SECURITY_KEY) loadSecurityUnread()
  }
)

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
  position: relative;
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
      padding: 4px 0 16px;

      .menu-item-label {
        overflow: hidden;
        text-overflow: ellipsis;
      }

      .menu-badge {
        margin-left: 8px;
        display: flex;

        :deep(.el-badge__content) {
          position: static;
          transform: none;
          line-height: 14px;
          height: 16px;
        }
      }
    }
  }

  .layout-menu {
    border-right: none;

    --el-menu-item-height: 40px;

    :deep(.el-menu-item-group__title) {
      padding: 14px 20px 6px;
      font-size: @font-size-xs;
      font-weight: 500;
      color: @text-color-placeholder;
      letter-spacing: 1px;
      transition: color @transition-fast;

      &:hover {
        color: @text-color-secondary;
      }
    }

    :deep(.el-menu-item-group:first-child .el-menu-item-group__title) {
      padding-top: 8px;
    }

    // 折叠态：隐藏分组标题，避免残留空白
    :deep(.el-menu--collapse .el-menu-item-group__title) {
      padding: 0;
      height: 0;
      overflow: hidden;
      opacity: 0;
    }
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

  // 窄屏宽屏提示（仅窄屏显示）
  .wide-tip {
    display: none;
    align-items: center;
    gap: 8px;
    padding: 8px 16px;
    margin: 8px 12px 0;
    font-size: @font-size-sm;
    color: @text-color-secondary;
    background: @bg-color-tertiary;
    border-radius: @border-radius;
    border: 1px solid @border-color-light;

    .wide-tip-icon {
      color: @warning-color;
      flex-shrink: 0;
    }

    .wide-tip-close {
      margin-left: auto;
      cursor: pointer;
      color: @text-color-placeholder;
      flex-shrink: 0;

      &:hover {
        color: @text-color;
      }
    }
  }

  @media (max-width: 768px) {
    .wide-tip {
      display: flex;
    }
  }
}
</style>
