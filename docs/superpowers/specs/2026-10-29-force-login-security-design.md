# 强制登录安全边界补全设计

- 日期：2026-10-29
- 状态：待评审
- 范围：后端 `server/` + 配套前端 `ui/`

## 1. 背景与问题

项目支持「强制登录」配置 `account.require_login`（默认 false）。开启后大部分 Web 边界已正确强制登录（Web API、CLI、下载/分享/预览、WebDAV、FTP 匿名、Open API），但审计发现以下**安全边界漏洞与信息泄露**：

| # | 问题 | 位置 | 风险等级 |
|---|------|------|----------|
| P1 | `GET /api/v1/ready` 挂在 `api` 组，绕过 LoginGate，强制登录开启时仍匿名 | [routes_web.go](file:///d:/Projects/lightshare/server/routes_web.go#L46) | 中 |
| P2 | `GET /api/v1/web/health` 匿名暴露 Go 版本、goroutine 数、**存储目录绝对路径** | [health/handler.go](file:///d:/Projects/lightshare/server/internal/health/handler.go#L48) | 中(信息泄露) |
| P3 | `/api/open/v1/openapi.json` 暴露 API 全貌：强制登录下 auth_mode=none 被 AuthGate 403，已登录 Web 用户也无法读文档（AuthGate 不认 Cookie）；需改为「强制登录时需有效凭证（Web 登录态或 API Key）」 | [routes_top.go](file:///d:/Projects/lightshare/server/routes_top.go#L115-L139) | 中 |
| P4 | 登录/注册仅 IP 级 5 次失败锁定，无验证码，暴力破解防护单一 | [login_limit.go](file:///d:/Projects/lightshare/server/internal/auth/login_limit.go#L11-L16) | 低-附加 |

`/options` 如前评估**不改字段**：其字段（FTP/WebDAV/预览/上传开关、端口）均非敏感信息，且登录后前端（AppFooter 连接方式、preview.js 可预览判定、SettingsView 配置刷新）依赖完整字段，前置登录白名单使其匿名可读是**功能必需**。仅移除其中敏感度偏高的 `upload.urlUpload.allowedIPRanges`（内网网段白名单，属于部署情报）。

## 2. 设计决策（已与用户确认）

1. **ready/health/options 瘦身**：仅返回必要项，避免未登录信息泄露。
2. **openapi.json 受强制登录影响**：开启强制登录后匿名不可读。
3. **验证码实现**：引入第三方库 `github.com/mojocn/base64Captcha`（PNG 输出）做薄封装；后台开关三态——`disabled / always / afterN`（始终关闭 / 始终开启 / 失败 N 次后开启）。
4. **验证码形态**：4 字符图形验证码（`base64Captcha` DriverString，PNG 输出，无字体/系统依赖）。
5. **验证码作用范围**：**Web 登录/注册 + lite 轻量版登录/注册**（FTP/WebDAV 不适用，用户已确认；lite 为用户后追加要求）。lite 为 SSR 无 JS 页面，采用服务端内嵌验证码。
6. **计数粒度**：复用 IP 登录限流计数（[login_limit.go](file:///d:/Projects/lightshare/server/internal/auth/login_limit.go#L11-L16) 的滑动窗口）。
7. **阈值**：默认 3 次失败后开启验证码（低于 5 次锁定红线，形成两档保护）。
8. **options 瘦身策略**：精简不敏感项方案（保留登录后必需字段，仅移除少数真正敏感项）。
9. **范围**：后端 + 配套前端一起改。

## 3. 方案设计

### 3.1 health / ready 瘦身

**现状**：[handler.go](file:///d:/Projects/lightshare/server/internal/health/handler.go#L48-L142) 的 `Health` 返回 `version / goVersion / goroutines / checks(含存储目录绝对路径) / timestamp`；`Ready` 返回 `status / checks(config/rootDirs/tempDir) / version`（只含布尔与路径存在性，无绝对路径泄露，主要问题在 Health）。

**改动：**

- `Health` 响应瘦身：
  - **移除** `goVersion`、`goroutines`。
  - **移除**存储目录检查的绝对路径 Message（ok 分支的 `Message: dirPath` 直接删掉；error 分支的「目录不存在: <path>」「无权限访问: <path>」改为不含路径的文案 `"目录不存在"` / `"无权限访问"`）。
  - **保留** `status / version / checks(status 布尔摘要) / timestamp`。确实保留布尔式的目录健康摘要（`storage: ok/error`），但不带路径。
- `Ready` 保持现状（不含敏感信息），但路由挂载收敛到 LoginGate。

**路由收敛：**
- `/api/v1/ready` 从 `api` 组移动到 `web` 组（走 LoginGate），强制登录开启时不再匿名。
- `/api/v1/web/health` 已在 `web` 组白名单内，保持匿名（但内容已瘦身）。

### 3.2 openapi 文档受强制登录影响

**现状核实**（自审确认）：
- [routes_top.go](file:///d:/Projects/lightshare/server/routes_top.go#L115-L139) 中 `openAPI := r.Group("/api/open/v1")` 在**组级**挂了 `openapi.AuthGateMiddleware(...)`；`openapi.json`、`/docs` 都注册在该组内。
- [auth.go](file:///d:/Projects/lightshare/server/internal/openapi/auth.go#L38-L96) `AuthGateMiddleware` 行为：
  - `auth_mode=none`：`requireLogin=true` 时**直接 403**（强制登录优先）；`requireLogin=false` 时匿名放行。
  - `auth_mode=api_key`：校验 Authorization Bearer API Key。
- [handler.go](file:///d:/Projects/lightshare/server/internal/openapi/handler.go#L439-L447)：`GetOpenAPISpec` 直接输出完整 spec；`GetDocsPage` 是 301 跳到 `/scalar.html`。
- [router.go](file:///d:/Projects/lightshare/server/router.go#L136-L138)：`/scalar.html` 及其 `/scalar/assets` 走 SPA 静态资源，匿名暴露。

**关键发现（自审推翻初稿方案）**：
1. 强制登录 + auth_mode=none 时，openapi.json 已被组级 AuthGate **403 拦截**——匿名者本来就读不到，不存在"匿名可读 API 全貌"的洞。
2. 但存在**割裂**：`/docs`/`/scalar.html` 静态页匿名可加载，页面内 fetch `openapi.json` 却 403——**已登录 Web 用户也读不到文档**（AuthGate 只认 API Key，不认 Web Cookie）。
3. 初稿的 `LoginRequiredWhenForced` 挂在**路由级**不可行：Gin 中组级 AuthGate 先执行并短路 403，路由级守卫永远不触发；且若 `requireLogin=false` 时无条件 `c.Next()`，会在 auth_mode=api_key 场景把文档改成匿名可读——开了新洞。

**最终方案（把 openapi.json 从组级 AuthGate 链中抽出，单独守卫）**：
- 将 `/api/open/v1/openapi.json` 从 `openAPI` 组内移出，**单独注册**在只挂 `LoginRequiredWhenForced()` 中间件的新子组（仍保留 FeatureGate / IP 白名单 / 限流 / 响应包装；**不挂 AuthGate**）。
- 新增 `openapi.LoginRequiredWhenForced()` 中间件逻辑（依据 [auth.go](file:///d:/Projects/lightshare/server/internal/openapi/auth.go#L98-L105) 的 `CurrentAuthMode()`）：
  ```
  cfg := appconfig.GetConfig()
  mode := CurrentAuthMode()
  if mode == auth_mode_api_key {
      // 保持现状：校验 Bearer API Key（复用 AuthGate 的 api_key 分支逻辑或直接调 AuthGate）
      AuthGate(rbac)=c.Next() // 或复用同一实现
      return
  }
  // auth_mode == none（历史默认）
  if !cfg.Account.RequireLogin {
      c.Next() // 非强制登录：公开（历史行为）
      return
  }
  // 强制登录 + none：要求有效凭证——Authorization Bearer（API Key 或 JWT）或 Cookie JWT
  if 无有效凭证 {
      c.AbortWithStatusJSON(401, {"error": "需要登录"})
      return
  }
  c.Next()
  ```
  凭证校验复用现有 Cookie JWT 解析（与 [auth.go 中间件](file:///d:/Projects/lightshare/server/internal/middleware/auth.go#L94-L104) 的 Cookie 回退认证一致）。
- `/docs`、`/scalar.html` 保持现状（前端静态资源，与登录页同级匿名）。
- **效果矩阵**：

  | requireLogin | auth_mode | openapi.json |
  |---|---|---|
  | false | none | 匿名可读（历史行为） |
  | false | api_key | 需 API Key（AuthGate） |
  | true | none | **需凭证**：Web Cookie JWT 或 Bearer，否则 401 |
  | true | api_key | 需 API Key（AuthGate） |

  强制登录后匿名者不可读 API 全貌；已登录 Web 用户携带 Cookie 可读文档（修复割裂）；API Key 消费者带 Bearer 可读。

### 3.3 图形验证码（自建，Web 登录 + lite 轻量版登录）

#### 3.3.1 配置

在 `appconfig.AccountConfig` 新增：

```yaml
account:
  captcha:
    # 验证码策略：disabled=始终关闭；always=始终开启；afterN=失败 N 次后开启
    mode: disabled   # 或 always / afterN
    # 阈值：mode=afterN 时生效，窗口内登录失败达到该次数后要求验证码（默认 3）
    trigger_after: 3
```

新增 `CaptchaConfig` 结构体与 `AccountResponse` 对应 DTO 字段：

```go
type CaptchaConfig struct {
    Mode         string `yaml:"mode"`           // disabled | always | afterN
    TriggerAfter int    `yaml:"trigger_after"`  // afterN 模式的失败次数阈值
}
```

DTO：`AccountResponse.Captcha CaptchaResponse`，字段 `mode`、`triggerAfter`。并在 [handler.go](file:///d:/Projects/lightshare/server/internal/appconfig/api/handler.go#L242-L243) 的 SaveConfig 的 account 节补写与 `updates.Add`。

**默认值**：`mode=disabled`（不改变现有行为）；`triggerAfter=3`。`fuzhan.sample.yaml` 与本部署 `fuzhan.yaml` 补充样例。

#### 3.3.2 验证码生成与校验

引入第三方库 **`github.com/mojocn/base64Captcha`**（v1.3.8，Apache-2.0，PNG 输出）——经查其无 Gin 专用插件，但本身就是通用 REST 友好的库，薄封装一层 handler 即可。新增 `server/internal/captcha` 包，内部**单例持有** `base64Captcha.Captcha`，对外只暴露本项目语义的两个函数（被 `internal/auth`（web handler）与 `internal/lite`（lite handler）共同引用；本包不依赖二者，无循环依赖）：

```go
// Generate 生成一张验证码图，返回：challenge(id) 与可直接 base64 内嵌的 PNG data URI
func Generate() (id string, pngBase64 string, err error)

// Verify 校验一次（clear=true 立即销毁，一次性语义）
func Verify(id, answer string) bool
```

- **driver**：`base64Captcha.NewDriverString(高度, 宽度, 噪声点, 干扰线, 4, 字符集, ...)`——4 字符、字符集排除易混淆的 `0O1lI`、随机斜线与噪点（库内置）。
- **store**：`base64Captcha.NewMemoryStore(收集阈值, 过期几秒)`——**内置**周期性清理，无自维护 map。
- **返回**：库的 `Generate()` 直接返回含 `data:image/png;base64,...` 前缀的字符串，主站接口与 lite 内嵌 `<img>` 都直接复用。
- **防范滥用**：验证码获取/生成接口本身做 **IP 维度简单限流**（每 IP 每分钟 30 张，防刷 challenge 占内存）。

**接入点 A —— 主站 Vue（动态拉取）：**
对外接口（挂 `auth` 组白名单匿名，位于 LoginGate 白名单内）：
- `GET /api/v1/web/auth/captcha` → 200 返回 `{ id, pngBase64 }`（`data:image/png;base64,...`）。前端按 options 的 `required` 决定是否拉取，提交登录时携带 id + answer；验证失败响应提示需验证码时前端动态拉取刷新。

**接入点 B —— lite 轻量版（服务端内嵌，零 JS）：**
lite 认证页是**无 JS 的纯 HTML 表单**（[auth_page.html](file:///d:/Projects/lightshare/server/internal/resources/auth_page.html#L193-L206)），不能依赖前端 options 动态拉取。方案：
- **渲染时判定**：`HandleLoginPage` / `HandleRegisterPage`（[auth_handler.go](file:///d:/Projects/lightshare/server/internal/lite/auth_handler.go#L141-L196)）渲染页面时，若当前 IP 需要验证码，直接调用 `captcha.Generate()`，把 `id`（隐藏域 `captcha_challenge`）+ PNG 图片（`<img src="{pngBase64(data:image/png;base64,...)}">`）内嵌进表单。
- **提交时校验**：`HandleLoginSubmit` / `HandleRegisterSubmit`（[auth_handler.go](file:///d:/Projects/lightshare/server/internal/lite/auth_handler.go#L150-L236)）在 `parseAuthForm` 后增加 `CaptchaGate(ip)`：需要验证码时读取 `captcha_challenge` 隐藏域 + 用户输入的 `captcha_text`，缺失/错误 → 重新渲染登录页并携带错误提示（同现有错误渲染路径，[renderAuthPage](file:///d:/Projects/lightshare/server/internal/lite/auth_handler.go#L104-L124)）。
- **刷新**：无需 JS——验证码每次渲染都重新 `Generate()`，重试失败重新渲染表单即得新图；也可在页面上放一个「看不清？刷新页面」链接（无需 JS）。

**服务端渲染判定与主站一致性**：两处触发判定都复用一个共享的 `account.captcha` 配置 + `afterN` 阈值，只是各自的 IP 失败计数源不同（web 用 auth/login_limit，lite 用 lite/auth_limit），见 3.3.3。配置语义对两个入口一致生效。

#### 3.3.3 触发判定：复用 IP 登录限流计数（web 与 lite 各一套）

**web 端**（`internal/auth`）：在 [login_limit.go](file:///d:/Projects/lightshare/server/internal/auth/login_limit.go#L11) 增补只读查询函数（不加锁语义之外的改动）：

```go
// loginFailureCount 返回指定 IP 当前窗口内的失败次数（含锁定期延续计数）
func loginFailureCount(ip string) int
```

**lite 端**（`internal/lite`）：lite 限流是**独立实现**（[auth_limit.go](file:///d:/Projects/lightshare/server/internal/lite/auth_limit.go#L8-L9) 注释："auth 包内的防护函数为包私有无法复用，此处独立实现"）。故对 lite 的同一套代码做**对称增量**：

```go
// 在 liteIPRecord 增加同名字段，函数换 lite 前缀
func liteLoginFailureCount(ip string) int
```

两端字段语义一致——新增 `displayFailures` 字段与现有 `count`（锁定计数）分离：`registerLoginFailure` / `liteRegisterFailure` 同时自增 `count` 与 `displayFailures`；`resetLoginFailures` / `liteResetFailures` 同时清零两者。这样锁定期 `count` 归零不影响 `displayFailures` 用于验证码触发判定。

**触发规则**（Web 与 lite 的登录/注册接口通用，仅计数源不同）：

```
failures := loginFailureCount(ip)      // web handler 用 auth 包
failures := liteLoginFailureCount(ip)  // lite handler 用 lite 包
captchaMode := cfg.Account.Captcha.Mode
switch captchaMode {
case always:
    require = true
case afterN:
    require = failures >= cfg.Account.Captcha.TriggerAfter
case disabled:
    require = false
}
```

可将该判定抽为共享辅助函数（入参 `mode, triggerAfter, failures` 或 `ip + 计数函数`），由两个 handler 各自调用，保证 `always/afterN/disabled` 三态语义完全一致。

**判定实现**：
- Web（Vue）：登录/注册 handler 前置 `CaptchaGate(ip)`，`require=true` 时请求必须携带 `captchaChallenge` + `captchaAnswer`；缺失或校验失败 → 400（code 指示需要验证码）；通过后继续原登录逻辑。
- lite（SSR）：渲染页时内嵌、提交时校验（见 3.3.2 接入点 B）；失败经现有错误渲染路径返回并携带新图。

**返回给前端的状态**：主站登录响应与 options 均带 `account.captcha.required`（布尔，当前 IP 是否需要验证码）；options 的 account 节新增 `captcha: { mode, triggerAfter, required }`。lite 不需要该字段（SSR 已把结果内嵌进页面）。

#### 3.3.4 注册接口的验证码

强制登录 + 允许注册同时开启时，**匿名注册保留**（用户已确认）。Web 与 lite 的注册接口都走各自入口的 `CaptchaGate`（触发阈值共享各自 IP 计数，防注册接口被刷验证码绕过登录限制）。注册成功后 `resetLoginFailures` / `liteResetFailures` 一并清零验证码状态。

### 3.4 options 字段调整

[handler.go](file:///d:/Projects/lightshare/server/internal/appconfig/api/handler.go#L30-L87) `GetOptions`：
- **移除** `upload.urlUpload.allowedIPRanges`（内网网段情报）。
- **保留其余全部字段**（登录后前端依赖，非敏感）。
- **新增** `account.captcha = { mode, triggerAfter, required }`（required 按当前请求 IP 动态计算，供前端登录页渲染验证码区）。

### 3.5 前端配套

**主站 Vue（改前端）**：
1. **新验证码组件** `ui/src/components/auth/CaptchaInput.vue`：
   - 显示 PNG 验证码图 + 输入框 + 「点击刷新」。
   - props：`v-model`（输入值）；加载时 `required` 为 true 才渲染（登录页按 options 的 `required` 显示）。
   - 提交登录时携带 `captchaId` + `captchaAnswer`。
2. **LoginView.vue**：
   - 从 store 读取 `account.captcha.required`，为 true 时显示验证码区并在登录请求携带验证码字段。
   - 登录失败响应若 code 提示需验证码，动态置 `required` 并显示验证码区。
   - 提供验证码刷新入口（点击图片换一张）。
3. **RegisterView / 注册表单**（[LoginView.vue](file:///d:/Projects/lightshare/ui/src/views/LoginView.vue#L126) 的注册切换）：同样的验证码联动。
4. **store**：`store.state.config` 增 `captchaMode / captchaTriggerAfter / captchaRequired` 字段；[main.js](file:///d:/Projects/lightshare/ui/src/main.js#L50-L62) 与 [SettingsView.vue](file:///d:/Projects/lightshare/ui/src/views/SettingsView.vue#L868-L881) 从 options 读取 `d.account?.captcha`。
5. **SettingsAuth.vue / SettingsView.vue**：设置页「账号」节新增验证码开关三态（radio：禁用 / 始终开启 / 失败 N 次开启）+ `triggerAfter` 数字输入；保存逻辑并入 account 节。
6. **AppFetch/api 层**：`LoginApi.captcha()` 新增接口；登录/注册请求体允许可选的 `captchaId`/`captchaAnswer`。

**lite 轻量版（无前端改动，纯后端 SSR）**：认证页 [auth_page.html](file:///d:/Projects/lightshare/server/internal/resources/auth_page.html) 增加验证码表单控件（隐藏域 `captcha_challenge` + PNG img + 输入框，模板顶部条件渲染）；[auth_tmpl.go](file:///d:/Projects/lightshare/server/internal/lite/auth_tmpl.go#L16-L20) 模板函数扩展提供验证码数据；逻辑由 [auth_handler.go](file:///d:/Projects/lightshare/server/internal/lite/auth_handler.go) 的渲染/提交路径处理，不涉及任何 JS/前端资源改动。

## 4. 数据流

### 4.1 登录（需验证码场景）

**主站 Vue（动态拉取）：**

```
Browser                      Gin                            captcha pkg           login_limit
  |  GET /options -----------> account.captcha.required      |                   loginFailureCount(ip)
  |  GET /auth/captcha ------> 200 {id, pngBase64}           |  Generate()
  |  POST auth/login {user,pwd,id,answer} -> CaptchaGate -> Verify(id,answer)
  |                            ├─ 验证失败 => 400 需验证码（销毁 id）
  |                            └─ 通过 => 原 Login 流程 -> 成功 SetAuthCookie / 失败 registerLoginFailure
```

**lite 轻量版（服务端内嵌，零 JS）：**

```
Browser                       Gin(lite)                              captcha pkg        lite auth_limit
  |  GET /lite/login --------> HandleLoginPage: 需要验证码? ---------> Generate()
  |                           渲染 form(id 隐藏域 + PNG img)                            liteLoginFailureCount(ip)
  |  POST /lite/login {user,pwd,captcha_challenge,captcha_text} -> CaptchaGate -> Verify(captcha_challenge,captcha_text)
  |                            ├─ 错误 => 重新渲染登录页 + 错误提示 + 新图
  |                            └─ 通过 => 原 Login 流程 -> SetAuthCookie + 302 /lite
```

### 4.2 验证码计数与 reset

- web：`registerLoginFailure(ip)` → `count++`、`displayFailures++`；达到 5 次 → 锁定（现有逻辑，count 归零但 displayFailures 保留窗口语义）。
- web：`resetLoginFailures(ip)` → 锁定/计数/displayFailures 全部清零（登录成功）。
- web：`loginFailureCount(ip)` 读 `displayFailures`——供 `afterN` 模式判定与 options 的 `required` 动态计算。
- lite：`liteRegisterFailure` / `liteResetFailures` / `liteLoginFailureCount` 与上面完全对称（各自独立 map）。

## 5. 错误处理

| 场景 | 行为 |
|------|------|
| 需要验证码但未携带（web） | 400，`{ code: "captcha_required", message: "请输入验证码" }` |
| 验证码错误/过期（web） | 400，`{ code: "captcha_invalid", message: "验证码错误或已过期" }`，前端刷新图形并清空输入 |
| 验证码错误/过期（lite） | 重新渲染认证页 + 错误提示条 + 新图（沿用 [renderAuthPage](file:///d:/Projects/lightshare/server/internal/lite/auth_handler.go#L104-L124) 错误渲染路径） |
| challenge 不存在（被重用） | 视为 captcha_invalid，一次性销毁 |
| 始终关闭（默认） | 不校验，行为与现状完全一致 |
| 验证码索取接口滥用（web） | IP 级限流（每分钟 30 次），超限返回 429 |
| lite 被滥用 | 复用 lite 自有限流（15min/5 次锁定）；验证码通道本身每次渲染计数一体，无需额外限流 |

## 6. 测试计划

### 后端
1. `captcha` 包单元测试：`Generate`/`Verify` 一次性命中、过期、错误字符集、并发安全。
2. `login_limit` 增 `displayFailures` 字段的测试：计数、锁定后窗口语义、reset；`lite/auth_limit` 同构测试。
3. auth handler 集成：`disabled / always / afterN` 三态验证码拦截逻辑；afterN=3 时第 3 次失败后需验证码。
4. lite handler 集成：认证页内嵌验证码渲染、提交校验失败重新渲染、三态触发、注册路径。
5. health handler 测试更新：断言响应不再含 `goVersion/goroutines/绝对路径`。
6. openapi 文档守卫测试：四象限矩阵（none+非强制=公开；none+强制=无凭证 401 / 带 Cookie 或 Bearer 通过；api_key=需 API Key）。
7. `GetOptions` 测试：不再返回 `allowedIPRanges`；account.captcha 三字段存在。

### 前端
1. `CaptchaInput.vue`：渲染、刷新、未加载时隐藏。
2. Login/Register 联调：afterN 场景手动验证三态。

### lite
1. 老旧浏览器（无 JS）下验证码渲染与提交可用。

## 7. 影响面与兼容性

- **默认 `mode=disabled`** → 现有部署零行为变化。
- `/health` 字段减少：若存在依赖 `goVersion/goroutines` 的外部探针需同步（本项目内无明显消费方——前端只读 `health.status`）。
- `/ready` 挂载点变更：强制登录开启后进入 web 组，行为符合预期。
- `allowedIPRanges` 移除：前端 `api/index.js` 无直接消费该字段（URL 上传设置使用自身接口），需在实现时复核一次。
- 验证码影响 **Web 登录/注册 + lite 登录/注册**（配置三态统一生效）；FTP/WebDAV/CLI/OpenAPI 不受影响（用户确认的范围）。
- lite 与主站使用**各自独立**的 IP 失败计数：同一 IP 在 lite 登录失败 3 次后，主站仍按主站自己的计数判定（并发打两个入口不会互相放大锁定，独立限流与独立验证码触发）。
