<template>
  <n-layout class="personal-layout" has-sider>
    <!-- Sidebar -->
    <n-layout-sider
      bordered
      collapse-mode="width"
      :collapsed-width="64"
      :width="200"
      :collapsed="collapsed"
      show-trigger="bar"
      @collapse="collapsed = true"
      @expand="collapsed = false"
      class="layout-sider"
    >
      <!-- User info at top of sidebar -->
      <div class="sider-header">
        <n-ellipsis v-if="!collapsed">
          <span class="sider-title">个人中心</span>
        </n-ellipsis>
        <n-ellipsis v-else>
          <n-icon size="20"><UserIcon /></n-icon>
        </n-ellipsis>
      </div>

      <n-menu
        :collapsed="collapsed"
        :collapsed-width="64"
        :collapsed-icon-size="22"
        :options="menuOptions"
        :value="activeKey"
        @update:value="handleMenuSelect"
      />
    </n-layout-sider>

    <!-- Content -->
    <n-layout-content class="layout-content">
      <router-view />
    </n-layout-content>
  </n-layout>
</template>

<script setup>
import { ref, computed, h, watch } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import {
  NLayout, NLayoutSider, NLayoutContent, NMenu, NIcon, NEllipsis
} from 'naive-ui'

const router = useRouter()
const route = useRoute()
const collapsed = ref(false)

// Icons
const UserIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M12 12c2.21 0 4-1.79 4-4s-1.79-4-4-4-4 1.79-4 4 1.79 4 4 4zm0 2c-2.67 0-8 1.34-8 4v2h16v-2c0-2.66-5.33-4-8-4z' })
])
const InfoIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm1 15h-2v-6h2v6zm0-8h-2V7h2v2z' })
])
const StorageIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M2 20h20v-4H2v4zm2-3h2v2H4v-2zM2 4v4h20V4H2zm4 3H4V5h2v2zm-4 7h20v-4H2v4zm2-3h2v2H4v-2z' })
])
const FolderIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M10 4H4c-1.1 0-1.99.9-1.99 2L2 18c0 1.1.9 2 2 2h16c1.1 0 2-.9 2-2V8c0-1.1-.9-2-2-2h-8l-2-2z' })
])
const PasswordIcon = () => h('svg', { xmlns: 'http://www.w3.org/2000/svg', viewBox: '0 0 24 24', fill: 'currentColor' }, [
  h('path', { d: 'M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1s3.1 1.39 3.1 3.1v2z' })
])

const menuOptions = [
  {
    label: '信息',
    key: '/user/info',
    icon: () => h(NIcon, null, () => h(InfoIcon))
  },
  {
    label: '存储',
    key: '/user/storage',
    icon: () => h(NIcon, null, () => h(StorageIcon))
  },
  {
    label: '私有文件',
    key: '/user/files',
    icon: () => h(NIcon, null, () => h(FolderIcon))
  },
  {
    label: '修改密码',
    key: '/user/password',
    icon: () => h(NIcon, null, () => h(PasswordIcon))
  }
]

// Active key matches current route
const activeKey = computed(() => {
  const path = route.path
  if (path.startsWith('/user/info')) return '/user/info'
  if (path.startsWith('/user/storage')) return '/user/storage'
  if (path.startsWith('/user/files')) return '/user/files'
  if (path.startsWith('/user/password')) return '/user/password'
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

    .sider-header {
      padding: 16px;
      border-bottom: 1px solid @border-color-light;
      font-weight: 600;
      font-size: @font-size-lg;
      color: @text-color;
      display: flex;
      align-items: center;
      gap: 8px;
    }
  }

  .layout-content {
    background: @bg-color-secondary;
    height: 100%;
    overflow: auto;
  }
}
</style>
