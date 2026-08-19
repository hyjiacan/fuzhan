# 实施计划

## 总览
共分 10 个工作项，按依赖关系排序。每个工作项完成后：测试 → 提交代码 → 开始下一个。

---

## 工作项 1: 修复临时文件删除 onPositiveClick 错误
**难度**: 低 | **影响范围**: 前端 TempView.vue
- 将 TempView.vue 中 `handleDelete` 函数的 `dialog.warning({...})` 调用改为使用 `useDialog()` 返回的 dialog 实例正确调用
- 确认 `onPositiveClick` 在 dialog API 中正确传入

## 工作项 2: 重复文件检查跳过 0 字节文件
**难度**: 低 | **影响范围**: 后端 index/service.go, DuplicateQuery
- 在 `ListDuplicateGroups` 的 SQL 查询中增加 `file_size > 0` 条件
- 确保 `DuplicateQuery` 结构体中的 `MinSize` 默认值调整为 > 0

## 工作项 3: Footer 添加帮助和关于入口
**难度**: 低 | **影响范围**: AppFooter.vue
- 在 Footer 右侧添加"帮助"和"关于"链接
- "关于"点击弹出对话框显示应用信息（版本、描述等）
- "帮助"链接到文档或说明页面

## 工作项 4: 用户管理列表显示配额使用情况
**难度**: 中 | **影响范围**: 后端 admin 服务, admin handler, 前端 AdminUsersView.vue
- 后端新增 API 接口 `GET /api/v1/admin/users/quota` 返回用户配额使用情况
- 修改 `UserItem` 结构体引入 `QuotaUsed` 字段
- 后端在 `ListUsers` 中查询每个用户的私有文件使用量
- 前端用户列表表格增加"配额使用"列

## 工作项 5: 下载通知合并到上传管理 + 修复 hover 配色
**难度**: 中 | **影响范围**: NotificationBell.vue, UploadManagerBar.vue, UploadManagerDialog.vue, AppHeader.vue
- 移除 `NotificationBell.vue` 组件
- 在上传管理对话框中集成 URL 下载任务列表和通知功能
- 移除 AppHeader 中的通知铃铛按钮
- 修复上传管理按钮在深色背景下的 hover 配色

## 工作项 6: 临时文件允许重复下载 + 下载后删除选项
**难度**: 中 | **影响范围**: 后端 temp_file_service.go, handler.go, models/temp_file.go, 前端 TempView.vue
- 后端 `TempFile` 模型增加 `AllowRepeatDownload` 字段
- 修改 `DownloadFile` 逻辑：当允许重复下载时，不限制下载次数
- 前端上传时增加"下载后自动删除"选项开关
- 前端临时文件列表显示下载次数和"下载后删除"状态

## 工作项 7: 设置页重构
**难度**: 高 | **影响范围**: SettingsView.vue, 后端 config handler
- 重新组织 Tab 结构：
  - 基本信息（应用信息、共享目录配置、私有文件配置、临时文件配置、上传配置、URL 上传配置）
  - 服务配置（WEB 服务、FTP 服务、FTPS 服务、WebDAV 服务、TLS 证书）
  - 存储配置
  - 预览配置
  - 数据库
  - 访问控制（IP 访问控制、文件扩展名用多行文本框）
- 卡片布局改为每行 2 列（宽度自适应）
- 各配置项卡片按内容相关性排序
- 文件扩展名输入改为多行文本框

## 工作项 8: 任务增强 - 数据库表 + 执行历史 + 状态进度
**难度**: 高 | **影响范围**: 后端 models, services, handlers, 前端多个组件
- 新增 `TaskRecord` 数据库表记录任务执行历史
- 新增后端 API：任务列表、任务历史、任务状态查询
- 前端新增任务管理页面或面板
- 显示当前执行任务的状态和进度

## 工作项 9: OpenAPI 独立菜单 + API KEY 合并 + 统计移到看板
**难度**: 高 | **影响范围**: 后端路由, 前端路由, SettingsView, AdminLayout, AdminDashboardView, AdminApiKeyView
- 新建 OpenAPI 管理页面（合并现有 API Key 管理功能）
- 从设置页移除 OpenAPI 选项卡
- 将 OpenAPI 调用统计移到看板页
- 从 AdminLayout 侧边栏移除旧的 API Key 菜单项，添加 OpenAPI 菜单项
- 前端路由新增 `/admin/openapi` 路由

## 工作项 10: 综合测试与修复
**难度**: 中 | **影响范围**: 全局
- 编译前后端验证所有功能正常
- 检查各页面渲染是否正确
- 修复发现的问题