# snake_case 迁移后 前后端字段对齐审计

> 背景：2026-11-11 的 snake_case 全量迁移（commit `992521e`）后，重新启动服务，界面上多个表格列显示 `-`（取不到值）。
> 本文档记录逐页排查结论，每条结论附「前端位置 ↔ 后端位置」。
>
> 状态图例：`确认不一致` / `确认一致` / `待验证`

---

## 零、根因（已取证）

**不是代码未对齐，而是运行中的后端是旧进程。**

| 证据 | 内容 |
|---|---|
| 端口 8888 的进程 | PID 12396，`fuzhan.exe`，`CreationDate = 2026/11/11 15:16:35`，路径 `%TEMP%\go-build4072917458\b001\exe\fuzhan.exe`（即 11-11 那次 `go run` 留下的进程） |
| 该进程实际响应 | `GET /api/v1/web/files/list/` 返回 `{"name":"baidunetdiskdownload","type":"directory","path":"...","fullPath":"...","modifiedTime":"2026-11-11T16:04:52","isDir":true,"rootName":"...","downloadCount":0,"dependencyCount":0,"canManage":false}` —— **全 camelCase** |
| 新代码的响应结构 | [handler.go](file:///d:/Projects/lightshare/server/internal/file/handler.go#L62-L80) `FileRecordInfo` 已是 `full_path` / `modified_time` / `is_dir` / `root_name` / `download_count` / `dependency_count` / `can_manage` |
| 前端 | `yarn dev`（端口 3000，PID 23424，`CreationDate = 2026/11/16 10:03:55`）跑的是迁移后的新代码，列 key 为 `modified_time` / `download_count` / `dependency_count` |
| `bin/` 里的二进制 | `fuzhan.exe` / `fuzhan` 均为 `2026/11/11 11:50~11:51`，二进制内 `modifiedTime` 出现 28 次、`dependency_count` 0 次 —— 同样是迁移前的产物 |

**结论：** 新前端 + 旧后端 → 除 `name` / `type` / `path` / `size` / `notes` 外，其余列（修改时间、下载次数、依赖数等）全部取不到值，显示 `-`。**重启后端即可消除，无需改代码。**

**处置：** 结束 PID 12396，用当前代码重新启动后端（`cd server; go run .`），再逐页验证。

---

## 一、已确认的问题

见「零、根因」。代码层面暂无确认不一致项（静态比对结果见第三节汇总）。

---

## 二、已确认无问题的页面（重启后端后浏览器实测）

处置：结束 PID 12396，`cd server; go run .` 重启。新进程 `GET /api/v1/web/files/list/` 已返回 `full_path` / `modified_time` / `is_dir` / `root_name` / `download_count` / `dependency_count` / `can_manage`。

| 页面 | URL | 实测结果 |
|---|---|---|
| 首页文件列表 | `/#/files` | ✅ 根目录行完整显示：名称 `baidunetdiskdownload`、大小 `-`（目录，符合预期）、修改时间 `2026-11-11 16:04:52`、下载次数 `-`（0 次，符合预期）、备注「添加备注」 |
| 最近 | `/#/recent` | ✅ 最近上传表：文件名/路径/大小/备注/上传时间全部有值（如 `clipboard-1792029290845.txt` / `274 B` / `2026-10-15 09:54:55`）；最近搜索关键词正常 |
| 热门 | `/#/hot` | ✅ 热门搜索关键词有计数（`kafka 2`、`cherry 1`）；热门下载文件表排名/路径/大小/备注/时间/次数全有值 |
| 临时 | `/#/temp` | ✅ 表头正常（文件名/大小/访问码/上传时间/过期时间/操作），空态「暂无临时文件」；用量显示 `- / 无限制` |

> 备注：`/#/private`、`/#/admin` 未登录时重定向回 `/#/files` 并弹出登录框，属预期行为，需登录后验证（见第五节）。

---

## 三、逐页静态核对

### 1. 首页文件列表（HomeView）

- 前端列定义：[columns.js](file:///d:/Projects/lightshare/ui/src/views/home/columns.js#L140-L187)
  - 读取 `row.name` / `row.size` / `row.modified_time` / `row.download_count` / `row.notes` / `row.dependency_count` / `row.path` / `row.type`
- 后端：[handler.go](file:///d:/Projects/lightshare/server/internal/file/handler.go#L62-L80) `FileRecordInfo`
  - `name` / `type` / `path` / `full_path` / `modified_time` / `size` / `notes` / `xxh3_hash` / `hash_status` / `record_id` / `is_dir` / `root_name` / `download_count` / `dependency_count` / `can_manage` / `uploader_ip`
- 结论：**确认一致**（旧进程返回 camel 导致显示 `-`，已由根因解释）

### 2. 私有存储（PrivateStorageView）

- 前端：[PrivateStorageView.vue](file:///d:/Projects/lightshare/ui/src/views/PrivateStorageView.vue) 目录行由前端本地构造（`_type: 'dir'`、`dirName`），文件行展开后端 `files[]`，读 `row.filename` / `row.file_size` / `row.code` / `row.upload_time`
- 后端：[private.go](file:///d:/Projects/lightshare/server/internal/file/private.go) 的 `gin.H` key 为 `filename` / `file_size` / `upload_time` / `owner` / `code`
- 结论：**确认一致**（`dirName` 是前端本地字段，非契约）

### 3. 临时存储（TempView）

- 前端：`TempView.vue` 读 `files[]`；`subdirs` 由前端映射为本地行对象（`dirName`）
- 后端：[temp_file.go](file:///d:/Projects/lightshare/server/internal/models/temp_file.go#L27-L64)
  - `TempFileResponse`：`id` / `code` / `filename` / `file_size` / `client_ip` / `download_count` / `expired_at` / `created_at`
  - `TempListResponse`：`files` / `subdirs` / `quota`（`used` / `limit`）
- 结论：**确认一致**

### 4. 文件预览（FilePreview）

- 前端：`FilePreview.vue` 读 `data.mime_type` / `data.previewable` / `data.content` / `data.chunked` / `data.total_chunks` / `data.size` / `data.file_url`
- 后端：[preview.go](file:///d:/Projects/lightshare/server/internal/file/preview.go#L129-L155) 动态赋值 `content` / `encoding` / `chunked` / `chunk_size` / `total_chunks` / `file_url`
- 结论：**确认一致**

### 5. 管理后台 - 任务（AdminTasksView）

- 前端列：`task_name` / `error_message` / `details`；行内 `row.task_type` / `row.status` / `row.progress` / `row.total_items` / `row.done_items` / `row.started_at` / `row.ended_at` / `row.trigger`
- 后端：[task_record.go](file:///d:/Projects/lightshare/server/internal/models/task_record.go) `TaskRecordResponse` 全 snake
- 结论：**确认一致**

### 6. Webhook 派发 payload

- 后端：[task_service.go](file:///d:/Projects/lightshare/server/internal/services/task_service.go#L186-L194) 动态 key `task_type` / `task_name` / `status` / `trigger` / `error_message`
- 结论：**确认一致**（本次迁移已修）

### 7. 健康检查

- 后端：[health/handler.go](file:///d:/Projects/lightshare/server/internal/health/handler.go) `checks["root_dirs"]` / `checks["temp_dir"]`
- 旧进程实测响应：`checks` 只有 `database` / `storage`，无 camel 残留
- 结论：**确认一致**

---

## 四、全仓静态比对结果

### 4.1 前端读 snake 键但后端不存在

扫描口径：前端所有 `xxx.<snake_key>` 形态的读取，逐个比对后端全部 `json`/`form` tag、`gin.H`/map 字面量 key、以及 `result["key"] =` 下标赋值。

结果：**仅 4 处命中，且全部是同一字段的误报** —— `FilePreview.vue:441-445` 的 `data.file_url`。该 key 由后端 [preview.go:155](file:///d:/Projects/lightshare/server/internal/file/preview.go#L155) 的 `result["file_url"] = fileUrl` 动态赋值，第一版扫描脚本未覆盖下标赋值，**实际存在，非问题**。

补扫后端下标赋值后共发现 55 个动态 key（`file_url` / `chunk_size` / `total_chunks` / `hash_status` / `uploader_ip` / `root_dirs` / `temp_dir` / `task_type` 等），全部为 snake_case，无 camel 残留。

### 4.2 前端读 camel 键但后端不存在

扫描口径：前端 `res.data.xxx` / `row.xxx` / `item.xxx` 等读取中所有 camelCase 键，比对后端全部 key 集合（含下标赋值）。命中 **117 条**，**逐条核对全部为前端组件内部 state，非 API 契约**，分类如下：

| 类别 | 代表键 | 位置 | 判定 |
|---|---|---|---|
| 上传队列本地对象 | `uploadId` / `displayText` / `checksumAlgorithms` / `checksumStorageType` / `checksumRootName` / `checksumPath` / `checksumCode` / `checksumLoading` / `checksumError` / `showNotes` / `showDeps` / `depFileName` / `depRelation` / `depDescription` / `depFileRecordId` | `components/upload/hooks/useUploadQueue.js`、`useUrlBatchUpload.js`、`useDependency.js`、`UploadManager.vue`、`UploadQueue.vue` | 前端本地队列字段，合法 |
| URL 下载任务本地对象 | `taskId` / `fileName` / `fileSize` / `retryCount` / `targetExists` / `alreadyUploaded` | `useUrlBatchUpload.js`、`UploadManager.vue` | 从后端 snake 字段（`task_id`/`file_name`/`file_size`/`retry_count`/`target_exists`/`already_uploaded`）赋值后存入本地对象，读的是本地对象，合法 |
| 目录行本地构造 | `dirName` | `PrivateStorageView.vue:262-264`、`TempView.vue:345-347` | 由后端 `subdirs: []string` 映射而来，前端本地字段，合法 |
| 设置差异项本地构造 | `oldText` / `newText` | `views/settings/SettingsChangesDrawer.vue` | `useSettingsDiff.js` 本地生成，合法 |
| JS 内置方法误报 | `startsWith` / `toLowerCase` | `useUploadQueue.js:56,82`、`AdminWebhookView.vue:266`、`SettingsView.vue:195`、`AdminSecurityView.vue:27` | 非字段，扫描误报 |

**结论：前端不存在读取后端 camel 字段的地方**，契约边界已全部对齐。

### 4.3 前端发 camel 请求体

`ui/src/api/index.js` 的请求体已统一为 snake（`upload_id` / `captcha_id` / `captcha_answer` / `page_size` / `storage_type` / `root_name` 等）。上传队列组装 payload 时显式转换（`useUploadQueue.js:174-179`），无 camel 泄漏。


---

## 五、浏览器逐页验证

环境：前端 `yarn dev`（http://localhost:3000，PID 23424，2026/11/16 10:03 启动）+ 后端 `go run .`（http://127.0.0.1:8888，2026/11/16 10:28 重启后）。

| 页面 | 结果 | 证据 |
|---|---|---|
| `/#/files` 首页 | ✅ 通过 | 根目录 `baidunetdiskdownload`，修改时间 `2026-11-11 16:04:52`（旧进程时为 `-`） |
| `/#/recent` 最近 | ✅ 通过 | 上传表 5 列全部有值；搜索关键词列表正常 |
| `/#/hot` 热门 | ✅ 通过 | 关键词计数、排名、文件大小、时间、下载次数全部有值 |
| `/#/temp` 临时 | ✅ 通过 | 表头与空态正常 |
| `/#/user/files` 私有存储 | ✅ 通过 | 表头（文件名/大小/访问码/上传时间/操作）正常，空态「私有存储暂无文件」；`用量: - / 10 GB` 中 `-` 是该账号实际用量为 0，`formatSize(0)` 显示 `-`，非字段问题 |
| `/#/admin/dashboard` 看板 | ✅ 通过 | 已用存储 `24.01 GB`、文件总数 `286`、重复文件组 `0`、活跃用户 `1`；存储使用情况（分区/配额/使用率）、临时文件配额、私有存储配额、热门搜索词、最近上传全部有值 |
| `/#/admin/users` 用户管理 | ✅ 通过 | 6 条用户全列有值（用户名/姓名/状态/用户ID/注册时间/配额 `0 B / 10.0 GB`、`0%`）；每行第一个 `-` 为「昵称」列且该字段确实为空（数据库无 nickname），非错位 |
| `/#/admin/files` 文件管理 | ✅ 通过 | 表头（文件名/大小/上传 IP/修改时间/备注/操作），行内修改时间有值 |
| `/#/admin/duplicates` 重复文件 | ✅ 通过 | 空态「暂无重复文件」 |
| `/#/admin/records` 操作记录 | ✅ 通过 | 4 个 Tab 表头完整；文件路径/大小/IP `::1`/上传时间全部有值 |
| `/#/admin/tasks` 系统作业 | ✅ 通过 | 任务历史：名称/类型/状态/触发原因/进度 `100%`/开始时间/完成时间/耗时 `2ms`/`28ms`/明细「扫描 286 个文件」全部有值 |
| `/#/admin/online` 在线 IP | ✅ 通过 | 表头完整（IP/在线时长/上次请求/首次请求/请求次数/User-Agent），空态「当前没有在线 IP」 |
| `/#/admin/api-keys` | ✅ 通过 | 3 个统计卡片（活跃/总计/已过期）与 9 列表头完整，空态「暂无数据」 |
| `/#/admin/uploads` 上传任务 | ✅ 通过 | 僵尸文件表 5 条记录：文件名/状态/文件大小 `14.84 MB`/已上传 `0 B (0%)`/创建时间/过期时间全部有值 |
| `/#/admin/resource` 资源监控 | ✅ 通过 | 硬件配置（CPU 16 核 / 内存 16.0 G / 磁盘 2 块 / 网卡 2 块）与实时指标全部有值：`已用 8.36 G / 总 16.00 G`、`读 ↓ 139 KB/s`、`写 ↑ 587 KB/s` |
| `/#/admin/behavior-analytics` 行为分析 | ✅ 通过 | 访问/上传/下载三块：总次数、独立用户、独立 IP、占比表全部有值（如访问 `7` 次、下载 `15` 次、体积 `380 MB`） |
| `/#/admin/security` 安全事件 | ✅ 通过 | 中危 `2857`、事件总数 `2857`、未读 `0`；事件表时间/级别/事件类型「认证失败(401)」/来源 IP `::1`/身份/目标/路径/状态全有值 |
| `/#/admin/logs` 日志 | ✅ 通过 | 日志目录 `D:\Projects\lightshare\server\logs`、级别过滤、`app.log · 200 行可见`、时间戳/级别/来源 `server/workers.go:66`/内容全有值 |
| `/#/admin/webhook` Webhook 推送 | ✅ 通过 | 全局参数表单正常渲染，「尚未配置回调地址」空态 |
| `/#/admin/mime` MIME 映射 | ✅ 通过 | 说明文案与「自定义条目（0）」表头（扩展名/MIME 类型/操作）正常，空态 |
| `/#/admin/settings` 设置 | ✅ 通过 | 共享目录表单（目录路径/显示名称/允许上传）正常渲染 |
| `/#/user/info` 个人信息 | ✅ 通过 | 用户类型「管理员」、用户名、用户 ID、注册时间 `2026-09-09 13:14:24` 有值；姓名/昵称为 `-`（库中为空，非错位） |

---

## 六、结论汇总

1. **`-` 的根因是运行旧后端，不是代码问题。** 迁移后代码与前端已对齐；重启后端后全部 17 个页面（含管理后台与个人中心）实测正常。
2. **代码层面未发现新的前后端字段不一致。** 前端读 snake 键 4 处命中均为扫描盲区误报（`file_url` 为下标赋值）；前端读 camel 键 117 处命中全部是组件内部 state。
3. **剩余显示 `-` 的三处是空数据，不是字段错位：**
   - 用户列表 / 个人信息 的「昵称」「姓名」—— 数据库该字段为空串
   - 私有存储「用量」—— 账号当前 0 字节，`formatSize(0)` 输出 `-`
4. **`bin/` 下的两个二进制是 11-11 迁移前的产物**，若用于部署需重新 `.\build.ps1`。

---

## 七、经验教训

1. **`formatSize(0)` 返回 `-`**：排查「表格显示 `-`」时先区分「取不到值」与「值本身为 0/空」，否则会误判成字段错位。
2. **旧进程陷阱**：`go run .` 留下的进程不会随代码修改自动重启，容易造成「改了代码却没生效」的假象。排查前后端契约问题时，第一步应确认运行进程的启动时间（`Get-CimInstance Win32_Process` 的 `CreationDate`）。
3. **静态扫描的两个盲区**：① 后端 `result["key"] = value` 下标赋值产生的动态 key；② 前端本地构造的对象字段（上传队列、目录行、设置差异项）。两者都必须人工排除，不能只看扫描命中数。
4. **空数据 vs 契约错位**：昵称、备注、用量这类可选字段为空时会渲染 `-`，与「字段名不匹配」表现一致，需结合接口原始响应或数据库值判定。
5. **管理端表格用 Element Plus Table V2**（`el-table-v2`，虚拟滚动，无原生 `<table>` 元素），用 `document.querySelectorAll('tr')` 取不到行，需读 `document.body.innerText` 或 `.el-table-v2__row`。
