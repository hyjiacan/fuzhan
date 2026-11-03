# 安全事件全渠道覆盖（FTP/WebDAV/DDoS/降噪）

## Context

现状核查结论：`管理 > 安全事件` 页面目前**无法准确发现所有途径的攻击行为**——

1. **FTP 完全不入库**：登录失败/暴破（`server/internal/ftp/handler.go` L166-228 内存 failCounts 仅写日志）、路径穿越/越权拒绝/匿名下载限流（`server/internal/access/ftp/virtual_fs.go`）均返回 `os.ErrPermission`，不写 `security_events`。
2. **WebDAV guard 错标**：401/403/429 经全局 AuditMiddleware 入库，但 `recordRejectedSecurityEvent` 硬编码 `guard=auth`；`models.GuardWebDAV/GuardFTP` 常量定义后从未赋值。
3. **无 DDoS/高频检测**：任何渠道都没有聚合速率告警。
4. **噪音淹没**：实测库中 2805 条全是 `auth.unauthorized`，大头是前端 token 过期的 GET 轮询 401（如 unread-summary 每2分钟）。

用户确认策略：**DDoS 仅检测告警不阻断**；**GET 轮询 401 不入库**。

## 已确认决策

- DDoS：新增检测器，命中阈值记 `ddos.suspected`(high) 事件，不拦截；配置节 `ddos:`（enabled 默认 false，未启用仅中文日志提示不启动协程）。
- 降噪：`isNonActivityRequest` 命中的 GET 401 不入库；写操作 401、登录/注册、WebDAV 401、403/429 全部保留。

## 实施步骤

### 1. models 常量扩展 — `server/internal/models/security_event.go`
- 事件类型新增 `EventDDoSSuspected = "ddos.suspected"`；guard 新增 `GuardDDoS = "ddos"`。

### 2. WebDAV guard 归一
- `server/internal/constants/context_keys.go`：新增 `ContextKeySecurityGuard ContextKey = "securityGuard"`。
- `server/internal/access/webdav/auth.go` + `routes.go`：三个 abort 分支（401 认证失败、429 锁定预检、429 下载限流、403 Depth 违规）abort 前 `c.Set(..., models.GuardWebDAV)`，抽 helper `markWebDAVGuard(c)`。
- `server/internal/middleware/audit.go` `recordRejectedSecurityEvent`：guard 改为「上下文标记优先，缺省 auth」。
- `server/internal/security/`（新文件 guard.go）：`GuardForScope(scope)`（webdav→GuardWebDAV、ftp→GuardFTP、其余→GuardAccessguard）与 `FailLockReason(scope)`（webdav 文案「WebDAV 认证失败达阈值，已对该 IP 锁定」）；`server/main.go` L316-327 failHook 改用二者，SCOPE_WEBDAV 的 operation 改 `auth.login`。

### 3. FTP 接入安全事件
- `server/internal/ftp/handler.go`：
  - 新增 `stripPort(addr)`（net.SplitHostPort），`AuthUser` 的 `clientIP` 归一为纯 IP（同时修复 `canManagePublic`/uploaderIp 带端口比对缺陷）。
  - 新增 helper `recordFTPSecurity(...)` → `security.Record(&models.SecurityEvent{Guard: GuardFTP, ...})`（无 HTTP 维度字段）。
  - 认证失败 → `auth.failed_login`(medium, operation=auth.login, target/identity=用户名)；频率超限 → `auth.locked`(high)，**仅跨越阈值那一次记录**（wasBelow 判定），与 Web 侧 login_limit.go 语义对齐。
- `server/internal/access/ftp/virtual_fs.go`：
  - 新增方法 `fs.recordSecurity(eventType, severity, operation, target, reason)`（identity=userUUID 或 anonymous，clientIP 用归一后的 fs.clientIP）。
  - `resolvePath` 增加 `op` 参数（约 12 个调用点机械改参：create/mkdir/open/openfile/remove/removeall/rename/chmod/chtimes/list/stat），反斜杠/`..`/绝对路径/未知命名空间拒绝 → `access.forbidden`(high, Reason 注明穿越类型)。
  - `canManagePublic` 拒绝点（Create/OpenFile/Remove/RemoveAll/Rename）→ `access.forbidden`(medium, "匿名或非归属用户尝试管理公开文件")。
  - Rename 跨命名空间 → `access.forbidden`(high)。
  - 匿名下载 `accessguard.Acquire(SCOPE_FTP)` 失败 → `rate.limited`(low)。
  - Stat/readDir 的 `..` 前置检查分支保持不入库（走 resolvePath 才记，避免重复）。

### 4. DDoS/高频检测器（仅告警）
- `server/internal/appconfig/config.go`：新增顶层节 `DDosConfig{Enabled, WindowSeconds(默认10), MaxRequests(默认200), CooldownMinutes(默认10)}` + `GetDDoSConfig()`（RLock 实时读，仿 GetDownloadRateLimit）。独立顶层 `ddos:` 节（不放 `security:` 下，避免 UI 增量保存整节覆盖丢字段）。
- `server/fuzhan.sample.yaml`：`download:` 节后补 `ddos:` 节（enabled: false + 中文注释）。
- `server/internal/security/ddos.go`（新）：仿 accessguard 的 map+mutex+清理 ticker；`InitDDoSDetector`（未启用仅 `utils.Info("DDoS 高频检测未启用")`）、`SyncDDoSDetector`（配置保存钩子启停）、`StopDDoSDetector`、`DDoSObserve(ip)`（固定窗口计数，超阈值且过冷却 → `ddos.suspected`(high, guard=ddos, operation=http.flood, Reason 含窗口/次数/阈值））。条目数硬上限 10000 保证内存有界。
- 挂点：`server/internal/middleware/monitoring.go` MonitoringMiddleware 取到 clientIP 后无条件 `security.DDoSObserve(clientIP)`（计全量请求）。FTP 不纳入（登录暴破已由步骤3覆盖）。
- `server/main.go`：InitWriter 后 `InitDDoSDetector()` + 注册保存钩子 `SyncDDoSDetector`；退出路径 `StopDDoSDetector()`。

### 5. 401 降噪
- `server/internal/middleware/monitoring.go` `isNonActivityRequest` 后缀清单补 `/security/events/unread-summary`。
- `server/internal/middleware/audit.go` `recordRejectedSecurityEvent`：switch 前短路——`status==401 && isNonActivityRequest(method,path)` 不入库。存量噪音不迁移清理（管理员标记已读消化）。

### 6. 前端 — `ui/src/views/AdminSecurityView.vue`
- `eventTypeMeta` 加 `'ddos.suspected': '疑似DDoS/高频'`；`guardMeta` 加 `ddos: '高频检测'`。
- `filters` 加 `guard: ''`，模板筛选区加「防护渠道」下拉（选项来自 guardMeta），`buildParams` 传 `guard`，重置含 guard。后端 `parseFilter` 已支持 guard 参数，无需改。

### 7. 测试
- 新 `server/internal/security/ddos_test.go`：fake store（嵌入 SecurityEventStore 接口+CreateBatch 收集）；未启用不产生事件/超阈值恰一条/冷却去重/窗口重置可再触发。配置改写用 `appconfig.LockConfig/UnlockConfig`（参照 webdav security_test.go L139-149）。
- 新 `server/internal/security/guard_test.go`：GuardForScope/FailLockReason 表驱动。
- 新 `server/internal/middleware/audit_test.go`：GET 轮询 401 不入库、POST 401 入库、403/429 入库、上下文标记 guard=webdav。
- 扩 `access/ftp/virtual_fs_test.go`：穿越/跨命名空间/越权/限流各断言事件维度与 ClientIP 无端口。
- 扩 `ftp/backend_test.go`：错密 failed_login、第5次恰一条 locked、第6次不再记。
- 扩 `access/webdav/security_test.go`：401 分支后断言上下文 guard 标记。

### 8. 文档同步（vitepress 审查约束）
- `docs/data-models.md`（SecurityEvent 节）：事件类型补 ddos.suspected，guard 补 webdav/ftp/ddos。
- `docs/api-contracts.md`：安全事件列表接口过滤参数补 guard。
- `docs/07-系统配置与选项模块.md`：新增 `ddos` 配置节说明（含「仅检测不阻断、未启用仅日志提示」语义）。
- `docs/FTP_WEBDAV_GUIDE.md`：补「安全事件覆盖」一节（各拒绝点入库口径）。

## 关键约束遵循
- 日志中文、`utils.Now()` UTC、gofmt -w（提交前 `gofmt -l server` 为空）、未启用功能仅日志提示、配置项需说明。
- 导入方向无环：security 不导入 accessguard（GuardForScope 用字符串字面量 switch）；access/ftp→security 无环。

## 验证
1. `cd server && go vet ./... && go test ./...`、`gofmt -l server` 为空。
2. `cd ui && npm run build`。
3. E2E（启动服务，配置 ddos.enabled=true）：
   - `curl -u bad:bad ftp://127.0.0.1:21/` ×6 → 库中 5 条 `auth.failed_login` + 1 条 `auth.locked`，guard=ftp，ClientIP 无端口。
   - `curl -u x:y http://127.0.0.1:PORT/api/v1/webdav/` → `auth.unauthorized` guard=webdav；连发达阈值 → `code.bruteforce` guard=webdav。
   - 高频压 `http://127.0.0.1:PORT/`（>200 req/10s）→ 一条 `ddos.suspected`(high, guard=ddos)，冷却内不重复。
   - token 过期停留管理页 2 个轮询周期 → `auth.unauthorized` 不再增长。
   - 前端安全事件页按「防护渠道=FTP/WebDAV/高频检测」筛选正确，新类型中文标签正常。
4. 文档与 sample.yaml 逐字段核对一致。

## 实施顺序
1 → (2 ∥ 3) → 4 → 5 → 6 → 7 → 8。
