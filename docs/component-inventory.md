# UI 组件清单

> 本文档由项目扫描自动生成 | 更新时间: 2026-07-02

---

## 目录

- [组件](#组件)
- [页面](#页面)
- [API 客户端](#api-客户端)
- [状态管理](#状态管理)
- [路由配置](#路由配置)
- [Element Plus 组件使用统计](#element-plus-组件使用统计)
- [组件关系图](#组件关系图)

---

## 组件

### 通用组件

#### AppHeader 应用头部导航

| 属性 | 类型 | 说明 |
|------|------|------|
| - | - | - |

**功能**: 应用顶部导航栏，含Logo、菜单、登录/注册弹框

**子组件**:
- LoginModal (el-dialog) - 登录弹框
- RegisterModal (el-dialog) - 注册弹框
- NotificationBell - 通知铃铛（未读计数）

---

#### AppFooter 应用底部栏

**功能**: 应用底部栏，显示系统消息和链接

---

#### NotificationBell 通知铃铛

**功能**: 显示未读通知数量，点击弹出通知列表

---

### 文件相关组件

#### FileItem 文件项组件

| 属性 | 类型 | 说明 |
|------|------|------|
| `fileInfo` | Object | 文件信息对象 |
| `showActions` | Boolean | 是否显示操作按钮 |

| 事件 | 参数 | 说明 |
|------|------|------|
| `preview` | fileInfo | 预览文件 |
| `download` | fileInfo | 下载文件 |

**功能**: 文件/目录列表项，显示文件名、大小、时间，提供预览/下载按钮

---

#### FilePreview 文件预览组件

| 属性 | 类型 | 说明 |
|------|------|------|
| `file` | Object | 文件信息 |

**功能**: 支持文本（分段加载）、图片、PDF预览

---

#### RecentUploadsFull 最近上传完整列表

| 属性 | 类型 | 说明 |
|------|------|------|
| `uploads` | Array | 上传记录数组 |

| 事件 | 参数 | 说明 |
|------|------|------|
| `go-to` | path | 跳转到文件位置 |
| `download` | record | 下载文件 |

**功能**: 最近上传记录的完整表格展示，含定位和下载功能

---

### 上传组件

#### UploadManager 上传管理组件

弹窗（el-dialog）已内聚到组件内部，调用方通过 `v-model` 控制显隐，无需再包一层 dialog；header 提供放大/还原按钮（撑满窗口），footer 渲染底部操作。

| 属性 | 类型 | 必填 | 说明 |
|------|------|------|------|
| `modelValue` | Boolean | 是 | 弹窗显隐（v-model） |
| `uploadApi` | Object | 是 | 上传API地址配置（type 必须为 chunked） |
| `title` | String | 否 | 弹窗标题，默认「上传文件」 |
| `defaultDir` | String | 否 | 默认上传目录 |
| `deleteOnDownload` | Boolean | 否 | 临时文件「下载后自动删除」勾选（支持 v-model:delete-on-download 双向同步） |

| 事件 | 参数 | 说明 |
|------|------|------|
| `update:modelValue` | boolean | 弹窗显隐变化 |
| `update:deleteOnDownload` | boolean | 「下载后自动删除」变化 |
| `upload-success` | response | 上传成功 |
| `upload-error` | error | 上传失败 |
| `upload-start` | file | 开始上传 |
| `upload-change` | progress | 上传进度变化 |
| `close` | - | 关闭上传面板 |

**功能**: 文件上传管理，支持本地/URL/剪贴板/新建文本四种方式（左侧 tabs 切换）、拖放、分片上传、队列管理、断点续传；「读取剪贴板」按钮位于剪贴板 tab-pane 内；弹窗关闭保护（有上传任务时二次确认）由组件内部处理

---

### Toast 组件

#### Toast 通知提示组件

| 属性 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `message` | String | - | 消息文本 |
| `type` | String | info | 消息类型 (info/success/warning/error) |
| `duration` | Number | 3000 | 显示时长(ms) |
| `suggestion` | String | - | 建议文本 |
| `retryable` | Boolean | false | 是否可重试 |
| `showRetry` | Boolean | false | 显示重试按钮 |

| 事件 | 参数 | 说明 |
|------|------|------|
| `close` | - | 关闭通知 |
| `retry` | - | 点击重试 |

**功能**: Toast通知提示，支持4种类型、建议文本、重试按钮

---

#### ToastContainer Toast容器

**功能**: Toast容器包装组件，集成 useToast composable

---

### 其他组件目录

| 目录 | 说明 |
|------|------|
| `settings/` | 系统设置相关组件 |
| `setup/` | 初始化向导相关组件 |

---

## 页面

| 页面 | 路由 | 主要组件 | 功能描述 |
|------|------|----------|----------|
| HomeView | `/files`, `/files/:path*` | el-button, el-table-v2, el-breadcrumb, el-dialog, el-input | 首页文件浏览，含搜索、上传、面包屑导航、文件预览；对 `canManage` 为真的文件（管理员或上传者IP一致）显示重命名/删除操作 |
| SetupView | `/setup` | el-steps, el-form, el-input, el-input-number, el-radio-group | 首次运行配置向导，分步完成基本配置 |
| TempView | `/temp` | el-table-v2, el-tag, el-dialog, el-input | 临时文件管理，基于IP的访问码分享系统 |
| PrivateStorageView | `/private` | el-table-v2, el-tag, el-dialog, el-progress | 私有存储管理，需登录，上传/删除/分享文件 |
| RecentView | `/recent` | el-table-v2, el-button, el-tag, el-empty | 最近上传文件列表展示 |
| HotView | `/hot` | el-table-v2, el-tag, el-empty | 热门下载文件排行 |
| UserView | `/user` | el-card, el-descriptions, el-progress, el-form | 个人中心，用户信息、配额展示、修改密码 |
| SettingsView | `/settings` | el-tabs, el-form, el-input, el-switch, el-card | 系统设置，多标签页配置 |
| AdminDashboardView | `/admin/dashboard` | el-card, el-descriptions, el-progress | 管理员仪表盘，统计卡片、存储使用、热门搜索 |
| AdminFilesView | `/admin/files`, `/admin/files/:path*` | el-breadcrumb, el-table-v2, el-tree-select | 管理员文件管理，支持搜索、重命名、移动、删除 |
| AdminUsersView | `/admin/users` | el-table-v2, el-dialog, el-tag | 用户管理，搜索、启用/禁用、重置密码、删除用户 |
| AdminTasksView | `/admin/tasks` | el-table-v2, el-button, el-progress | 任务管理，查看/执行定时任务 |
| AdminUploadsView | `/admin/uploads` | el-table-v2, el-pagination | 上传管理，跟踪上传会话、清理残留 |
| AdminRecordsView | `/admin/records` | el-table-v2, el-button | 记录管理，查看系统操作记录 |
| AdminDuplicatesView | `/admin/duplicates` | el-table-v2, el-tag | 重复文件管理，检测与清理重复文件 |
| AdminApiKeyView / AdminOpenApiView | `/admin/openapi` | el-form, el-input, el-table-v2 | OpenAPI 与 API Key 管理 |
| AdminOnlineView | `/admin/online` | el-table-v2, el-alert, el-button | 在线 IP 统计，展示当前在线 IP 及在线时长，自动刷新 |

---

## API 客户端

位置: `ui/src/api/index.js`

### API 模块

| 模块 | 导出 | 说明 |
|------|------|------|
| AuthApi | 认证相关 | login, register, getUserInfo, changePassword |
| ConfigApi | 配置相关 | get, save, reload |
| FileApi | 文件操作 | list, recent, rename, move, preview, previewChunk, delete |
| UploadApi | 上传相关 | session, chunk, finalize, urlUpload, urlFileInfo |
| AdminApi | 管理功能 | users, sessions, files, urlTasks, tls, onlineIps |
| PrivateApi | 私有存储 | files, quota, session, chunk |
| TempApi | 临时文件 | list, upload, download, session, extend |
| SetupApi | 初始化 | status, save, validateDir, networkInterfaces, defaultConfig |
| SystemApi | 系统 | getOptions, health, ready |
| MonitorApi | 监控 | storage, access, keywords, rankings, hotDownloads |
| NotificationApi | 通知 | getNotifications, markAsRead, mergeAnonymous |

### Axios 配置

- 基础URL: `/api/v1`
- 超时: 30秒
- 请求拦截器: 添加 Authorization header
- 响应拦截器: 处理 401/403，自动刷新 token

---

## 状态管理

位置: `ui/src/store/index.js`

### 状态结构

```javascript
{
  user: {
    authenticated: boolean,
    name: string,
    avatar: string,
    token: string
  },
  config: {
    appName: string,
    version: string,
    privateStorageEnabled: boolean,
    tempFilesEnabled: boolean,
    ftpEnabled: boolean,
    ftpPort: number
  },
  currentPath: string,
  fileList: Array,
  loading: boolean,
  breadcrumb: Array,
  message: {
    text: string,
    type: string
  },
  searchState: {
    isSearching: boolean,
    isCompleted: boolean,
    query: string,
    searchTime: number
  },
  notifications: Array,
  activeUrlTasks: boolean
}
```

### 核心方法

| 方法 | 说明 |
|------|------|
| `setUser(user)` | 设置用户信息 |
| `setAuthenticated(bool)` | 设置认证状态 |
| `setConfig(config)` | 设置系统配置 |
| `setFileList(files)` | 设置文件列表（自动排序） |
| `setCurrentPath(path)` | 设置当前路径 |
| `loadFileList(path)` | 加载文件列表（含面包屑构建） |
| `searchFiles(query)` | 搜索文件（SSE，含超时控制） |
| `adminSearchFiles(query)` | 管理员搜索文件（SSE，不记历史） |
| `validateAuth()` | 验证本地登录状态（含JWT exp检查） |
| `setNotifications(notifications)` | 设置通知列表 |
| `setActiveUrlTasks(bool)` | 设置活跃URL任务状态（控制通知轮询） |
| `clearSearchResources()` | 清理搜索SSE连接和定时器 |

---

## 路由配置

位置: `ui/src/router/index.js`

### 路由表

| 路径 | 页面 | meta | 说明 |
|------|------|------|------|
| `/files` | HomeView | navName: '文件' | 文件浏览 |
| `/files/:pathMatch(.*)*` | HomeView | - | 子路径 |
| `/setup` | SetupView | - | 初始化 |
| `/private` | PrivateStorageView | requiresAuth, hideFromNavIfAdmin | 私有存储 |
| `/temp` | TempView | navName: '临时' | 临时文件 |
| `/recent` | RecentView | navName: '最近' | 最近上传 |
| `/hot` | HotView | navName: '热门' | 热门下载 |
| `/user` | UserView | requiresAuth, hideFromNavIfAdmin | 个人中心 |
| `/admin/users` | AdminUsersView | requiresAuth, requiresAdmin, navName | 用户管理 |
| `/admin/dashboard` | AdminDashboardView | requiresAuth, requiresAdmin, navName | 仪表盘 |
| `/settings` | SettingsView | requiresAuth, requiresAdmin, navName | 设置 |
| `/admin/files` | AdminFilesView | requiresAuth, requiresAdmin, navName | 文件管理 |
| `/admin/files/:pathMatch(.*)*` | AdminFilesView | requiresAuth, requiresAdmin | 子路径 |
| `/admin/zombie` | AdminZombieView | requiresAuth, requiresAdmin, navName | 僵尸文件 |
| `/admin/url-tasks` | AdminURLTaskView | requiresAuth, requiresAdmin, navName | URL下载任务 |
| `/admin/online` | AdminOnlineView | requiresAuth, requiresAdmin, navName | 在线IP |
| `/:pathMatch(.*)*` | - | - | 默认重定向到 /files |

### 路由模式

使用 `createWebHashHistory()` - Hash 路由模式

### 路由守卫

- 未初始化时重定向到 `/setup`
- `requiresAuth` 页面：检查 token 有效期，无效则弹出登录框
- `requiresAdmin` 页面：检查 userRole === 'admin'
- 私有存储未启用时禁止访问 `/private` 和 `/user`
- 登录弹框触发事件：`showLoginDialogEvent`

---

## Element Plus 组件使用统计

> 前端架构已于 2026-08 从 Naive UI 迁移至 Element Plus 2.4.4。
> 以下为迁移后主要组件及使用页面统计。

| 组件类型 | 使用页面数 | 典型使用页面 |
|---------|-----------|----------|
| el-button | 全部 | 所有页面 |
| el-table-v2 | 8 | HomeView, TempView, PrivateStorageView, RecentView, HotView, AdminFilesView, AdminUsersView, AdminUploadsView |
| el-dialog | 7 | HomeView, TempView, PrivateStorageView, UserView, AppHeader, AdminUsersView, UploadStatusDialog |
| el-card | 6 | UserView, SettingsView, AdminDashboardView, HotView, RecentView, SetupView |
| el-form/el-form-item | 6 | SetupView, UserView, SettingsView, AppHeader, AdminUsersView, UploadManager |
| el-input | 6 | HomeView, TempView, SetupView, SettingsView, AdminUsersView, PrivateStorageView |
| el-icon | 5 | AdminDashboardView, AdminUsersView, AppHeader |
| el-tag | 4 | TempView, PrivateStorageView, RecentView, AppHeader |
| el-empty | 4 | HomeView, RecentView, HotView, AdminDashboardView |
| el-alert | 3 | SetupView, AdminDashboardView |
| el-progress | 3 | PrivateStorageView, UserView, AdminDashboardView |
| el-breadcrumb | 2 | HomeView, AdminFilesView, PrivateStorageView, TempView |
| el-tabs/el-tab-pane | 2 | SettingsView, SetupView |
| el-switch | 2 | SettingsView, SetupView |
| el-tree-select | 1 | AdminFilesView |
| el-descriptions | 1 | UserView, AdminDashboardView |
| el-badge | 1 | AdminDashboardView |
| el-input-number | 2 | SetupView, SettingsView, AdminDashboardView |
| el-radio-group | 2 | SetupView, SettingsView |
| el-steps | 1 | SetupView |
| el-pagination | 4 | RecentView, AdminFilesView, AdminUploadsView, AdminTasksView |

---

## 组件关系图

```
App.vue
├── AppHeader
│   ├── LoginModal (el-dialog)
│   ├── RegisterModal (el-dialog)
│   └── NotificationBell
├── router-view
│   ├── HomeView
│   │   ├── UploadManager (子组件)
│   │   ├── FilePreview (弹框)
│   │   └── ToastContainer
│   ├── TempView
│   │   ├── UploadManager
│   │   └── FilePreview
│   ├── PrivateStorageView
│   │   └── UploadManager
│   ├── RecentView
│   │   └── RecentUploadsFull
│   ├── HotView
│   ├── SetupView
│   ├── UserView
│   ├── SettingsView
│   ├── AdminDashboardView
│   ├── AdminFilesView
│   ├── AdminUsersView
│   ├── AdminTasksView
│   ├── AdminUploadsView
│   └── AdminOpenApiView
└── AppFooter
    └── ToastContainer
        └── Toast

ToastContainer
└── Toast (多个)
```
