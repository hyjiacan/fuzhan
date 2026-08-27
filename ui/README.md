# 浮栈 - 前端

基于 Vue 3 + Naive UI + Vite 构建的前端界面。

## 技术栈

- **Vue 3** (Composition API + `<script setup>`)
- **Vite 7** (构建工具)
- **Naive UI** (UI 组件库)
- **Vue Router** (Hash 模式路由)
- **Axios** (HTTP 客户端)
- **Less** (CSS 预处理器)
- **hash-wasm** (分片 xxh3 校验)
- **Playwright** (E2E 测试)

## 项目结构

```
src/
├── main.js              # 入口点（配置加载、通知轮询、Token验证）
├── App.vue              # 根组件
├── api/index.js         # Axios 客户端 + 11个 API 模块
├── assets/              # 静态资源
├── components/          # 可复用组件
│   ├── common/          # AppHeader, AppFooter, NotificationBell
│   ├── file/            # FileItem, FilePreview, RecentUploadsFull
│   ├── upload/          # UploadManager
│   ├── settings/        # 系统设置组件
│   ├── setup/           # 初始化向导组件
│   └── migration/       # 数据库迁移组件
├── views/               # 页面组件（13个）
├── router/index.js      # 路由配置
├── store/index.js       # 状态管理
├── composables/         # 组合式函数
├── config/              # 前端配置
├── plugins/             # Vue 插件注册
├── styles/              # 全局样式
└── utils/               # 工具函数
```

## 开发

```bash
yarn dev         # 启动开发服务器（localhost:5173）
yarn build       # 生产构建（输出到 dist/）
yarn preview     # 预览构建产物
yarn test        # Playwright 测试
yarn test:e2e    # E2E 测试（需先启动后端服务）
```

## 后端 API

所有 API 通过 `/api/v1/` 前缀访问，需要后端服务（8080 端口）运行中。

## 构建说明

生产构建后，产物位于 `dist/` 目录，会被嵌入到 Go 后端二进制文件中。
