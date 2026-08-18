import { createRouter, createWebHashHistory } from 'vue-router'
import { AuthApi } from '../api'
import store from '../store'

// 路由级代码分割 - 动态导入
const HomeView = () => import('../views/HomeView.vue')
const SetupView = () => import('../views/SetupView.vue')
const TempView = () => import('../views/TempView.vue')
const RecentView = () => import('../views/RecentView.vue')
const HotView = () => import('../views/HotView.vue')

// Layouts
const PersonalLayout = () => import(/* webpackChunkName: "layouts-personal" */ '../layouts/PersonalLayout.vue')
const AdminLayout = () => import('../layouts/AdminLayout.vue')

// Personal sub-views
const UserInfoSection = () => import(/* webpackChunkName: "views-user-info" */ '../components/user/UserInfoSection.vue')
const UserStorageSection = () => import(/* webpackChunkName: "views-user-storage" */ '../components/user/UserStorageSection.vue')
const UserPasswordSection = () => import('../components/user/UserPasswordSection.vue')
const PrivateStorageView = () => import(/* webpackChunkName: "views-private" */ '../views/PrivateStorageView.vue')

// Admin sub-views
const AdminDashboardView = () => import(/* webpackChunkName: "views-admin-dashboard" */ '../views/AdminDashboardView.vue')
const AdminFilesView = () => import(/* webpackChunkName: "views-admin-files" */ '../views/AdminFilesView.vue')
const AdminUsersView = () => import('../views/AdminUsersView.vue')
const AdminApiKeyView = () => import(/* webpackChunkName: "views-admin-api-key" */ '../views/AdminApiKeyView.vue')
const AdminUploadsView = () => import(/* webpackChunkName: "views-admin-uploads" */ '../views/AdminUploadsView.vue')
const AdminDuplicatesView = () => import(/* webpackChunkName: "views-admin-duplicates" */ '../views/AdminDuplicatesView.vue')
const SettingsView = () => import('../views/SettingsView.vue')

// 登录/注册弹框触发事件
export const showLoginDialogEvent = new EventTarget()
export const showRegisterDialogEvent = new EventTarget()

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    // ========== 匿名公共路由 ==========
    {
      path: '/files',
      name: 'files',
      component: HomeView,
      meta: { navName: '文件' }
    },
    {
      path: '/files/:pathMatch(.*)*',
      name: 'files-sub',
      component: HomeView
    },
    {
      path: '/setup',
      name: 'setup',
      component: SetupView
    },
    {
      path: '/temp',
      name: 'temp',
      component: TempView,
      meta: { navName: '临时' }
    },
    {
      path: '/recent',
      name: 'recent',
      component: RecentView,
      meta: { navName: '最近' }
    },
    {
      path: '/hot',
      name: 'hot',
      component: HotView,
      meta: { navName: '热门' }
    },

    // ========== 个人路由（普通用户） ==========
    {
      path: '/user',
      component: PersonalLayout,
      meta: { requiresAuth: true, navName: '个人' },
      children: [
        { path: '', redirect: { name: 'user-info' } },
        {
          path: 'info',
          name: 'user-info',
          component: UserInfoSection
        },
        {
          path: 'storage',
          name: 'user-storage',
          component: UserStorageSection
        },
        {
          path: 'files',
          name: 'user-files',
          component: PrivateStorageView
        },
        {
          path: 'password',
          name: 'user-password',
          component: UserPasswordSection
        }
      ]
    },

    // ========== 管理路由（管理员） ==========
    {
      path: '/admin',
      component: AdminLayout,
      meta: { requiresAuth: true, requiresAdmin: true, navName: '管理' },
      children: [
        { path: '', redirect: { name: 'admin-dashboard' } },
        {
          path: 'dashboard',
          name: 'admin-dashboard',
          component: AdminDashboardView
        },
        {
          path: 'files',
          name: 'admin-files',
          component: AdminFilesView
        },
        {
          path: 'files/:pathMatch(.*)*',
          name: 'admin-files-sub',
          component: AdminFilesView
        },
        {
          path: 'uploads',
          name: 'admin-uploads',
          component: AdminUploadsView
        },
        {
          path: 'duplicates',
          name: 'admin-duplicates',
          component: AdminDuplicatesView
        },
        {
          path: 'records',
          name: 'admin-records',
          component: () => import('@/views/AdminRecordsView.vue')
        },
        {
          path: 'users',
          name: 'admin-users',
          component: AdminUsersView
        },
        {
          path: 'api-keys',
          name: 'admin-api-keys',
          component: AdminApiKeyView
        },
        {
          path: 'settings',
          name: 'admin-settings',
          component: SettingsView
        }
      ]
    },

    // 旧路由重定向
    { path: '/private', redirect: '/user/files' },
    { path: '/settings', redirect: '/admin/settings' },

    // 默认重定向
    {
      path: '/:pathMatch(.*)*',
      redirect: '/files'
    }
  ]
})

// 外部标志（由 main.js 设置）
let shouldRedirectToSetup = false
export function setSetupRedirect(value) {
  shouldRedirectToSetup = value
}

// 路由守卫（Vue Router 4 推荐返回式，不依赖 next 回调）
router.beforeEach(async (to, from) => {
  // 未初始化时重定向到 setup
  if (shouldRedirectToSetup && to.name !== 'setup') {
    return { name: 'setup' }
  }

  const privateStorageEnabled = store?.state?.config?.privateStorageEnabled !== false

  if (!privateStorageEnabled) {
    // 私有存储未启用时，禁止访问私有/用户路由，直接重定向
    if (to.path.startsWith('/user/') || to.path === '/user') {
      return { name: 'files' }
    }
  }

  if (to.meta.requiresAuth) {
    const isValid = await store.actions.validateAuth()
    if (!isValid) {
      showLoginDialogEvent.dispatchEvent(new CustomEvent('show'))
      return { name: 'files' }
    }
  }

  if (to.meta.requiresAdmin) {
    const role = localStorage.getItem('userRole')
    if (role !== 'admin') {
      return { name: 'files' }
    }
  }
})

export default router
