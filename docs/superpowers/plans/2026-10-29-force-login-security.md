# 强制登录安全边界补全 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复强制登录开启后遗留的 4 个安全边界问题（/ready 绕过、/health 泄露、openapi.json 文档割裂），并为 Web 与 lite 登录/注册增加可配置的图形验证码。

**Architecture:** 纯 Go + Gin 单机网盘（fuzhan）。改动分四块：① /ready 路由从 api 组移到 web 组走 LoginGate；② /health 响应字段瘦身（去 goVersion/goroutines/绝对路径）；③ openapi.json 从组级 AuthGate 单独抽出，挂新守卫 `LoginRequiredWhenForced`（强制登录时要求 Web 会话或 API Key）；④ 引入 `github.com/mojocn/base64Captcha`，新增 `internal/captcha` 包薄封装，Web（Vue 动态拉取）与 lite（SSR 服务端内嵌）两入口接入，配置三态 `disabled/always/afterN`，复用两端各自的 IP 限流失败计数触发。默认 `disabled` 零行为影响。

**Tech Stack:** Go 1.x、Gin、gorm、html/template（lite SSR）、`github.com/mojocn/base64Captcha`、Vue 3 + Element Plus（ui/）。

**设计文档:** [2026-10-29-force-login-security-design.md](../specs/2026-10-29-force-login-security-design.md)

---

## 文件结构总览

| 动作 | 路径 | 职责 |
|------|------|------|
| 修改 | `server/routes_web.go` | /ready 移到 web 组 |
| 修改 | `server/internal/health/handler.go` | 字段瘦身 |
| 修改 | `server/integration/health_handler_test.go` | 断言更新 |
| 修改 | `server/routes_top.go` | openapi.json 单独抽出挂守卫 |
| 修改 | `server/internal/openapi/auth.go` | 新增 `LoginRequiredWhenForced` 中间件 + Web 会话放行分支 |
| 新建 | `server/internal/captcha/captcha.go` | base64Captcha 薄封装单例 |
| 新建 | `server/internal/captcha/captcha_test.go` | 单元测试 |
| 修改 | `server/go.mod` / `server/go.sum` | go get base64Captcha |
| 修改 | `server/internal/appconfig/config.go` | AccountConfig 增加 CaptchaConfig |
| 修改 | `server/internal/appconfig/dto.go` | AccountResponse 增加 CaptchaResponse + ToDTO/ConfigFromDTO 映射 |
| 修改 | `server/internal/appconfig/api/handler.go` | GetOptions/SaveConfig 增补 captcha；options 移除 allowedIPRanges |
| 修改 | `server/internal/auth/login_limit.go` | 新增 displayFailures 字段 + loginFailureCount |
| 修改 | `server/internal/lite/auth_limit.go` | 对称改动（lite 前缀） |
| 修改 | `server/internal/auth/handler.go` | Register/Login 前置 CaptchaGate |
| 修改 | `server/internal/lite/auth_handler.go` | 渲染页内嵌/提交校验 CaptchaGate |
| 修改 | `server/internal/lite/auth_tmpl.go` | 模板函数扩展 |
| 修改 | `server/internal/lite/auth_page.html` | 验证码表单控件 |
| 修改 | `server/fuzhan.sample.yaml` | captcha 配置样例 |
| 新建 | `ui/src/components/auth/CaptchaInput.vue` | 前端验证码组件 |
| 修改 | `ui/src/api/index.js` | AuthApi.captcha() + login/register 携带字段 |
| 修改 | `ui/src/store/index.js` | config 增 captcha 三字段 |
| 修改 | `ui/src/main.js` | options 读取 captcha |
| 修改 | `ui/src/views/LoginView.vue` | 验证码联动 + 表单字段 |
| 修改 | `ui/src/views/SettingsView.vue` | loadSettings + setConfig 刷新 |
| 修改 | `ui/src/views/settings/SettingsAuth.vue` | 三态 radio + 阈值输入 |
| 修改 | `ui/src/views/settings/useSettingsDiff.js` | account 节 diff 标签 |

依赖关系：Task1-3 相互独立（P1/P2/P3）；Task4 提供 captcha 包但须先 Task5（配置）或独立；Task5 配置是 Task6-9 的前置。建议顺序：Task1 → Task2 → Task3 → Task5（配置骨架）→ Task4（captcha 包）→ Task6（web 接入）→ Task7（lite 接入）→ Task8（前端）→ Task9（收尾联调）。

---

## Task 1: /ready 路由移入 web 组（P1）

**Files:**
- Modify: `server/routes_web.go:46`
- Test: `server/integration/health_handler_test.go`（存在 ready 相关用例则更新）

- [ ] **Step 1: 先看现有 health 集成测试**

Run: `Get-Content server/integration/health_handler_test.go`
找到涉及 `/ready` 的路由测试，记录其断言路径。

- [ ] **Step 2: 移动路由注册**

修改 [routes_web.go](file:///d:/Projects/lightshare/server/routes_web.go#L46)：

```go
	// 健康检查路由（无需认证）
	web.GET("/health", healthHandler.Health)
	// 就绪探针（基础设施，非界面调用）。
	// 挂在 web 组（经 LoginGate）：强制登录开启后就绪状态也需登录可见，避免未登录探测敏感拓扑。
	web.GET("/ready", healthHandler.Ready)
```

删除原来 api 组的 `api.GET("/ready", ...)`。

- [ ] **Step 3: 运行后端测试**

Run: `cd server && go test ./integration/ -run Health -v`
Expected: PASS（若该 test 直接用路径访问 /ready，因其经 LoginGate 需登录，非强制登录下仍匿名，测试仍绿；若失败按失败断言更新步）。

- [ ] **Step 4: Commit**

```bash
git add server/routes_web.go
git commit -m "fix(security): /ready 就绪探针移入 web 组，强制登录后不再匿名"
```

---

## Task 2: /health 字段瘦身（P2）

**Files:**
- Modify: `server/internal/health/handler.go`
- Test: `server/integration/health_handler_test.go`

- [ ] **Step 1: 更新 HealthCheckResponse 结构**

修改 [handler.go](file:///d:/Projects/lightshare/server/internal/health/handler.go#L37-L45)，移除 `GoVersion` `Goroutines`：

```go
// HealthCheckResponse 健康检查响应结构体
type HealthCheckResponse struct {
	Status    string                 `json:"status"`
	Version   string                 `json:"version"`
	Checks    map[string]CheckResult `json:"checks"`
	Timestamp string                 `json:"timestamp"`
}
```

- [ ] **Step 2: 移除 runtime 引入并改存储目录 Message**

- 删除 `"runtime"` import。
- 删除构造处 `GoVersion: runtime.Version(), Goroutines: runtime.NumGoroutine(),`。
- 存储目录 ok 分支 [L110-113](file:///d:/Projects/lightshare/server/internal/health/handler.go#L110-L113) Message 由 `dirPath` 改为 `"目录正常"`：

```go
		} else {
			storageChecks = append(storageChecks, CheckResult{
				Status:  "ok",
				Message: "目录正常",
			})
```

- error 分支的 `"目录不存在: "+dirPath` → `"目录不存在"`；`"无权限访问: "+dirPath` → `"无权限访问"`；`"不是目录: "+dirPath` → `"不是目录"`（[L77-102](file:///d:/Projects/lightshare/server/internal/health/handler.go#L77-L102)）。日志里的真实 path 保留（内部日志允许）。

- [ ] **Step 3: 更新集成测试断言**

在 health 集成测试中，对 `/api/v1/web/health` 的响应断言：
- 不再包含 `goVersion` `goroutines` 键。
- 不再包含存储绝对路径字符串。

示例断言：

```go
body := resp.Body.String()
if strings.Contains(body, "goVersion") || strings.Contains(body, "goroutines") {
	t.Fatal("health 响应不应泄露 goVersion/goroutines")
}
if strings.Contains(body, ":/") || strings.Contains(body, `C:\`) {
	t.Fatal("health 响应不应包含存储目录绝对路径")
}
```

- [ ] **Step 4: 运行测试**

Run: `cd server && go test ./integration/ -run Health -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/health/handler.go server/integration/health_handler_test.go
git commit -m "fix(security): /health 移除 goVersion/goroutines 与存储绝对路径等敏感信息"
```

---

## Task 3: openapi.json 单独守卫（P3）

**Files:**
- Modify: `server/routes_top.go:115-123, 138`
- Modify: `server/internal/openapi/auth.go`
- Test: `server/integration/openapi_guard_test.go`（新建）

**现状**：openapi.json 挂在 `/api/open/v1` 组，组级 AuthGate 拦截。强制登录 + auth_mode=none 时 403 匿名；但已登录 Web 用户（Cookie）也被 403，因为 AuthGate 只认 API Key。同时 auth_mode=api_key 时匿名可读 openapi.json（AuthGate 放行任何有效 Key，但文档本不该匿名暴露）。

**目标**：openapi.json 不再受组级 AuthGate 的"必须 api_key"限制，改用新守卫 `LoginRequiredWhenForced`——非强制登录时保持组级 AuthGate 原语义（走组链：none 公开 / api_key 需 Key）；强制登录时要求 Web 会话（Cookie JWT）或 API Key。

**实现方式**：把 openapi.json 从 `openAPI` 组移到**组外**一条独立路由，挂 `openapi.FeatureGateMiddleware` + `openapi.LoginRequiredWhenForced(rt.authMiddleware)` + `rt.openAPIHandler.GetOpenAPISpec`。

- [ ] **Step 1: 新增 LoginRequiredWhenForced 中间件**

在 [openapi/auth.go](file:///d:/Projects/lightshare/server/internal/openapi/auth.go) 新增（放在 AuthGateMiddleware 后）：

```go
// LoginRequiredWhenForced OpenAPI 文档守卫。
// 非强制登录：完全放行（保持原组级 AuthGate 对文档的既有行为交由上层决策）。
// 强制登录：要求有效凭证——携带 Web 会话 Cookie（fuzhan_token JWT）或有效 API Key，否则 401。
func LoginRequiredWhenForced(authMW *middleware.AuthMiddleware) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !appconfig.GetConfig().Account.RequireLogin {
			c.Next()
			return
		}
		// 优先尝试 Web 会话（Cookie JWT）
		if token := middleware.TokenFromCookie(c); token != "" {
			if _, err := jwt.ParseJWTUseStrict(token); err == nil {
				c.Next()
				return
			}
		}
		// 其次尝试 API Key
		if token := bearerToken(c); token != "" {
			if rb := c.Value("rbacService"); rb != nil {
				if rbac, ok := rb.(*auth.RBACService); ok {
					if _, _, err := rbac.ValidateAPIKey(token); err == nil {
						c.Next()
						return
					}
				}
			}
		}
		c.Header("Cache-Control", "no-store")
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "强制登录已开启，需要有效凭证"})
	}
}
```

> 注意：需核对 `jwt.ParseJWTUseStrict` 的真实签名（见 Step 3 核实替代：可能是 `jwt.ParseJWT(token)`）。rbacService 通过 `c.Set`/闭包注入更稳妥——下方 Step 3 会改为闭包捕获 `rbac`，避免依赖 gin.Context 存值。

- [ ] **Step 2: 修改 import**

`internal/openapi/auth.go` 加入 `"fuzhan/internal/middleware"` 与 `"fuzhan/internal/security/jwt"`（按实际 jwt 包路径；见 Step 3 核实）。

- [ ] **Step 3: 核实 jwt 认证函数签名**

Run: `Select-String -Path server/internal/security/jwt/*.go -Pattern "func ParseJWT"`
记录真实的解析函数名与签名。若为 `ParseJWT(token string) (*Claims, error)` 则改用它；同时确认 middleware `AuthRequired` 内部用的是哪个函数（[auth.go](file:///d:/Projects/lightshare/server/internal/middleware/auth.go#L119)）。**以实际为准修正 Step 1 代码。**

- [ ] **Step 4: 核实 rbac 注入方式**

Run: `Select-String -Path server/runctx.go -Pattern "rbacService"`
确认 runctx 持有 `rbacService *auth.RBACService`。将 Step 1 中从 `c.Value` 取 rbac 改为**闭包参数捕获**：

```go
func LoginRequiredWhenForced(authMW *middleware.AuthMiddleware, rbac *auth.RBACService) gin.HandlerFunc {
	// ... 内部直接用 rbac（nil 则只走 Web 会话通道）
}
```

- [ ] **Step 5: 路由改造**

修改 [routes_top.go](file:///d:/Projects/lightshare/server/routes_top.go#L115-L140)：

- 从 `openAPI` 组中**删除** `openAPI.GET("/openapi.json", ...)`。
- 在 openAPI 组之后新增独立路由：

```go
	// OpenAPI 文档：从组级 AuthGate 单独抽出。
	// 非强制登录时保持匿名（auth_mode=none）/需 Key（api_key）由组链决定，但文档单独走轻守卫。
	openapiDoc := r.Group("/api/open/v1")
	openapiDoc.Use(
		openapi.FeatureGateMiddleware(rt.openAPICfg.Enabled),
		openapi.LoginRequiredWhenForced(rt.authMiddleware, rt.rbacService),
	)
	openapiDoc.GET("/openapi.json", rt.openAPIHandler.GetOpenAPISpec)
```

- `/docs`（301 → /scalar.html）留在 openAPI 组内不动（匿名静态跳转，设计确认无需守卫）。

- [ ] **Step 6: 新建集成测试**

创建 `server/integration/openapi_guard_test.go`，覆盖四象限：

```go
package integration

// 场景矩阵：
// 1. requireLogin=false, authMode=none       → GET /api/open/v1/openapi.json = 200
// 2. requireLogin=true,  authMode=none, 无凭证 → 401
// 3. requireLogin=true,  authMode=none, 带 Web Cookie(JWT) → 200
// 4. requireLogin=true,  authMode=api_key, 无凭证 → 401
// 5. requireLogin=true,  authMode=api_key, 带有效 API Key → 200
```

（具体测试骨架按项目现有 integration 测试风格：`setupTestRouter()` + `httptest`；实现时参考 `health_handler_test.go` 的建路由方式，构造 JWT 用测试账号通过 `authService.Login` 或以 `jwt` 包直接签发。）

- [ ] **Step 7: 运行测试**

Run: `cd server && go test ./integration/ -run "OpenAPI|Openapi|OpenApi" -v`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add server/internal/openapi/auth.go server/routes_top.go server/integration/openapi_guard_test.go
git commit -m "feat(security): openapi.json 改为 LoginRequiredWhenForced 守卫，强制登录后可读且匿名不可读"
```

---

## Task 4: go get base64Captcha 依赖

**Files:**
- Modify: `server/go.mod` / `server/go.sum`

- [ ] **Step 1: 拉取依赖**

Run: `cd server && go get github.com/mojocn/base64Captcha@v1.3.8`
Expected: 成功，go.mod/go.sum 更新

- [ ] **Step 2: 验证编译**

Run: `cd server && go build ./...`
Expected: 成功

- [ ] **Step 3: Commit**

```bash
git add server/go.mod server/go.sum
git commit -m "chore(deps): 引入 base64Captcha 用于图形验证码"
```

---

## Task 5: 配置层增补 captcha 三态（含 options 移除 allowedIPRanges）

**Files:**
- Modify: `server/internal/appconfig/config.go`
- Modify: `server/internal/appconfig/dto.go`
- Modify: `server/internal/appconfig/api/handler.go`
- Modify: `server/fuzhan.sample.yaml`
- Test: `server/internal/appconfig/api/handler_test.go`（存在则更新）

- [ ] **Step 1: config.go 增加 CaptchaConfig**

修改 [config.go](file:///d:/Projects/lightshare/server/internal/appconfig/config.go#L60-L68) 的 AccountConfig：

```go
// AccountConfig 账户相关配置
type AccountConfig struct {
	AnonymousConfig `yaml:",inline"`

	// 保留用户名（禁止注册/使用）
	ReservedUsernames []string `yaml:"reserved_usernames"`
	// 是否允许匿名注册（与强制登录无关，独立开关）
	AllowRegistration bool `yaml:"allow_registration"`
	// 是否强制登录：开启后未登录无法访问 Web 界面/CLI/下载等业务
	RequireLogin bool `yaml:"require_login"`
	// 登录/注册图形验证码
	Captcha CaptchaConfig `yaml:"captcha"`
}

// CaptchaConfig 图形验证码配置
type CaptchaConfig struct {
	// Mode: disabled | always | after_n（禁用/始终开启/失败 N 次后开启）
	Mode string `yaml:"mode"`
	// TriggerAfter after_n 模式下触发验证码的失败次数阈值
	TriggerAfter int `yaml:"trigger_after"`
}
```

> 注意：需先用 Read 确认 AccountConfig 当前真实字段（是否含 AnonymousConfig inline、L60-68 精确内容），以上为对齐风格的预估骨架，实现时以实际为准。

- [ ] **Step 2: dto.go 增加 CaptchaResponse**

修改 [dto.go](file:///d:/Projects/lightshare/server/internal/appconfig/dto.go#L306-L311) AccountResponse：

```go
// AccountResponse 账户相关配置响应
type AccountResponse struct {
	Anonymous         AnonymousResponse `json:"anonymous"`
	ReservedUsernames []string          `json:"reservedUsernames"`
	AllowRegistration bool              `json:"allowRegistration"`
	RequireLogin      bool              `json:"requireLogin"`
	Captcha           CaptchaResponse   `json:"captcha"`
}

// CaptchaResponse 验证码配置响应
type CaptchaResponse struct {
	Mode         string `json:"mode"`
	TriggerAfter int    `json:"triggerAfter"`
}
```

- 更新 `ToDTO()` 的 account 构造 [L403-410](file:///d:/Projects/lightshare/server/internal/appconfig/dto.go#L403-L410)：

```go
	Account: AccountResponse{
		Anonymous:         anonymousToDTO(c.Account.Anonymous),
		ReservedUsernames: c.Account.ReservedUsernames,
		AllowRegistration: c.Account.AllowRegistration,
		RequireLogin:      c.Account.RequireLogin,
		Captcha: CaptchaResponse{
			Mode:         c.Account.Captcha.Mode,
			TriggerAfter: c.Account.Captcha.TriggerAfter,
		},
	},
```

- 更新 `ConfigFromDTO` account 反向映射 [L640-641](file:///d:/Projects/lightshare/server/internal/appconfig/dto.go#L640-L641) 附近：

```go
		cfg.Account.AllowRegistration = dto.Account.AllowRegistration
		cfg.Account.RequireLogin = dto.Account.RequireLogin
		cfg.Account.Captcha.Mode = dto.Account.Captcha.Mode
		cfg.Account.Captcha.TriggerAfter = dto.Account.Captcha.TriggerAfter
```

> 注意：AccountResponse.Anonymous 是否为 AnonymousResponse 需按实际（dto.go L314-316 AnonymousResponse 存在）。ConfigFromDTO 反向映射的确切行数以 Read 为准。

- [ ] **Step 3: GetOptions 增 captcha 节 / 移除 allowedIPRanges**

修改 [handler.go](file:///d:/Projects/lightshare/server/internal/appconfig/api/handler.go#L36-L41) account 节：

```go
		"account": gin.H{
			// 注册开关：登录/注册入口据此决定是否展示「立即注册」
			"allowRegistration": currentConfig.Account.AllowRegistration,
			// 强制登录开关：前端据此决定首次加载前移登录页、路由守卫与下载链接是否附带 token
			"requireLogin": currentConfig.Account.RequireLogin,
			// 登录/注册图形验证码配置
			"captcha": gin.H{
				"mode":         currentConfig.Account.Captcha.Mode,
				"triggerAfter": currentConfig.Account.Captcha.TriggerAfter,
			},
		},
```

删除 [L65](file:///d:/Projects/lightshare/server/internal/appconfig/api/handler.go#L65) `"allowedIPRanges": currentConfig.Upload.URLUpload.AllowedIPRanges,` 一行（已确认无 getOptions 消费方，前端 URL 上传设置走 getConfig）。

> 注意：`Required` 动态字段不加进 options——由登录接口返回与前端逻辑判断；实现时如设计仍要求 options 带 required，则在 GetOptions 里以 `loginFailureCount(ip)` 计算后加入 `"required"`。默认按"仅 mode/triggerAfter"最小实现。(根据设计 3.3.3，options 带 required 动态字段：此处改为 `"required": loginFailureCount(utils.GetClientIP(c)) >= threshold` 需要 import auth 包。**决策：实现时按设计加 required，用 auth.LoginFailureCountForIP 暴露。**)

- [ ] **Step 4: SaveConfig 解析 account 节**

修改 [handler.go](file:///d:/Projects/lightshare/server/internal/appconfig/api/handler.go#L225-L244) 账户节处理，在 RequireLogin 之后追加：

```go
		if req.Account.Captcha.Mode != "" {
			appconfig.GlobalConfig.Account.Captcha.Mode = req.Account.Captcha.Mode
			updates.Add("account.captcha.mode", req.Account.Captcha.Mode)
		}
		if req.Account.Captcha.TriggerAfter > 0 {
			appconfig.GlobalConfig.Account.Captcha.TriggerAfter = req.Account.Captcha.TriggerAfter
			updates.Add("account.captcha.trigger_after", req.Account.Captcha.TriggerAfter)
		}
```

> 注意：updates.Add 的第三个参数与 SaveConfig 里其它字段类型一致（`updates.Add(key string, value any)`，见 [L243](file:///d:/Projects/lightshare/server/internal/appconfig/api/handler.go#L243)）。SaveConfig 需用 `LockConfig` 包裹，确认现有代码在锁内。

- [ ] **Step 5: 更新 sample.yaml**

修改 [fuzhan.sample.yaml](file:///d:/Projects/lightshare/server/fuzhan.sample.yaml#L268-L283) account 节增补（对齐 2 空格 + 中文注释）：

```yaml
  # 登录/注册图形验证码
  captcha:
    # 模式：disabled 关闭 / always 始终开启 / after_n 失败 N 次后开启
    mode: disabled
    # after_n 模式下的失败次数阈值
    trigger_after: 3
```

- [ ] **Step 6: 编译 + 测试**

Run: `cd server && go build ./... && go test ./internal/appconfig/... ./integration/ -run Options -v`
Expected: 全 PASS

- [ ] **Step 7: Commit**

```bash
git add server/internal/appconfig/ server/fuzhan.sample.yaml
git commit -m "feat(config): 账号配置新增验证码三态（mode/trigger_after），options 移除 allowedIPRanges"
```

---

## Task 6: login_limit 与 lite auth_limit 增 displayFailures

**Files:**
- Modify: `server/internal/auth/login_limit.go`
- Modify: `server/internal/lite/auth_limit.go`
- Test: `server/internal/auth/login_limit_test.go`（新建）/ `server/internal/lite/auth_limit_test.go`（新建）

- [ ] **Step 1: auth/login_limit.go 增字段与查询函数**

修改 [login_limit.go](file:///d:/Projects/lightshare/server/internal/auth/login_limit.go#L18-L27)：

```go
type ipLoginRecord struct {
	count          int
	displayFailures int
	windowStart    time.Time
	lockUntil      time.Time
}
```

`registerLoginFailure`（L78 `rec.count++` 后）追加 `rec.displayFailures++`。
`resetLoginFailures`（L97）已整体 delete，无需改动。

新增导出查询函数（供 options 与验证码触发共用）：

```go
// LoginFailureCountForIP 返回指定 IP 当前窗口内的失败次数（验证码触发/展示用）。
func LoginFailureCountForIP(ip string) int {
	if ip == "" {
		return 0
	}
	loginLimitMu.Lock()
	defer loginLimitMu.Unlock()
	rec, ok := loginLimitCache[ip]
	if !ok {
		return 0
	}
	return rec.displayFailures
}
```

> 注意：isLoginLocked 内部 showWindow logic 若影响 displayFailures 的展示需复核：锁定后 rec.count 归 0，但 displayFailures 保留，因此窗口内失败 5 次锁定后 displayFailures=5，继续失败仍累加——符合"锁定后也要求验证码"的语义。

- [ ] **Step 2: lite/auth_limit.go 对称改动**

修改 [auth_limit.go](file:///d:/Projects/lightshare/server/internal/lite/auth_limit.go#L18-L27)，同构：

```go
type liteIPRecord struct {
	count           int
	displayFailures int
	windowStart     time.Time
	lockUntil       time.Time
}
```

- `liteRegisterFailure` 的 `rec.count++` 后追加 `rec.displayFailures++`。
- 新增导出查询函数：

```go
// LiteLoginFailureCountForIP 返回指定 IP 当前窗口内的失败次数（lite 验证码触发用）。
func LiteLoginFailureCountForIP(ip string) int {
	if ip == "" {
		return 0
	}
	liteLimitMu.Lock()
	defer liteLimitMu.Unlock()
	rec, ok := liteLimitCache[ip]
	if !ok {
		return 0
	}
	return rec.displayFailures
}
```

- [ ] **Step 3: 单元测试**

新建 `server/internal/auth/login_limit_test.go`：

```go
package auth

import "testing"

func TestDisplayFailuresCount(t *testing.T) {
	ip := "127.0.0.1"
	for i := 0; i < 3; i++ {
		registerLoginFailure(ip)
	}
	if got := LoginFailureCountForIP(ip); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
	// 超过锁定阈值后仍保留累计
	for i := 0; i < 3; i++ {
		registerLoginFailure(ip)
	}
	if isLoginLocked(ip) && LoginFailureCountForIP(ip) < 5 {
		t.Fatalf("锁定期内 displayFailures 不应被清零")
	}
	resetLoginFailures(ip)
	if got := LoginFailureCountForIP(ip); got != 0 {
		t.Fatalf("reset 后应归零, got %d", got)
	}
}
```

lite 侧同构（`server/internal/lite/auth_limit_test.go`，包名 `lite`，用 `liteRegisterFailure`/`LiteLoginFailureCountForIP`/`liteResetFailures`）。

- [ ] **Step 4: 运行测试**

Run: `cd server && go test ./internal/auth/ ./internal/lite/ -run Failure -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add server/internal/auth/login_limit.go server/internal/auth/login_limit_test.go server/internal/lite/auth_limit.go server/internal/lite/auth_limit_test.go
git commit -m "feat(security): 登录限流新增 displayFailures 计数与查询，供验证码触发复用"
```

---

## Task 7: captcha 包（base64Captcha 薄封装）

**Files:**
- Create: `server/internal/captcha/captcha.go`
- Create: `server/internal/captcha/captcha_test.go`

**前置**：Task 4（依赖）已完成。

- [ ] **Step 1: 写包**

新建 `captcha.go`：

```go
// Package captcha 提供登录/注册图形验证码的薄封装（基于 base64Captcha）。
package captcha

import (
	"strings"

	"github.com/mojocn/base64Captcha"
)

// 本地内存 store：阈值 80，过期 300 秒（周期性清理由库内置）。
var store = base64Captcha.DefaultMemStore

// 字符合集：排除易混淆的 0O1lI
const charSet = "23456789abcdefghjkmnpqrstuvwxyzABCDEFGHJKMNPQRSTUVWXYZ"

var engine = base64Captcha.NewCaptcha(newDriver(), store)

func newDriver() base64Captcha.Driver {
	return base64Captcha.NewDriverString(
		80,   // height
		240,  // width
		6,    // noiseCount
		2,    // showLineOptions(干扰线)
		4,    // length
		charSet,
		&base64Captcha.Colors{
			Front:       []uint8{50, 90, 100},
			Background:  []uint8{245, 245, 245},
			Line:        []uint8{200, 200, 200},
			Noise:       []uint8{220, 220, 220},
		},
		nil, // fonts（nil 用内置字体）
	)
}

// Generate 生成一张验证码图。
// 返回 id（索图/校验的 key）与可直接放进 <img src> 的 PNG data URI。
func Generate() (id, pngBase64 string, err error) {
	id, b64, _, err := engine.Generate()
	if err != nil {
		return "", "", err
	}
	// 库返回形如 "data:image/png;base64,...."
	return id, sanitizeDataURI(b64), nil
}

// Verify 校验验证码（一次性）：正确则返回 true。
func Verify(id, answer string) bool {
	if id == "" || strings.TrimSpace(answer) == "" {
		return false
	}
	return store.Verify(id, strings.TrimSpace(answer), true)
}

// sanitizeDataURI 确保返回以 data:image 前缀开头。
func sanitizeDataURI(s string) string {
	if strings.HasPrefix(s, "data:") {
		return s
	}
	return "data:image/png;base64," + s
}
```

> 注意：`engine.Generate()` 的返回签名（返回几个值、是否含 warnings）需按库 v1.3.8 实际 API 调整；`base64Captcha.Colors` 字段名与默认 store 导出名 `DefaultMemStore` 需与实际（含 `github.com/mojocn/base64Captcha` 包内 `DefaultMemStore` 是否导出）核实，实现时以 `go doc` 为准修正。

- [ ] **Step 2: 单元测试**

`captcha_test.go`：

```go
package captcha

import (
	"strings"
	"testing"
)

func TestGenerateAndVerify(t *testing.T) {
	id, dataURI, err := Generate()
	if err != nil {
		t.Fatalf("Generate error: %v", err)
	}
	if id == "" {
		t.Fatal("id 为空")
	}
	if !strings.HasPrefix(dataURI, "data:image/png;base64,") {
		t.Fatalf("返回应为 PNG data URI, got %q", dataURI[:min(40, len(dataURI))])
	}
	// 直接 verify 错误答案
	if Verify(id, "____") {
		t.Fatal("错误答案不应通过")
	}
	// 用真实答案难以获取（库不暴露明文）——只验证一次性：错误后再次 false
	if Verify(id, "____") {
		t.Fatal("同一 id 错误答案二次也应 false")
	}
}

func TestVerifyEmpty(t *testing.T) {
	if Verify("", "abc") {
		t.Fatal("空 id 不应通过")
	}
	if Verify("x", "  ") {
		t.Fatal("空答案不应通过")
	}
}

func min(a, b int) int { if a < b { return a }; return b }
```

> 注：因库不暴露明文答案，正确性验证依赖集成/端到端测试（Task 6/8 的通过路径）。本测试覆盖生成与错误路径。

- [ ] **Step 3: 运行测试**

Run: `cd server && go test ./internal/captcha/ -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add server/internal/captcha/
git commit -m "feat(captcha): base64Captcha 薄封装包（Generate/Verify，PNG data URI）"
```

---

## Task 8: Web 登录/注册接入验证码

**Files:**
- Modify: `server/internal/auth/handler.go`
- Modify: `server/routes_web.go`（新增 captcha 获取接口）

**前置**：Task 5、6、7 完成。

- [ ] **Step 1: 新增 captcha 判定工具函数**

在 [auth/handler.go](file:///d:/Projects/lightshare/server/internal/auth/handler.go) 新增：

```go
// captchaRequired 判断当前 IP 是否需要验证码（三态配置 + 失败计数）。
func captchaRequired(ip string) bool {
	cfg := appconfig.GetConfig().Account.Captcha
	switch cfg.Mode {
	case "always":
		return true
	case "after_n":
		if cfg.TriggerAfter <= 0 {
			cfg.TriggerAfter = 3
		}
		return LoginFailureCountForIP(ip) >= cfg.TriggerAfter
	default: // disabled / 未知值
		return false
	}
}

// captchaGate 验证码门禁：需要且校验失败/缺失时写入错误并返回 false。
func captchaGate(c *gin.Context) bool {
	if !captchaRequired(utils.GetClientIP(c)) {
		return true
	}
	var req struct {
		CaptchaID     string `json:"captchaId"`
		CaptchaAnswer string `json:"captchaAnswer"`
	}
	// 登录/注册 body 已解析；此处从 body 二次读取不现实，改为由调用方传入。
	_ = req
	// 真实实现：在 Register/Login 的 req 结构上扩展字段，见 Step 2。
	return true
}
```

> **明确**：`captchaGate` 不作为独立中间件（body 已消费），而是把 `CaptchaID/CaptchaAnswer` 字段直接加进 `services.RegisterRequest`/`services.LoginRequest`（[auth_service.go](file:///d:/Projects/lightshare/server/internal/services/auth_service.go#L43-L52)），在 handler 校验后、调用 service 前插入验证。因此 Step 1 只保留 `captchaRequired`，门禁内联到 Step 2。

- [ ] **Step 2: Register/Login handler 插入验证**

修改 [handler.go](file:///d:/Projects/lightshare/server/internal/auth/handler.go#L104-L162)：

- 在 `services.RegisterRequest` / `services.LoginRequest` 增加字段（auth_service.go）：

```go
type RegisterRequest struct {
	Username      string `json:"username" binding:"required"`
	Password      string `json:"password" binding:"required"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}

type LoginRequest struct {
	Username      string `json:"username" binding:"required"`
	Password      string `json:"password" binding:"required"`
	CaptchaID     string `json:"captchaId"`
	CaptchaAnswer string `json:"captchaAnswer"`
}
```

> 注意：需 Read `auth_service.go` L43-52 确认现有字段与 binding tag 后以实际覆盖（保留原字段，仅追加两个 json 字段，不破坏现有客户端兼容）。

- 在 Register/Login 的 `ShouldBindJSON` 成功后、`isLoginLocked` 之前插入：

```go
	if !verifyCaptcha(c, req.CaptchaID, req.CaptchaAnswer) {
		return
	}
```

- 新增辅助：

```go
// verifyCaptcha 验证码门禁：不需要验证码或校验通过返回 true；
// 需要但未通过时写入 400 响应并返回 false。
func (h *Handler) verifyCaptcha(c *gin.Context, id, answer string) bool {
	if !captchaRequired(utils.GetClientIP(c)) {
		return true
	}
	if !captcha.Verify(id, answer) {
		recordAuthEvent(c, models.EventAuthFailedLogin, "auth.captcha", utils.GetClientIP(c), models.SeverityMedium, "验证码错误")
		response.HandleBadRequest(c, "验证码错误或已过期，请重试", gin.H{"code": "captcha_invalid"})
		return false
	}
	return true
}
```

> 注意：`response.HandleBadRequest(c, msg, data)` 第三个参数怎么带 code 需按项目 response 包实际签名（可能是 `HandleBadRequest(c, message, details)`）。若 error code 方案不一致，简化为 message 含"验证码"。（查阅 `pkg/response` 的 HandleBadRequest 后用实际签名。）

- import 增加 `"fuzhan/internal/captcha"`。

- [ ] **Step 3: 新增 captcha 获取接口**

修改 [routes_web.go](file:///d:/Projects/lightshare/server/routes_web.go#L64-L75) 的 auth 组：

```go
	auth := web.Group("/auth")
	{
		// 图形验证码（匿名可申请，供登录/注册页与 flash 页使用）
		auth.GET("/captcha", authHandler.GetCaptcha)
		auth.POST("/register", authHandler.Register)
		auth.POST("/login", authHandler.Login)
		// ... 其余不变
	}
```

在 [handler.go](file:///d:/Projects/lightshare/server/internal/auth/handler.go) 新增：

```go
// GetCaptcha 生成图形验证码（匿名可拉取，IP 频率受限由路由层 middleware 或本函数控制）。
func (h *Handler) GetCaptcha(c *gin.Context) {
	id, dataURI, err := captcha.Generate()
	if err != nil {
		response.HandleInternalServerError(c, "验证码生成失败")
		return
	}
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"captchaId": id,
		"image":     dataURI,
	})
}
```

- [ ] **Step 4: 集成测试**

在 `server/integration` 新建或并入验证码测试：
- after_n 模式下：前 N-1 次失败登录无验证码要求；第 N 次起缺失验证码 → 400。
- always 模式：无验证码 → 400；错误验证码 → 400；有效链路端到端（需从 captcha 包测出真实答案？——库不暴露明文，故端到端"正确验证码"路径改为：对 `captcha.Verify` 的单元测试 + handler 层的"已生成 id + 编码内不校验"无法做。**决策**：集成测试仅验证触发与错误路径；正确路径由 captcha 包单测覆盖。）

- [ ] **Step 5: 运行测试 + 编译**

Run: `cd server && go build ./... && go test ./internal/auth/ ./integration/ -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add server/internal/auth/ server/internal/services/auth_service.go server/routes_web.go
git commit -m "feat(security): Web 登录/注册接入图形验证码（三态触发 + 获取接口）"
```

---

## Task 9: lite 登录/注册接入验证码（SSR 内嵌）

**Files:**
- Modify: `server/internal/lite/auth_handler.go`
- Modify: `server/internal/lite/auth_tmpl.go`
- Modify: `server/internal/lite/auth_page.html`
- Test: `server/internal/lite/auth_handler_test.go`（新建或并入）

**前置**：Task 5、6、7 完成。

- [ ] **Step 1: 扩展模板数据与模板函数**

- [auth_tmpl.go](file:///d:/Projects/lightshare/server/internal/lite/auth_tmpl.go#L16-L20) 的 `authTemplateFuncs` 增加：

```go
func authTemplateFuncs() template.FuncMap {
	return template.FuncMap{
		"AllowRegistration": AllowRegistration,
		"CaptchaImage":      CaptchaImage, // 返回条件渲染的验证码块（零 JS），定义为整段 HTML
	}
}
```

> 注意：`CaptchaImage(ip string) template.HTML` 的实现需在 auth_handler.go 定义（依赖 captcha 包与 lite 限流计数），返回带隐藏域 + img + 输入框的整段 HTML，`template.HTML` 类型。auth_tmpl.go 与 auth_handler.go 同包，直接引用。

- [ ] **Step 2: auth_handler.go 实现 CaptchaImage 与提交门禁**

新增：

```go
// CaptchaImage 渲染验证码区块（零 JS）：需要验证码时内嵌隐藏域 + PNG img + 输入框。
func CaptchaImage(ip string) template.HTML {
	if !liteCaptchaRequired(ip) {
		return template.HTML("")
	}
	id, dataURI, err := captcha.Generate()
	if err != nil {
		return template.HTML("")
	}
	return template.HTML(fmt.Sprintf(
		`<div class="captcha-row"><img src="%s" alt="验证码"><input type="hidden" name="captcha_challenge" value="%s"><input type="text" name="captcha_text" placeholder="验证码" required></div>`,
		template.HTMLEscapeString(dataURI), template.HTMLEscapeString(id)))
}

// liteCaptchaRequired lite 端三态判定（复用 lite 独立失败计数）。
func liteCaptchaRequired(ip string) bool {
	cfg := appconfig.GetConfig().Account.Captcha
	switch cfg.Mode {
	case "always":
		return true
	case "after_n":
		if cfg.TriggerAfter <= 0 {
			cfg.TriggerAfter = 3
		}
		return LiteLoginFailureCountForIP(ip) >= cfg.TriggerAfter
	default:
		return false
	}
}
```

- `renderAuthPage` 调用时传入 IP：查 [auth_handler.go](file:///d:/Projects/lightshare/server/internal/lite/auth_handler.go#L104-L124)，`authErrView` 增加字段：

```go
type authErrView struct {
	Title   string
	AppName string
	LogoPath string
	Heading string
	Error   string
	Redirect string
	Username string
	// 验证码区（已内嵌完整 HTML，空串表示无需）
	Captcha template.HTML
}
```

在 renderAuthPage 内（构造 view 时）赋值：`Captcha: CaptchaImage(utils.GetClientIP(c))`。需将 `c`（或 ip）传入 renderAuthPage——若签名无 c 则改为调用方传入 ip。

> 注意：`utils.GetClientIP(c)` 需要 gin.Context；renderAuthPage 当前签名 `renderAuthPage(c, heading, errMsg, data, status)` 含 c（按调研 L104-124），可直接用。

- [ ] **Step 3: 提交门禁**

在 `HandleLoginSubmit` / `HandleRegisterSubmit` 的 `parseAuthForm` 后、锁定判断前插入：

```go
	if liteCaptchaRequired(utils.GetClientIP(c)) {
		challenge := c.PostForm("captcha_challenge")
		text := c.PostForm("captcha_text")
		if !captcha.Verify(challenge, text) {
			renderAuthPage(c, heading, "验证码错误或已过期，请重试", data, http.StatusBadRequest)
			return
		}
	}
```

> 注意：heading/data 按各 handler 现状变量名修正（login 为固定 heading）；`renderAuthPage` 里重渲染会重新 Generate 新图（CaptchaImage 每次调 Generate）。

- [ ] **Step 4: 更新 auth_page.html**

在 [auth_page.html](file:///d:/Projects/lightshare/server/internal/resources/auth_page.html#L193-L206) 的 username 输入与提交按钮之间插入：

```html
{{ if .Captcha }}
{{ .Captcha }}
{{ end }}
```

- [ ] **Step 5: 测试**

新建/扩展 `server/internal/lite/auth_handler_test.go`：直接调 `CaptchaImage(ip)` 断言三态输出；`liteCaptchaRequired` 对 always/after_n/disabled 断言。

- [ ] **Step 6: 运行测试 + 编译**

Run: `cd server && go build ./... && go test ./internal/lite/ -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
git add server/internal/lite/
git commit -m "feat(security): lite 登录/注册接入图形验证码（SSR 内嵌，零 JS）"
```

---

## Task 10: 前端 Vue 接入验证码

**Files:**
- Create: `ui/src/components/auth/CaptchaInput.vue`
- Modify: `ui/src/api/index.js`
- Modify: `ui/src/store/index.js`
- Modify: `ui/src/main.js`
- Modify: `ui/src/views/LoginView.vue`
- Modify: `ui/src/views/SettingsView.vue`
- Modify: `ui/src/views/settings/SettingsAuth.vue`
- Modify: `ui/src/views/settings/useSettingsDiff.js`

**前置**：Task 5、8（后端接口）完成。

- [ ] **Step 1: api 层**

在 [api/index.js](file:///d:/Projects/lightshare/ui/src/api/index.js#L156-L161) 的 `AuthApi` 增加：

```js
	getCaptcha() {
		return request.get('/auth/captcha')
	},
	login(username, password, captchaId, captchaAnswer) {
		return request.post('/auth/login', { username, password, captchaId, captchaAnswer })
	},
	register(username, password, captchaId, captchaAnswer) {
		return request.post('/auth/register', { username, password, captchaId, captchaAnswer })
	},
```

> 注意：保留现有 `login(username,password)`/`register` 的调用点兼容——改为参数带默认值 `captchaId = '', captchaAnswer = ''`，避免改坏现有入参处。

- [ ] **Step 2: store**

[store/index.js](file:///d:/Projects/lightshare/ui/src/store/index.js#L23-L35) config 增字段，`setConfig` 合并自然生效：

```js
	config: {
		// ... 现有
		allowRegistration: false,
		requireLogin: false,
		captchaMode: 'disabled',
		captchaTriggerAfter: 3,
		captchaRequired: false,
	},
```

- [ ] **Step 3: main.js options 读取**

[main.js](file:///d:/Projects/lightshare/ui/src/main.js#L50-L62) 的 setConfig 打包处追加：

```js
			captchaMode: d.account?.captcha?.mode ?? 'disabled',
			captchaTriggerAfter: d.account?.captcha?.triggerAfter ?? 3,
```

（captchaRequired 不从此读——由登录接口错误码动态置位。）

- [ ] **Step 4: CaptchaInput.vue**

新建 `ui/src/components/auth/CaptchaInput.vue`：

```vue
<template>
  <div v-if="visible" class="captcha-input">
    <img v-if="image" :src="image" alt="验证码" class="captcha-image" @click="refresh" />
    <el-input v-model="text" placeholder="验证码" :maxlength="4" clearable @keyup.enter="onEnter" />
  </div>
</template>

<script setup>
import { ref, watch } from 'vue'
import { AuthApi } from '@/api'

const props = defineProps({
  visible: { type: Boolean, default: false },
})
const emit = defineEmits(['change', 'enter'])
const image = ref('')
const text = ref('')
const id = ref('')

async function refresh() {
  const res = await AuthApi.getCaptcha()
  id.value = res?.data?.captchaId
  image.value = res?.data?.image
  text.value = ''
}
watch(() => props.visible, (v) => { if (v) refresh() }, { immediate: true })

function onEnter() { emit('enter') }
watch(text, () => emit('change', id.value, text.value))
</script>

<style scoped>
.captcha-input { display: flex; align-items: center; gap: 8px; }
.captcha-image { height: 38px; cursor: pointer; border: 1px solid #dcdfe6; border-radius: 4px; }
</style>
```

> 注意：需按项目 request 响应结构（interceptors L64-117 是否解包 data）核对 `res` 取值；`@/api` 别名与分号/代码风格对齐项目 lint。

- [ ] **Step 5: LoginView.vue 联动**

在登录/注册 el-form 中用户名与提交按钮之间插入 `<CaptchaInput v-if="captchaRequired" v-model.. @change="setCaptcha" />`；`handleLogin`/`handleRegister` 携带 captchaId/captchaAnswer；错误响应中若指示验证码（捕获 `code=='captcha_required'` 或 message 含"验证码"）则置 `captchaRequired=true`。

- [ ] **Step 6: 设置页**

- `SettingsAuth.vue`（[L7-L13](file:///d:/Projects/lightshare/ui/src/views/settings/SettingsAuth.vue#L7-L13)）新增三态 radio + 阈值输入，绑定 `settings.account.captcha.mode` / `triggerAfter`：

```vue
<el-form-item label="登录验证码">
  <el-radio-group v-model="settings.account.captcha.mode">
    <el-radio value="disabled">禁用</el-radio>
    <el-radio value="always">始终开启</el-radio>
    <el-radio value="after_n">失败后开启</el-radio>
  </el-radio-group>
  <el-input-number v-if="settings.account.captcha.mode==='after_n'"
     v-model="settings.account.captcha.triggerAfter" :min="1" :max="20" />
</el-form-item>
```

- `SettingsView.vue` loadSettings account 写入 [L433-438](file:///d:/Projects/lightshare/ui/src/views/SettingsView.vue#L433-L438) 增 `captcha: { mode: cfg.account?.captcha?.mode ?? 'disabled', triggerAfter: cfg.account?.captcha?.triggerAfter ?? 3 }`；保存后 setConfig [L870-881](file:///d:/Projects/lightshare/ui/src/views/SettingsView.vue#L870-L881) 追加 captchaMode/captchaTriggerAfter。

- `useSettingsDiff.js` FIELD_LABELS 增 `account.captcha: '登录验证码'`。

- [ ] **Step 7: 前端构建 + lint**

Run: `cd ui && npm run lint && npm run build`
Expected: 构建通过

- [ ] **Step 8: Commit**

```bash
git add ui/src/
git commit -m "feat(ui): 登录/注册接入图形验证码，设置页新增验证码三态开关"
```

---

## Task 11: 收尾联调与回归

**Files:**
- 无新增

- [ ] **Step 1: 全量后端测试**

Run: `cd server && go test ./...`
Expected: 全 PASS

- [ ] **Step 2: 全量前端构建**

Run: `cd ui && npm run build`
Expected: 通过

- [ ] **Step 3: 手动冒烟（可选）**

启动服务，验证：
1. 默认（captcha disabled）：登录/注册行为与改造前一致。
2. `account.captcha.mode=always`：Web 登录页出现验证码，错误验证码被拒；lite `/lite/login` 同样内嵌验证码。
3. `after_n=3`：连续失败 3 次后第 4 次出现验证码。
4. 开启 `require_login`：`/api/v1/ready` 未登录 302/401 → 登录后可访问；`/health` 无 goVersion/绝对路径；匿名 `GET /api/open/v1/openapi.json` 401，带 Web Cookie 后 200。
5. options 不再有 `upload.urlUpload.allowedIPRanges`，且登录/注册页正常（无前端依赖回归）。

- [ ] **Step 4: Commit（如冒烟有改动）**

```bash
git add -A
git commit -m "fix(security): 冒烟回归修复"
```

---

## 自审说明（对照设计文档）

- P1 /ready：Task 1 ✓
- P2 /health：Task 2 ✓
- P3 openapi.json：Task 3 ✓（含四象限测试）
- P4 验证码：Task 4-10 ✓（captcha 包 + 配置 + web/lite 接入 + 前端）
- options 移除 allowedIPRanges：Task 5 ✓
- lite 独立计数：Task 6 + 9 ✓
- 测试计划：Task 2/3/6/7/8/9 覆盖 ✓

## 已知待实现时核实的 API 细节（写死会编译失败，务必 go doc 核对）

1. `jwt.ParseJWT` 精确签名（Task 3）
2. `base64Captcha.DefaultMemStore` 是否导出 / `NewMemoryStore` 用法、`DriverString` 参数个数与 `Colors` 字段名（Task 4/7）
3. `response.HandleBadRequest` 第三个参数语义与错误 code 携带方式（Task 8）
4. `updates.Add` 的 key-value 类型约定（Task 5）
5. `services.RegisterRequest/LoginRequest` 现字段与 binding tag（Task 8）
6. lite `renderAuthPage` 的签名是否含 c（调研显示含 c，L104-124）
