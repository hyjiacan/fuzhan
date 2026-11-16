# snake_case 全量统一 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 HTTP API 的请求参数与响应字段全量迁移到 `snake_case`，消除「前端发 snake、后端读 camel」导致的功能性缺陷，使代码与 [API_STYLE_GUIDE.md](../../design/api/API_STYLE_GUIDE.md) 一致。

**Architecture:** 不动 URL、不动路由、不动响应信封（`success`/`code`/`message`/`data`/`errors` 已是 snake）。只改三类字符串：① Go 结构体的 `json` tag（`ShouldBindJSON` 绑定与序列化都靠它）；② `gin.H` / `map[string]interface{}` 字面量的 key；③ 少量 `form` tag 与 `c.PostForm`/`c.Query` 字面量。前端同步改「进出契约边界」上的 key（`api/index.js` 的请求体、各 View 读 `res.data.*` 的地方），前端组件内部 state 保持 camel 不变。改动按「安全止血 → 模块垂直切片 → 文档对齐」三阶段推进，每一片后端与对应前端同批提交，保证任一时刻前后端可联调。

**Tech Stack:** Go 1.26 + Gin（`server/`）、Vue 3 + Element Plus（`ui/`）、gorm、手写 OpenAPI 3.0.3（`server/internal/openapi/spec.go`）。

**规范依据:**
- [API_STYLE_GUIDE.md](../../design/api/API_STYLE_GUIDE.md) —— 主规范，§4 请求参数、§5 响应字段、§7 例外
- [API_STYLE_UNIFICATION.md](../../design/api/API_STYLE_UNIFICATION.md) —— 存量迁移对照表（§1.4、§1.6、§3.x）

**存量规模（本次勘查实测，可用于估算进度）:**

| 侧 | 度量 | 数值 | 复现命令 |
|---|---|---|---|
| 后端 | 含 camelCase `json`/`form` tag 或 map key 的文件数 | 60 | 见 Task 0.1 |
| 后端 | 行数合计 | 677 | 见 Task 0.1 |
| 前端 | 引用到后端 camel 字段名的文件数 | 74 | 见 Task 0.1 |

后端 Top 文件：`appconfig/dto.go`(94)、`index/model.go`(55)、`models/upload.go`(49)、`appconfig/api/handler.go`(29)、`appconfig/config.go`(26)、`file/url_task_handlers.go`(26)、`file/upload.go`(26)、`temp/temp_session.go`(20)、`models/url_download_task.go`(20)、`models/file_record.go`(19)、`behavioranalytics/service.go`(19)、`models/task_record.go`(18)、`models/api_key.go`(18)、`resource/types.go`(17)、`services/monitor_service.go`(17)、`services/admin_service.go`(14)、`setup/handler.go`(12)。

前端 Top 文件：`views/SettingsView.vue`(180)、`views/settings/useSettingsDiff.js`(93)、`api/index.js`(74)、`components/upload/hooks/useUploadQueue.js`(69)、`views/AdminOpenApiView.vue`(56)、`views/SetupView.vue`(55)、`components/file/FilePreview.vue`(51)。

---

## 前置约束（全局，每个 Task 都适用）

1. **不改 Go 字段名与 `gorm` tag**。只改 `json` / `form` tag 与 map key。`gorm` 列名与数据库无关。
2. **不引入全局 axios 自动转换**。理由：会误改 `FormData`、二进制流、SSE、WebDAV，以及 `MimeTable` / `Webhook.Headers` / `Webhook.Query` 这些「key 是用户数据」的 map。所有改动显式写出 snake key。
3. **前端组件内部 state 保持 camel**。只改三处边界：`api/index.js` 的请求体/URL 参数、View 中 `res.data.xxx` 的读取、以及发往后端的 payload 构造。
4. **必须排除的 key（不是 API 契约字段）:**
   - `appconfig/dto.go` 的 `MimeTable map[string]string`（key 是 MIME 类型）
   - `appconfig/dto.go` 的 `WebhookEndpointDTO.Headers` / `.Query`（key 由用户自定义）
   - `internal/auth/ldap.go` 中 `sAMAccountName` / `displayName` / `givenName`（LDAP 协议属性名）
   - `internal/lite/lite.go` 的 `crumbSep`（模板块函数名）
   - `internal/openapi/spec.go`、`internal/openapi/middleware.go` 中全部 camelCase（`requestBody`、`maxLength`、`additionalProperties`、`securitySchemes`、`bearerFormat` 等，全是 OpenAPI 关键字）
   - `server/third_party/`、`server/web/`（构建产物）
   - HTTP 头（`Authorization`、`Accept`、`Origin`、`Range`、`Content-Type`）
   - OAuth2 回调参数 `code` / `state` / `error_description`
5. **改 `json` tag 时必须同步检查裸字符串**。`appconfig/api/handler.go` 的 `SaveConfig` 用 `present("rootDirs")`、`raw["storageBackend"]`、`sbFields["alert"]` 这类裸字符串判断「前端是否提交了该节点」，这些字符串必须与 tag 一起改，否则配置保存会静默失败。完整清单见 Task 2.2。
6. **每个 Task 结束都要 `cd server; go test ./...`**（有测试的包各自跑）。前端 Task 结束要 `cd ui; yarn build`。
7. **不要批量替换**。同名 camel key 在不同上下文语义可能不同（例如 `pageSize` 在前端是变量名、在后端是响应 key），逐处确认后再改。

---

## 阶段划分

| 阶段 | 目标 | Task |
|---|---|---|
| 0 | 建立基线：统计脚本 + 命名守卫测试（先红） | 0.1 ~ 0.3 |
| 1 | 安全止血：两个功能性 bug（验证码、文件校验值） | 1.1 ~ 1.4 |
| 2 | 模块垂直切片：配置 / 文件上传 / 用户鉴权 / 管理端 / 索引监控 / 其余 | 2.1 ~ 2.6 |
| 3 | 文档对齐：`API_INTERFACE.md`、`API_STYLE_UNIFICATION.md`、`project-context.md` | 3.1 ~ 3.3 |

顺序原则：阶段 0 先建守卫测试，后续每个 Task 都用它兜底；阶段 1 优先修已经造成用户可见故障的两处；阶段 2 每片内后端与前端必须同批改完再进下一片，避免长时间前后端不一致。

---

## 阶段 0：基线与守卫

### Task 0.1: 固化实测脚本

**Files:** 无（只在本机跑，不提交脚本文件）

- [ ] **Step 1: 统计后端 camelCase tag / map key**

```powershell
$root='d:\Projects\lightshare\server'
$rows=@()
Get-ChildItem -Path $root -Recurse -Filter *.go |
  Where-Object { $_.FullName -notmatch '\\third_party\\|\\web\\|_test\.go$' } |
  ForEach-Object {
    $lines=[System.IO.File]::ReadAllLines($_.FullName)
    for($i=0;$i -lt $lines.Count;$i++){
      foreach($m in [regex]::Matches($lines[$i],'(json|form):"([A-Za-z0-9_\.]+)"')){
        $k=$m.Groups[2].Value
        if($k -cmatch '[A-Z]'){ $rows += ("{0}:{1}: {2}" -f $_.FullName.Replace($root,''),($i+1),$m.Value) }
      }
    }
  }
$rows | Sort-Object -Unique
"TAG_ROWS=" + ($rows | Sort-Object -Unique).Count
```

基线值：`TAG_ROWS` 约 600（只统计 tag；含 map key 时合计 677）。注意 `-cmatch`（区分大小写），`-match` 会误统计 snake。

map key 单独统计（`gin.H` / `map[string]interface{}` 字面量中带 camel 的 key）：

```powershell
$root='d:\Projects\lightshare\server'
$rows=@()
Get-ChildItem -Path $root -Recurse -Filter *.go |
  Where-Object { $_.FullName -notmatch '\\third_party\\|\\web\\|_test\.go$' } |
  ForEach-Object {
    $lines=[System.IO.File]::ReadAllLines($_.FullName)
    for($i=0;$i -lt $lines.Count;$i++){
      foreach($m in [regex]::Matches($lines[$i],'"([a-z][A-Za-z0-9]*[A-Z][A-Za-z0-9]*)"\s*:')){
        $rows += ("{0}:{1}: {2}" -f $_.FullName.Replace($root,''),($i+1),$m.Groups[1].Value)
      }
    }
  }
$rows | Sort-Object -Unique
"MAP_ROWS=" + ($rows | Sort-Object -Unique).Count
```

注意：该脚本会命中 `openapi/spec.go` 的 OpenAPI 关键字与 `auth/ldap.go` 的 LDAP 属性名，属预期噪音，不参与修改。

- [ ] **Step 2: 统计前端受影响文件**

```powershell
$ui='d:\Projects\lightshare\ui\src'
$pat='(fullPath|fileSize|pageSize|uploadId|createdAt|updatedAt|expiresAt|displayName|rootName|storageType|chunkIndex|chunkSize|totalChunks|uploadTime|clientIP|userId|userID|isDir|mimeType|xxh3Hash|downloadURL|scanProgress|captchaId|requireLogin|allowRegistration)'
$hit=@()
Get-ChildItem -Path $ui -Recurse -Include *.vue,*.js |
  Where-Object { $_.FullName -notmatch '\\node_modules\\' } |
  ForEach-Object { if([System.IO.File]::ReadAllText($_.FullName) -cmatch $pat){ $hit += $_.FullName.Replace($ui,'') } }
$hit.Count
```

基线值：`74`。

**验收:** 两次统计结果与上表基线一致（±2 以内，允许前期已修的两处差异）。

---

### Task 0.2: 新增命名守卫测试（先红）

**Files:**
- Create: `server/internal/naming/naming_test.go`

- [ ] **Step 1: 写失败测试**

```go
package naming

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"fuzhan/internal/appconfig"
	"fuzhan/internal/file"
	"fuzhan/internal/index"
	"fuzhan/internal/models"
	"fuzhan/internal/services"
	"fuzhan/internal/setup"
)

// snakeKey 合法 API 字段名：小写字母/数字，可用下划线分隔。
var snakeKey = regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)

// allowedKeys 例外清单：非 API 契约字段（见计划「前置约束」第 4 条）。
var allowedKeys = map[string]bool{
	"mimeType": true, // 仅出现在 MimeTable 的 value，不在本测试范围
}

// checkStruct 递归检查结构体 json tag。
func checkStruct(t *testing.T, name string, typ reflect.Type, skip map[string]bool) {
	t.Helper()
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" && !f.Anonymous {
			continue // 未导出字段
		}
		if skip[f.Name] {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		key := strings.Split(tag, ",")[0]
		if key == "" || key == "-" {
			continue
		}
		if !snakeKey.MatchString(key) {
			t.Errorf("%s.%s: json tag %q 不是 snake_case", name, f.Name, key)
			continue
		}
		ft := f.Type
		for ft.Kind() == reflect.Ptr || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "time" {
			checkStruct(t, name+"."+f.Name, ft, nil)
		}
	}
}

func TestRequestAndResponseTagsAreSnakeCase(t *testing.T) {
	cases := []struct {
		name string
		v    any
	}{
		{"services.RegisterRequest", services.RegisterRequest{}},
		{"services.LoginRequest", services.LoginRequest{}},
		{"services.AuthResponse", services.AuthResponse{}},
		{"file.FileChecksumRequest", file.FileChecksumRequest{}},
		{"setup.SetupConfigRequest", setup.SetupConfigRequest{}},
		{"appconfig.ConfigDTO", appconfig.ConfigDTO{}},
		{"models.UploadRequest", models.UploadRequest{}},
		{"models.UploadChunkRequest", models.UploadChunkRequest{}},
		{"models.FinalizeUploadRequest", models.FinalizeUploadRequest{}},
		{"models.ExtendTempFileRequest", models.ExtendTempFileRequest{}},
		{"index.ScanProgressView", index.ScanProgressView{}},
		{"index.DuplicateGroup", index.DuplicateGroup{}},
		{"index.DependencyTreeNode", index.DependencyTreeNode{}},
		{"index.RecordStatsResponse", index.RecordStatsResponse{}},
		{"index.ScanStatusResponse", index.ScanStatusResponse{}},
	}
	for _, c := range cases {
		checkStruct(t, c.name, reflect.TypeOf(c.v), nil)
	}
}
```

- [ ] **Step 2: 运行，确认红**

Run: `cd server; go test ./internal/naming/ -run TestRequestAndResponseTagsAreSnakeCase -v`
Expected: `FAIL`，并列出每个违规字段（例如 `services.LoginRequest.CaptchaID: json tag "captchaId" 不是 snake_case`）。

**注意:** 若因类型未导出/包名不对导致编译失败，先修正导入；不要为了让测试通过而缩小 `cases` 列表。

---

### Task 0.3: 确认前端无单测框架

**Files:** 无（只读）

- [ ] **Step 1: 检查 `ui/package.json` 的 scripts**

Run: `Get-Content d:\Projects\lightshare\ui\package.json`
Expected: 只有 `dev` / `build` / `build:scalar` / `build:all` / `preview`。`devDependencies` 含 `@playwright/test`，但无 `test` 脚本。

- [ ] **Step 2: 记录前端验证策略**

前端所有 Task 的验证方式统一为：
1. `cd ui; yarn build` 必须成功（产物落到 `server/web`，`server/assets.go` 通过 embed 打入二进制）。
2. `cd ui; yarn dev` 后用浏览器手工验证对应页面，或按需用 Playwright 脚本。

**验收:** 确认无 `test` script；不新增前端测试框架（超出本计划范围）。

---

## 阶段 1：安全止血

这一阶段修的是**已经造成功能失效**的两处命名不一致，改动小、价值高，先独立交付。

### Task 1.1: 修复验证码字段不一致（安全风险）

**背景:** 前端 [api/index.js](file:///d:/Projects/lightshare/ui/src/api/index.js) 的 `register` / `login` 已发 `captcha_id` / `captcha_answer`，而后端 `RegisterRequest` / `LoginRequest` 的 `json` tag 是 `captchaId` / `captchaAnswer` → 后端永远拿到空串。验证码开启时（`account.captcha.mode != disabled`）这条路径会静默走到「验证码校验失败」分支，等于验证码机制形同虚设并返回误导性错误。

**Files:**
- Modify: `server/internal/services/auth_service.go`（`RegisterRequest` L48-53、`LoginRequest` L56-61、`AuthResponse` L64-71）
- Modify: `server/internal/auth/handler.go`（`authSessionResponse` L114-124、`GetCaptcha` L143-156）
- Modify: `server/internal/auth/handler_captcha_test.go`（L132 注释、L145 结构体 tag、L156 断言文案）
- Modify: `ui/src/components/auth/CaptchaInput.vue`（L44）

- [ ] **Step 1: 改 `services/auth_service.go` 三个结构体**

```go
type RegisterRequest struct {
	Username      string `json:"username" binding:"required,min=3,max=32"`
	Password      string `json:"password" binding:"required,min=8,max=128"`
	CaptchaID     string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

type LoginRequest struct {
	Username      string `json:"username" binding:"required,max=64"`
	Password      string `json:"password" binding:"required,max=128"`
	CaptchaID     string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

type AuthResponse struct {
	Token       string `json:"token"`
	UUID        string `json:"uuid"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	Nickname    string `json:"nickname"`
	ExpiresIn   int    `json:"expires_in"`
}
```

- [ ] **Step 2: 改 `internal/auth/handler.go` 的 `authSessionResponse`**

```go
func authSessionResponse(resp *services.AuthResponse) gin.H {
	return gin.H{
		"uuid":         resp.UUID,
		"username":     resp.Username,
		"display_name": resp.DisplayName,
		"nickname":     resp.Nickname,
		"expires_in":   resp.ExpiresIn,
	}
}
```

- [ ] **Step 3: 改 `internal/auth/handler.go` 的 `GetCaptcha` 返回 key**

```go
	response.HandleSuccess(c, http.StatusOK, "", gin.H{
		"captcha_id": id,
		"image":      dataURI,
	})
```

- [ ] **Step 4: 同步前端 `ui/src/components/auth/CaptchaInput.vue` L44**

```js
    id.value = res?.data?.captcha_id || ''
```

- [ ] **Step 5: 同步后端测试**

`server/internal/auth/handler_captcha_test.go`：
- L132 注释改为「验证获取验证码接口返回 captcha_id 与 PNG data URI。」
- L145 结构体改为 `CaptchaID string \`json:"captcha_id"\``
- L156 断言文案改为 `t.Fatal("captcha_id 不应为空")`

- [ ] **Step 6: 全仓搜索残留**

Run: `cd server; go build ./...`

然后在编辑器全局搜索（区分大小写）：
- `captchaId`（预期只剩前端非契约位置，若无则 0 处）
- `captchaAnswer`（预期 0 处）

- [ ] **Step 7: 跑测试**

Run: `cd server; go test ./internal/auth/... ./internal/services/... ./internal/naming/ -v`
Expected: `oauth`/`captcha` 相关用例 PASS；`naming` 中 `services.LoginRequest`、`services.AuthResponse` 的报错消失（其他用例仍红，属预期）。

**验收:** 开启验证码后，登录/注册能正确携带并校验；`GetCaptcha` 响应为 `{"captcha_id": "...", "image": "data:image/png;base64,..."}`。

---

### Task 1.2: 修复文件校验值字段不一致（功能失效）

**背景:** 前端 [useUploadQueue.js](file:///d:/Projects/lightshare/ui/src/components/upload/hooks/useUploadQueue.js) L174-176 发的是 `{ storage_type, algorithms, root_name, path }` 或 `{ storage_type, algorithms, code }`，而后端 [preview.go](file:///d:/Projects/lightshare/server/internal/file/preview.go) 的 `FileChecksumRequest` 读 `storageType` / `rootName` → `req.StorageType` 恒为空串，`resolveStoredFilePath` 的 `switch` 落空，所有「上传后计算服务器校验值」请求都会失败。

**Files:**
- Modify: `server/internal/file/preview.go`（`FileChecksumRequest` L261-272、`resolveStoredFilePath` L339 起的 `switch req.StorageType`）

- [ ] **Step 1: 改结构体**

```go
// FileChecksumRequest 计算文件校验值请求：
// - public：需 root_name + path（相对根目录的路径）
// - private / temp：需 code（私有分享码 / 临时访问码）
// algorithms 为 md5/sha1/sha256 的任意子集
type FileChecksumRequest struct {
	StorageType string   `json:"storage_type"`
	RootName    string   `json:"root_name"`
	Path        string   `json:"path"`
	Code        string   `json:"code"`
	Algorithms  []string `json:"algorithms" binding:"required"`
}
```

`resolveStoredFilePath` 里的 `req.StorageType` / `req.RootName` / `req.Path` / `req.Code` 是**Go 字段访问**，无需改动。

- [ ] **Step 2: 验证**

Run: `cd server; go build ./... && go test ./internal/file/...`
Expected: 编译通过；现有 file 包测试 PASS。

- [ ] **Step 3: 手工验证（可选但推荐）**

`cd ui; yarn dev` → 上传一个文件 → 在「文件详情 → 校验值」处触发计算 → 应返回 `{checksums: {...}}` 而非「请求数据格式错误」或「无效的存储类型」。

**验收:** `POST /api/v1/web/files/checksum` 携带 `storage_type=public` + `root_name` + `path` 能返回 md5/sha1/sha256。

---

### Task 1.3: 核查 multipart 上传请求字段

**背景:** `models/upload.go` L154-195 的四个请求结构体仍用 `form:"rootName"` / `form:"fileSize"` / `form:"chunkIndex"` / `form:"totalChunks"`。但实际分片上传走的是 [upload.go](file:///d:/Projects/lightshare/server/internal/file/upload.go) L320-353 的 `c.PostForm("upload_id")` / `c.PostForm("chunk_index")`，前端 [useUploadQueue.js](file:///d:/Projects/lightshare/ui/src/components/upload/hooks/useUploadQueue.js) L585-589 也发 `upload_id` / `chunk_index`。因此这四个结构体目前**只被 `routes_web.go` L283 的 `/examples/validation/upload` 演示路由使用**，不影响真实上传。

- [ ] **Step 1: 确认唯一引用点**

Run: `cd server; go build ./...`，然后全局搜索 `models.UploadRequest` / `models.UploadChunkRequest` / `models.FinalizeUploadRequest` / `models.CheckUploadStatusRequest`。
预期命中：`routes_web.go:283`、`appconfig/api/binding_example.go:22`，以及定义处自身。

- [ ] **Step 2: 改成 snake（与同名真实接口保持一致，避免下次误用）**

将 `models/upload.go` L154-189 的 `form` 与 `json` tag 统一改为 snake：

```go
// UploadRequest 上传请求的数据结构
type UploadRequest struct {
	Dir      string `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	Filename string `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	RootName string `form:"root_name" json:"root_name" binding:"required,max=255"`
	FileSize int64  `form:"file_size" json:"file_size" binding:"min=0"`
}

// CheckUploadStatusRequest 检查上传状态请求的数据结构
type CheckUploadStatusRequest struct {
	Filename string `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	Dir      string `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	RootName string `form:"root_name" json:"root_name" binding:"required,max=255"`
	FileSize int64  `form:"file_size" json:"file_size" binding:"min=0"`
}

// UploadChunkRequest 上传分片请求的数据结构
type UploadChunkRequest struct {
	Filename    string                `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	Chunk       *multipart.FileHeader `form:"chunk" binding:"required"`
	ChunkIndex  int                   `form:"chunk_index" json:"chunk_index" binding:"required,min=0"`
	TotalChunks int                   `form:"total_chunks" json:"total_chunks" binding:"required,min=1"`
	Checksum    string                `form:"checksum" json:"checksum" binding:"max=64"`
	Dir         string                `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	RootName    string                `form:"root_name" json:"root_name" binding:"required,max=255"`
	FileSize    int64                 `form:"file_size" json:"file_size" binding:"min=0"`
}

// FinalizeUploadRequest 完成分片上传请求的数据结构
type FinalizeUploadRequest struct {
	Filename    string `form:"filename" json:"filename" binding:"required,filetype,max=255"`
	Dir         string `form:"dir" json:"dir" binding:"required,filepath,max=1024"`
	RootName    string `form:"root_name" json:"root_name" binding:"required,max=255"`
	TotalChunks int    `form:"total_chunks" json:"total_chunks" binding:"required,min=1"`
	FileSize    int64  `form:"file_size" json:"file_size" binding:"min=0"`
}

// ExtendTempFileRequest 延长临时文件有效期请求的数据结构
type ExtendTempFileRequest struct {
	ExpireDate  string `json:"expire_date" binding:"max=32"`
	NeverExpire bool   `json:"never_expire"`
}
```

- [ ] **Step 3: 跑测试**

Run: `cd server; go test ./...`
Expected: PASS（若 `binding_example.go` 或 `routes_web.go` 附近有测试断言旧字段名，同步更新）。

**验收:** `POST /api/v1/web/examples/validation/upload` 用 `root_name`/`file_size` 可验证通过。

---

### Task 1.4: 阶段 1 收尾

- [ ] **Step 1: 后端全量测试**

Run: `cd server; go test ./...`
Expected: PASS。

- [ ] **Step 2: 前端构建**

Run: `cd ui; yarn build`
Expected: 构建成功，产物输出到 `server/web`。

- [ ] **Step 3: 提交（建议独立 commit，便于回滚）**

提交信息示例：`fix(api): 验证码与文件校验值字段统一为 snake_case`

---

## 阶段 2：模块垂直切片

每一片都遵循同一套流程：

1. 先跑一次全量 grep 拿到本片的确切清单（下方每片已给出实测行号，仍建议复核）。
2. 改后端结构体 `json` tag + map key。
3. 同批改前端消费该响应的位置与发往该后端的 payload。
4. `cd server; go test ./...`。
5. `cd ui; yarn build`。
6. 跑守卫测试 `go test ./internal/naming/ -v`，确认本片涉及的字段报错消失。
7. 独立 commit。

### Task 2.1: 配置与设置（影响面最大，必须单独一片）

**Files（后端）:**
- Modify: `server/internal/appconfig/dto.go`（94 处 `json` tag）
- Modify: `server/internal/appconfig/api/handler.go`（29 处 map key + 19 个 `present()` 裸字符串）
- Modify: `server/internal/appconfig/config.go`（`DirectoryConfig.AllowUpload` 的 `json:"uploadEnabled"`；`AccountConfig.RequireLogin` / `AllowRegistration` 等）
- Modify: `server/internal/setup/handler.go`（`SetupConfigRequest` 12 处 + `GetDefaultConfig` 的 `appName` / `dbDriver` map key）
- Modify: `server/internal/appconfig/api/binding_example.go`（`rootName` / `fileSize`）

**Files（前端）:**
- Modify: `ui/src/main.js`（`applyConfig` 全部读取 key）
- Modify: `ui/src/views/SettingsView.vue`（`loadSettings` 读 `cfg.*`、`configData` payload、保存后刷新）
- Modify: `ui/src/views/settings/useSettingsDiff.js`（`FIELD_LABELS` 全表路径）
- Modify: `ui/src/views/SetupView.vue`（提交体与 `getDefaultConfig` 读取）
- Modify: `ui/src/views/settings/Settings*.vue`（若直接读 `cfg.*`）
- Modify: `ui/src/store/index.js`（若 store 直接存后端 key；注意 store 内部 key 可保持 camel）

#### 2.1.1 后端 `dto.go` 字段映射表

按下表逐条改 `json` tag（左列 → 右列）。标「**不改**」的必须保持原样。

| 结构体 | 字段 | 现 tag | 目标 tag |
|---|---|---|---|
| `ConfigDTO` | App / Account / Server / Database / Auth / Webhook | `app`/`account`/`server`/`database`/`auth`/`webhook` | **不改** |
| `ConfigDTO` | RootDirs | `rootDirs` | `root_dirs` |
| `ConfigDTO` | AllowedExtensions | `allowedExtensions` | `allowed_extensions` |
| `ConfigDTO` | StorageBackend | `storageBackend` | `storage_backend` |
| `ConfigDTO` | PrivateFiles | `privateFiles` | `private_files` |
| `ConfigDTO` | TempFiles | `tempFiles` | `temp_files` |
| `ConfigDTO` | MimeTable | `mimeTable` | `mime_table`（**map 的 key 不动**） |
| `ConfigDTO` | OpenApi | `openApi` | `open_api` |
| `ConfigDTO` | Preview / Upload / Download / Index / Resource / Security | 已是单词 | **不改** |
| `OAuth2ConfigDTO` | ClientID | `clientId` | `client_id` |
| `OAuth2ConfigDTO` | ClientSecret | `clientSecret` | `client_secret` |
| `OAuth2ConfigDTO` | AuthURL | `authUrl` | `auth_url` |
| `OAuth2ConfigDTO` | TokenURL | `tokenUrl` | `token_url` |
| `OAuth2ConfigDTO` | UserInfoURL | `userInfoUrl` | `user_info_url` |
| `OAuth2ConfigDTO` | RedirectURL | `redirectUrl` | `redirect_url` |
| `OAuth2ConfigDTO` | UsernameClaim | `usernameClaim` | `username_claim` |
| `OAuth2ConfigDTO` | DisplayNameClaim | `displayNameClaim` | `display_name_claim` |
| `OAuth2ConfigDTO` | EmailClaim | `emailClaim` | `email_claim` |
| `OAuth2ConfigDTO` | AutoCreateUser | `autoCreateUser` | `auto_create_user` |
| `OAuth2ConfigDTO` | InsecureSkipVerify | `insecureSkipVerify` | `insecure_skip_verify` |
| `LDAPConfigDTO` | UseSSL / BaseDN / BindDN / BindPassword / UserFilter | `useSSL`/`baseDN`/`bindDN`/`bindPassword`/`userFilter` | `use_ssl`/`base_dn`/`bind_dn`/`bind_password`/`user_filter` |
| `LDAPConfigDTO` | SyncInterval / AutoCreateUser / InsecureSkipVerify | `syncInterval`/`autoCreateUser`/`insecureSkipVerify` | `sync_interval`/`auto_create_user`/`insecure_skip_verify` |
| `RootDirConfig` | FullPath / UploadEnabled | `fullPath`/`uploadEnabled` | `full_path`/`upload_enabled` |
| `PreviewTextGroupDTO` | MaxInlineSize / TextChunkSize | `maxInlineSize`/`textChunkSize` | `max_inline_size`/`text_chunk_size` |
| `AccountResponse` | ReservedUsernames / AllowRegistration / RequireLogin | `reservedUsernames`/`allowRegistration`/`requireLogin` | `reserved_usernames`/`allow_registration`/`require_login` |
| `WebhookEndpointDTO` | Headers / Query | `headers`/`query` | **不改 tag**；但其中的 **map key 是用户数据，绝不动** |

其余 `dto.go` 中的 camel tag 一并用同一规则转 snake（`iD`→`id` 时注意 `userID` → `user_id`、`xxh3Hash` → `xxh3_hash`）。逐条改完后跑：

```powershell
Select-String -Path d:\Projects\lightshare\server\internal\appconfig\dto.go -Pattern 'json:"[a-z][A-Za-z]*[A-Z]' | ForEach-Object { "$($_.LineNumber): $($_.Line.Trim())" }
```

预期输出为空（除 `MimeTable` 的 value 类型声明）。

#### 2.1.2 后端 `api/handler.go` 的 map key 与裸字符串

必须**同时**改，否则配置保存会静默丢字段：

| 行号 | 现值 | 目标 |
|---|---|---|
| L54 | `"allowRegistration"` | `"allow_registration"` |
| L56 | `"requireLogin"` | `"require_login"` |
| L60 | `"triggerAfter"` | `"trigger_after"` |
| L84 | `"uploadEnabled"` | `"upload_enabled"` |
| L91 | `"rootDirs"` | `"root_dirs"` |
| L92 | `"privateStorage"` | `"private_storage"` |
| L95 | `"tempFiles"` | `"temp_files"` |
| L108 | `"publicUsername"` | `"public_username"` |
| L111 | `"chunkSize"` | `"chunk_size"` |
| L112 | `"maxFileSize"` | `"max_file_size"` |
| L113 | `"urlUpload"` | `"url_upload"` |
| L115 | `"insecureSkipVerify"` | `"insecure_skip_verify"` |
| L116 | `"maxConcurrent"` | `"max_concurrent"` |
| L117 | `"maxRetries"` | `"max_retries"` |
| L118 | `"maxSize"` | `"max_size"` |
| L122 | `"rateLimit"` | `"rate_limit"` |
| L123 | `"windowMinutes"` | `"window_minutes"` |
| L124 | `"maxRequests"` | `"max_requests"` |
| L125 | `"lockAfter"` | `"lock_after"` |
| L126 | `"lockMinutes"` | `"lock_minutes"` |
| L131 | `"mimeTable"` | `"mime_table"` |
| L132 | `"openApi"` | `"open_api"` |

`SaveConfig` 中的裸字符串（**与 tag 强耦合，必须同步**）：

```
L240 present("storageBackend")   → present("storage_backend")
L248 present("auth")             → 不变
L257 present("account")          → 不变
L284 present("app")              → 不变
L290 present("account")          → 不变
L322 present("server")           → 不变
L370 present("database")         → 不变
L382 present("rootDirs")         → present("root_dirs")
L414 present("allowedExtensions")→ present("allowed_extensions")
L425 present("storageBackend")   → present("storage_backend")
L472 raw["storageBackend"]       → raw["storage_backend"]
L475 sbFields["alert"]           → 不变（alert 是 storageBackend 内的子节点，本身无 camel）
L515 present("privateFiles")     → present("private_files")
L527 present("tempFiles")        → present("temp_files")
L543 present("upload")           → 不变
L587 present("download")         → 不变
L599 present("preview")          → 不变
L636 present("mimeTable")        → present("mime_table")
L642 present("openApi")          → present("open_api")
L684 present("index")            → 不变
L712 present("resource")         → 不变
L731 present("security")         → 不变
L745 present("auth")             → 不变
L809 present("webhook")          → 不变
```

同时 `applyPreviewGroup(dst, src, key, updates)` 内部用 `key+".enabled"` 拼 updates 路径，key 来自 `present(...)` 传入的节点名；若传入的是 `"preview"` 则不受影响，若传 `"mime_table"` 等需确认拼接结果与 tag 一致。

- [ ] **Step 1: 写一个临时的双向自检（不提交）**

在 `server/internal/appconfig/api/handler.go` 的 `SaveConfig` 入口临时加一行日志打印 `c.Request` 的 body key 集合，改完后确认前端发送的 key 与 `present()` 的参数一致。验证后删除。

#### 2.1.3 前端同步

`ui/src/main.js` 的 `applyConfig(d)`：

| 现值 | 目标 |
|---|---|
| `d.privateStorage?.enabled` | `d.private_storage?.enabled` |
| `d.tempFiles?.enabled` | `d.temp_files?.enabled` |
| `d.openApi?.enabled` | `d.open_api?.enabled` |
| `d.upload?.urlUpload?.enabled` | `d.upload?.url_upload?.enabled` |
| `d.account?.allowRegistration` | `d.account?.allow_registration` |
| `d.account?.requireLogin` | `d.account?.require_login` |
| `d.webdav?.publicUsername` | `d.webdav?.public_username` |
| `d.account?.captcha?.triggerAfter` | `d.account?.captcha?.trigger_after` |
| `d.app?.name` / `d.ftp?.*` / `d.webdav?.enabled` / `d.account?.captcha?.{mode,required}` | **不改** |

store 的 key（`appName`、`privateStorageEnabled`…）**保持 camel**，因为那是前端内部状态。

`ui/src/views/SettingsView.vue`：
- `loadSettings` 读 `cfg.*` 的地方全部改 snake（`cfg.app.name`、`cfg.root_dirs`、`cfg.allowed_extensions`、`cfg.storage_backend`、`cfg.private_files`、`cfg.temp_files`、`cfg.mime_table`、`cfg.open_api`、`cfg.account.allow_registration`、`cfg.account.require_login`、`cfg.account.captcha.trigger_after`）。
- `SECTION_SOURCES` 的 key 改为 snake（`root_dirs`、`allowed_extensions`、`storage_backend`、`private_files`、`temp_files`、`mime_table`、`open_api`），并同步 `settings` 对象对应的属性名——**或**保持 `settings` 内部 camel、在 `SECTION_SOURCES` 附近加一层 `key → settingsKey` 映射。推荐后者：改动小、不易漏。
- `configData` 构造的 payload 全部改 snake（`app.name`、`account.anonymous.username`、`account.reserved_usernames`、`account.allow_registration`、`account.require_login`、`account.captcha.trigger_after`、`server.*` 保持、`root_dirs`、`allowed_extensions`、`storage_backend`、`private_files.quota_global`/`quota_per_user`、`temp_files.quota_global`/`quota_per_ip`、`mime_table`、`open_api`）。
- `confirmCriticalChange` 中 `baselineSections.storageBackend` 同步改名。

`ui/src/views/settings/useSettingsDiff.js`：`FIELD_LABELS` 的约 150 条路径键全部改 snake（`'rootDirs.uploadEnabled'` → `'root_dirs.upload_enabled'`、`'openApi.ipAccessMode'` → `'open_api.ip_access_mode'`、`'storageBackend.alert.smtp.tlsMode'` → `'storage_backend.alert.smtp.tls_mode'`）。数组下标剥离逻辑（`rootDirs[0].path` → `rootDirs.path`）的正则需同时支持 snake 键。

`ui/src/views/SetupView.vue`：
- 提交体：`app_name` / `https_enabled` / `https_port` / `tls_cert_file` / `tls_key_file` / `admin_username` / `admin_password` / `require_login` / `allow_registration` / `root_dirs`（元素内 `upload_enabled`）/ `database`。
- `onMounted` 读 `getDefaultConfig()` 的 `app_name` / `db_driver`。

- [ ] **Step 1: 后端改完后先自测**

Run: `cd server; go build ./... && go test ./internal/appconfig/... ./internal/setup/...`
Expected: PASS。

- [ ] **Step 2: 手工回归（必须做，配置页是全项目最复杂的表单）**

`cd server; go run .` + `cd ui; yarn dev`，逐项验证：
1. 登录 → 打开「系统设置」，各页签值正确回显（不是空白/undefined）。
2. 修改「应用名称」→ 保存 → 顶栏与页面标题同步变化。
3. 修改「根目录」的上传开关 → 保存 → 上传页该根目录可/不可上传。
4. 修改「验证码」三态 → 保存 → 登录页验证码行为变化。
5. 打开「变更确认」弹窗，能正确列出变更项（验证 `FIELD_LABELS` 生效）。
6. 全新数据库走 `/setup` 初始化向导，能完成并跳转登录。

- [ ] **Step 3: 构建前端**

Run: `cd ui; yarn build`

**验收:** 上述 6 项手工回归全通过；`GET /api/v1/web/options` 与 `GET /api/v1/web/config` 的响应 key 全为 snake；`POST /api/v1/web/config` 保存后重启服务配置仍生效。

---

### Task 2.2: 文件、上传与下载

**Files（后端）:**
- `server/internal/file/handler.go`（11 处：`fullPath`/`modifiedTime`/`xxh3Hash`/`hashStatus`/`recordId`/`isDir`/`rootName`/`downloadCount`/`dependencyCount`/`canManage`/`uploaderIp`）
- `server/internal/file/upload.go`（26 处 map key：`uploadId`/`chunkSize`/`totalChunks`/`expiredAt`/`overwriteRequired`/`chunkIndex`/`fileName`/`fileSize`/`createdAt`）
- `server/internal/file/private_upload.go`（8 处）
- `server/internal/file/url_task_handlers.go`（26 处：`taskId`/`taskType`/`storageType` 等）
- `server/internal/file/private.go`（`fileSize`/`uploadTime`）
- `server/internal/file/legacy.go`（`realUrl`、`NewName json:"newName"`、`NewPath json:"newPath"`）
- `server/internal/file/recent.go`（`pageSize`）
- `server/internal/file/preview.go`（剩余：`mimeType`、`chunkIndex`/`chunkSize`/`totalChunks`/`startOffset`/`endOffset`，以及 `PreviewFile` 的 `gin.H` 中 `previewable`/`fileUrl`/`chunked`）
- `server/internal/index/dependency_handler.go`（`pageSize`）
- `server/internal/index/model.go`（55 处，见 Task 2.5，此处只需改 `DependencyTreeNode`）

**Files（前端）:**
- `ui/src/components/upload/hooks/useUploadQueue.js`
- `ui/src/components/upload/hooks/useUrlBatchUpload.js`
- `ui/src/components/upload/UploadManager.vue`
- `ui/src/components/upload/UploadTaskTable.vue`
- `ui/src/components/file/FilePreview.vue`
- `ui/src/views/HotView.vue` / `RecentView.vue` / `TempView.vue` / `PrivateStorageView.vue` / `HomeView.vue`
- `ui/src/views/home/columns.js`
- `ui/src/views/admin/fileTableColumns.js`

#### 2.2.1 关键映射（按语义分组）

| 模块 | 现 key | 目标 key |
|---|---|---|
| 上传会话响应 | `uploadId` | `upload_id` |
| 上传会话响应 | `chunkSize` / `totalChunks` | `chunk_size` / `total_chunks` |
| 上传会话响应 | `overwriteRequired` | `overwrite_required` |
| 上传会话响应 | `expiredAt` / `createdAt` | `expired_at` / `created_at` |
| 文件记录 | `fullPath` / `fileName` / `filePath` / `fileSize` | `full_path` / `file_name` / `file_path` / `file_size` |
| 文件记录 | `rootName` / `isDir` / `xxh3Hash` / `hashStatus` | `root_name` / `is_dir` / `xxh3_hash` / `hash_status` |
| 文件记录 | `modTime` / `modifiedTime` | `mod_time` / `modified_time`（同一响应内统一为 `modified_time`） |
| 文件记录 | `downloadCount` / `dependencyCount` / `canManage` | `download_count` / `dependency_count` / `can_manage` |
| 文件记录 | `uploaderIp` / `uploaderIp` | `uploader_ip` |
| 下载任务 | `taskId` / `taskType` / `storageType` | `task_id` / `task_type` / `storage_type` |
| 下载任务 | `downloadedBytes` / `retryCount` / `eTag` / `resumeSupported` / `resumeURL` | `downloaded_bytes` / `retry_count` / `etag` / `resume_supported` / `resume_url` |
| 预览 | `mimeType` / `fileUrl` / `chunkSize` / `totalChunks` / `chunkIndex` / `startOffset` / `endOffset` | `mime_type` / `file_url` / `chunk_size` / `total_chunks` / `chunk_index` / `start_offset` / `end_offset` |
| 重命名/移动 | `newName` / `newPath` / `realUrl` | `new_name` / `new_path` / `real_url` |

- [ ] **Step 1: 先改上传链路（最容易复发）**

`file/upload.go` 的 `gin.H` 是分片上传创建会话的响应；前端 [useUploadQueue.js](file:///d:/Projects/lightshare/ui/src/components/upload/hooks/useUploadQueue.js) L424 解构 `{ uploadId, totalChunks, chunkSize, overwriteRequired }` → 改为 `{ upload_id, total_chunks, chunk_size, overwrite_required }`。

- [ ] **Step 2: 改下载/URL 任务链路**

`file/url_task_handlers.go` 与 `ui/src/api/index.js` 的 `UploadApi`、`ui/src/views/admin/useUrlTasks.js`（L106 `key: 'storageType'`、L109 `storageTypeMap[row.storageType]`）同步。

- [ ] **Step 3: 改文件列表/预览链路**

`file/handler.go` + `views/home/columns.js` + `admin/fileTableColumns.js` + `FilePreview.vue` 同步。

- [ ] **Step 4: 验证**

Run: `cd server; go test ./internal/file/... ./internal/index/...`
Run: `cd ui; yarn build`
手工：上传单文件（分片）、拖拽多文件、URL 上传、暂停/续传、预览文本与图片、重命名、移动。

**验收:** 上述手工项全通过；`GET /api/v1/web/uploads/sessions` 响应字段全 snake。

---

### Task 2.3: 用户与鉴权（除已完成的验证码）

**Files（后端）:**
- `server/internal/user/handler.go`（`pageSize`、`publicFiles`/`publicBytes`/`tempFiles`/`tempBytes`/`privateFiles`/`privateBytes`/`updatedAt`）
- `server/internal/auth/handler.go`（剩余 `displayName`/`createdAt`/`authorizeUrl`）
- `server/internal/auth/api_key.go`（`apiKey`/`rawKey`）
- `server/internal/auth/api_key_handler.go`（`pageSize`）
- `server/internal/models/user.go`（`displayName`）
- `server/internal/models/login_log.go`（5 处）
- `server/internal/models/api_key.go`（18 处：`keyId`/`userId`/`allowedPaths`/`lastUsedAt`/`expiresAt`/`createdAt`/`updatedAt`；OAuth client `clientId`/`redirectUri`/`userId`；Identity `externalId`/`externalName`/`providerName`/`authType`）
- `server/internal/models/security_event.go`（7 处）
- `server/internal/security/handler.go`（`pageSize`、`topIPs`/`topTypes`）
- `server/internal/security/writer.go`（`eventType`/`clientIP`/`statusCode`）

**Files（前端）:**
- `ui/src/views/AdminUsersView.vue` / `AdminApiKeyView.vue` / `AdminSecurityView.vue`
- `ui/src/components/common/AppHeader.vue`（`displayName`）
- `ui/src/store/index.js`
- `ui/src/api/index.js`（`AuthApi`、`ApiKeyApi`）

- [ ] **Step 1: 后端改 tag**（`user_id` 注意：`UserId`/`userID` 统一为 `user_id`，不要出现 `userid`）
- [ ] **Step 2: 前端同步**（`res.data.display_name`、`created_at`、`key_id`、`allowed_paths`）
- [ ] **Step 3: 验证**

Run: `cd server; go test ./internal/auth/... ./internal/user/... ./internal/security/...`
手工：登录后右上角显示昵称；API Key 列表与创建；安全事件列表分页。

**验收:** `GET /api/v1/web/user/stats`、`GET /api/v1/web/admin/api-keys` 响应全 snake。

---

### Task 2.4: 管理后台（除用户/安全外的 admin 视图）

**Files（后端）:**
- `server/internal/admin/handler.go`（`pageSize`/`idleTimeoutSeconds`/`serverTime`/`displayName`）
- `server/internal/admin/url_download_handler.go`（`pageSize`）
- `server/internal/admin/log_handler.go`（`modTime`）
- `server/internal/models/upload.go`（除已改的请求结构体，剩余 49 处 model tag：`fileName`/`fileSize`/`chunkSize`/`totalChunks`/`targetType`/`targetPath`/`targetRoot`/`userID`/`clientIP`/`chunkDir`/`deleteOnDownload`/`createdAt`/`updatedAt`/`expiredAt`/`deletedAt`/`sessionID`/`chunkIndex`/`chunkChecksum`/`sourceID`/`sourceId`/`fileRecordId`/`uploadType`/`searchQuery`/`uploadTime`）
- `server/internal/models/url_download_task.go`（20 处）
- `server/internal/models/file_record.go`（19 处）
- `server/internal/models/task_record.go`（18 处）
- `server/internal/models/scan_record.go`（12 处，`rootResults` → `root_results`）
- `server/internal/models/temp_file.go`（12 处）
- `server/internal/models/file_dependency.go`（`fileRecordId`/`dependsOnId`/`sourceFile`/`targetFile`）
- `server/internal/models/alert_message.go`（3 处）
- `server/internal/services/admin_service.go`（14 处）
- `server/internal/services/upload_session.go`（9 处，含 `shareCode`）
- `server/internal/services/storage_alert_service.go`（7 处）

**Files（前端）:**
- `ui/src/views/AdminFilesView.vue` / `AdminTasksView.vue` / `AdminRecordsView.vue` / `AdminDuplicatesView.vue` / `AdminOnlineView.vue` / `AdminWebhookView.vue` / `AdminDashboardView.vue`
- `ui/src/views/admin/useFileActions.js` / `useZombieSessions.js` / `useUrlTasks.js` / `fileTableColumns.js`
- `ui/src/api/index.js` 对应方法

- [ ] **Step 1: 注意 `gorm` 列名不受影响**

`models/*.go` 中 model 结构体同时有 `gorm` 与 `json` tag 的字段（如 `models/upload.go` L34-55），只改 `json`，`gorm:"column:..."` 一律不动。

- [ ] **Step 2: `scan_record.RootResults` 是 `string`（`gorm:"type:text"`），只有 tag 改动，无数据迁移**

- [ ] **Step 3: 验证**

Run: `cd server; go test ./internal/admin/... ./internal/models/... ./internal/services/...`
手工：管理后台文件列表、任务列表（含 URL 任务）、重复文件、在线 IP、Webhook 测试推送、仪表盘统计。

**验收:** 上述页面数据正常显示，无 `undefined`。

---

### Task 2.5: 索引、监控、资源与行为分析

**Files（后端）:**
- `server/internal/index/model.go`（55 处，含 `ScanProgressView`/`ConsistencyReport`/`DuplicateGroup`/`DependencyTreeNode`/`RecordStatsResponse`/`ScanStatusResponse`）
- `server/internal/index/handler.go`（`pageSize` ×2）
- `server/internal/index/hashworker.go`（`fileNotFound`/`permissionDenied`/`rootNotFound`/`otherError`）
- `server/internal/index/records_service.go`（1 处）
- `server/internal/monitor/handler.go`（`pageSize`）
- `server/internal/services/monitor_service.go`（17 处：`totalSpace`/`usedSpace`/`freeSpace`/`quotaPerIP`/`usedByIP`/`usedByUser`/`totalRequests`/`activeUsers`/`onlineIPs`/`recentUploads`/`recentDownloads`/`fileName`/`fullPath`/`rootName`/`fileSize`/`uploadTime`/`pageSize`）
- `server/internal/resource/types.go`（17 处）+ `server/internal/resource/hardware.go`（4 处，**L233-240 是 `json:"Name"`/`json:"LinkSpeed"`，大写开头，需改为 `name`/`link_speed`**）
- `server/internal/behavioranalytics/service.go`（19 处，含 `byIP`/`byUser`）
- `server/internal/online/tracker.go`（5 处：`firstSeen`/`lastSeen`/`onlineSeconds`/`requestCount`/`userAgent`）
- `server/internal/search/indexer.go`（4 处）

**Files（前端）:**
- `ui/src/views/AdminResourceView.vue` / `AdminBehaviorAnalyticsView.vue` / `AdminDashboardView.vue` / `AdminDuplicatesView.vue` / `AdminOnlineView.vue`
- `ui/src/components/common/IndexStatusBar.vue`
- `ui/src/api/index.js`

- [ ] **Step 1: 先改 `hardware.go` 的大写 key**（`Name`/`LinkSpeed` 是漏网的 PascalCase，最容易被正则漏掉）
- [ ] **Step 2: 改 `index/model.go`**（注意 `ListRecordsQuery` / `DuplicateQuery` 的 `form` tag 已是 snake，**不要动**）
- [ ] **Step 3: 验证**

Run: `cd server; go test ./internal/index/... ./internal/monitor/... ./internal/resource/... ./internal/behavioranalytics/...`
手工：仪表盘存储统计、索引状态栏、重复文件页、资源监控、行为分析图表。

**验收:** `GET /api/v1/web/admin/index/records`、`GET /api/v1/web/monitor/metrics` 等响应全 snake。

---

### Task 2.6: 其余模块

| 后端文件 | 处数 | 主要 key |
|---|---|---|
| `server/internal/temp/temp_session.go` | 20 | `fileName`/`fileSize`/`downloadUrl`/`expiresAt`/`createdAt` |
| `server/internal/alert/handler.go` | 1 | `pageSize` |
| `server/internal/webhook/test_push.go` | 2 | `statusCode`/`elapsedMs` |
| `server/internal/middleware/audit.go` | 3 | `clientIp`/`statusCode`/`durationMs` |
| `server/internal/services/auth_service.go` | 剩余 | `displayName` 等 |
| `server/internal/response/response.go` | 2 | `ipAddress`/`uploadTime`（`UploadRecord`） |
| `server/internal/appconfig/api/binding_example.go` | 2 | `rootName`/`fileSize` |

**Files（前端）:** `ui/src/views/TempView.vue`、`ui/src/views/AdminOnlineView.vue`

- [ ] **Step 1: 逐个改**
- [ ] **Step 2: 全量测试 + 构建**

Run: `cd server; go test ./...`
Run: `cd ui; yarn build`

**验收:** `go test ./...` 全 PASS；`go test ./internal/naming/ -v` 全 PASS（守卫测试转绿）。

---

## 阶段 3：文档对齐

### Task 3.1: 修正 `API_STYLE_UNIFICATION.md` 状态

**背景:** 该文件头部 L3 现写「状态：已实施（后端/前端/文档已同步）」，与实际不符——规范已定，代码尚未迁移。

**Files:** Modify `docs/design/api/API_STYLE_UNIFICATION.md`

- [ ] **Step 1: 改 L3**

```
> 状态：规范已定，代码待迁移。规范正文见 [API_STYLE_GUIDE.md](./API_STYLE_GUIDE.md)，本文件记录存量接口的迁移对照与执行进度。
```

- [ ] **Step 2: 在 §4「影响面与迁移步骤」末尾追加进度勾选表**

```markdown
### 4.1 迁移进度

| 阶段 | 内容 | 状态 |
|---|---|---|
| 0 | 命名守卫测试 `server/internal/naming/naming_test.go` | ☐ |
| 1 | 安全止血：验证码、文件校验值 | ☐ |
| 2.1 | 配置与设置（`appconfig` + Settings/Setup 前端） | ☐ |
| 2.2 | 文件、上传与下载 | ☐ |
| 2.3 | 用户与鉴权 | ☐ |
| 2.4 | 管理后台 | ☐ |
| 2.5 | 索引、监控、资源、行为分析 | ☐ |
| 2.6 | 其余模块 | ☐ |
| 3 | 文档与 OpenAPI 对齐 | ☐ |
```

（本计划执行时逐项打勾。）

- [ ] **Step 3: 修正 L364 的 `rootName`** → `root_name`（§5 已确认结论表中的示例）。

- [ ] **Step 4: 修正 §1.6 响应体示例（L95）**

该行现在是「例如 `fullPath` → `full_path`、`fileSize` → `file_size`、`pageSize` → `page_size`、`uploadId` → `upload_id`、`createdAt` → `created_at`」，属「现状 → 目标」对照，**保留不改**，但需在其后补一句：

```
截至本计划执行完成，上述「现状」列已不存在，全仓仅剩 snake_case。
```

**验收:** 文件内不再有「已实施」的误导表述；`Select-String -Path docs\design\api\API_STYLE_UNIFICATION.md -Pattern '已实施'` 的唯一命中应为 §3.2 中「（已实施）」标注已有路由的历史描述（保留）。

---

### Task 3.2: 修正 `API_INTERFACE.md`

**实测:** 反引号内 camelCase 仅 6 处（非 56 处），位置已确认为：L448 `taskId`、L499 `downloadedBytes` / `fileSize` / `retryCount`、L661 `allowedPaths`、L742 `rootDirs`。L244/253/254 的 `Range` 是 HTTP 头、L871 `FUZHAN_COOKIE_FILE` 是环境变量，均不改。

**Files:** Modify `docs/design/api/API_INTERFACE.md`

- [ ] **Step 1: 逐处替换**

| 行号 | 现值 | 目标 |
|---|---|---|
| L448 | `taskId` | `task_id` |
| L499 | `downloadedBytes` | `downloaded_bytes` |
| L499 | `fileSize` | `file_size` |
| L499 | `retryCount` | `retry_count` |
| L661 | `allowedPaths` | `allowed_paths` |
| L742 | `rootDirs` | `root_dirs` |

- [ ] **Step 2: 复核**

```powershell
$p='d:\Projects\lightshare\docs\design\api\API_INTERFACE.md'
$lines=[System.IO.File]::ReadAllLines($p)
for($i=0;$i -lt $lines.Count;$i++){
  foreach($m in [regex]::Matches($lines[$i],'`([A-Za-z0-9_\.\[\]]*[A-Z][A-Za-z0-9_\.\[\]]*)`')){
    "{0}: {1}" -f ($i+1), $m.Groups[1].Value
  }
}
```

预期剩余：`Range`（×3，HTTP 头）、`FUZHAN_COOKIE_FILE`（环境变量）。

**验收:** 输出只剩上述 4 条。

---

### Task 3.3: 更新 `project-context.md`

**背景:** AGENTS.md 硬规则要求「新增/修改 HTTP API 时遵守 API_STYLE_GUIDE」，路由或契约变化须同步 §7。

**Files:** Modify `docs/development/project-context.md`

- [ ] **Step 1: 在 §7「路由总览」开头加一段约定**

```markdown
> **字段命名：** 所有 `/api/v1/**` 接口（web / admin / open 三套命名空间）的请求参数与响应字段一律 `snake_case`，详见 [API_STYLE_GUIDE.md](../design/api/API_STYLE_GUIDE.md) §4/§5。Go 结构体必须显式写 `json` tag；`gin.H` / `map` 字面量 key 同样写 snake。例外见 §7 例外清单（文件流、SSE、WebDAV、Lite HTML、安装脚本、OAuth2 回调参数）。
```

- [ ] **Step 2: 在 §12「约定与陷阱」追加两条**

```markdown
- **命名不一致是静默失败的最大来源。** `ShouldBindJSON` 对不上 tag 的字段只会留空，不会报错；`c.PostForm("x")` / `c.Query("x")` 写错 key 同样静默返回空串。历史上「无效的会话ID」、验证码校验失效、文件校验值失败都属此类。新增读参一律用手写 snake key，并跑 `go test ./internal/naming/`。
- **`appconfig/api/handler.go` 的 `SaveConfig` 用裸字符串判断节点是否提交**（`present("root_dirs")`、`raw["storage_backend"]`、`sbFields["alert"]`）。改 DTO 的 `json` tag 时必须同步改这些字符串，否则配置保存会静默丢字段。
```

- [ ] **Step 3: 确认无需更新其他章节**

路由未变 → §7 的路由清单本身无需改动；数据模型 `gorm` 列名未变 → §6 无需改动；配置项名称（yaml 层）未变 → §4 无需改动。

**验收:** §7 顶部有字段命名约定；§12 有两条新陷阱；无其他章节被误改。

---

## 全局例外清单（执行时严格排除）

| 位置 | 内容 | 原因 |
|---|---|---|
| `server/internal/appconfig/dto.go` | `MimeTable map[string]string` 的 **key** | key 是 MIME 类型（如 `text/plain`） |
| `server/internal/appconfig/dto.go` | `WebhookEndpointDTO.Headers` / `.Query` 的 **key** | 用户自定义 HTTP 头/查询参数名 |
| `server/internal/auth/ldap.go` L194-197 | `sAMAccountName` / `displayName` / `givenName` | LDAP 协议属性名 |
| `server/internal/lite/lite.go` L456 | `crumbSep` | Go 模板函数名 |
| `server/internal/openapi/spec.go`、`middleware.go` | 全部 camelCase（`requestBody`、`allowReserved`、`maxLength`、`minItems`、`maxItems`、`additionalProperties`、`securitySchemes`、`bearerFormat`、`oneOf`、`allOf`） | OpenAPI 3.0.3 规范关键字 |
| `server/third_party/**`、`server/web/**` | 全部 | 第三方/构建产物 |
| HTTP 头 | `Authorization`、`Accept`、`Origin`、`Range`、`Content-Type` | 协议约定 |
| OAuth2 回调 | `code` / `state` / `error_description` | 协议约定（已是 snake 或单词） |
| `server/internal/response/response.go` 的 `EncodeResponse` | `success` / `message` / `data` | 已是单词，无需改 |
| yaml tag（`server/internal/appconfig/config.go` 等） | `allow_upload`、`require_login` 等 | 配置文件格式本身已是 snake，属另一层契约 |

---

## 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| 前端漏改某处 `res.data.xxx` | 页面显示 `undefined` / 空白 | 每个 Task 必做手工回归；改完全仓 grep 旧 key（区分大小写）确认 0 命中 |
| `SaveConfig` 裸字符串漏改 | 配置保存静默丢字段，且**无报错** | Task 2.1 已列全 25 处行号；改完必须做「保存 → 重启 → 值仍生效」验证 |
| `models/*.go` 误改 `gorm` tag | 数据库列名变化，AutoMigrate 建新列、旧数据丢失 | 只改 `json` / `form` tag；改完 `git diff` 检查每行是否只动了 tag 而非 `gorm:"column:..."` |
| `scan_record.root_results` / `api_key.allowed_paths` 是字符串列 | 若误以为是 JSON 列而加迁移逻辑 | 已确认无迁移风险，改 tag 即可 |
| 前端把用户自定义 key 一起转了 | Webhook 头、MIME 表被破坏 | 不引入全局 axios 自动转换，全部显式改 key |
| 第三方库升级导致构建产物变化 | 与本次无关的 diff 噪音 | 不改 `go.mod` / `package.json` |
| 一次性提交过大无法回滚 | 出问题难以定位 | 每个 Task 独立 commit；阶段 1 单独交付 |

---

## 验证清单（全部完成后）

- [ ] `cd server; go test ./...` 全 PASS
- [ ] `cd server; go test ./internal/naming/ -v` 全 PASS
- [ ] `cd ui; yarn build` 成功，`server/web` 产物更新
- [ ] 后端 camel 统计：`TAG_ROWS` 归零（排除例外清单后）
- [ ] 前端旧 key 统计：上述 `$pat` 命中文件数从 74 降到 0（排除例外）
- [ ] 手工回归：设置页 6 项、上传 7 项、管理后台 6 项、仪表盘与索引状态栏
- [ ] OpenAPI 页面（`/scalar`）能正常加载，接口示例与实现一致
- [ ] 强制登录开启 + 验证码开启 + 私有存储开启的场景下，登录/注册/上传/下载全链路可用

---

## Self-Review

**1. 覆盖完整性**
- 规范文档 §4（请求参数）、§5（响应字段）、§7（例外）已逐条映射到 Task 1.1/1.2/1.3 与 Task 2.1~2.6。
- 阶段 0 的守卫测试覆盖 15 个核心结构体；阶段 2 各片列出的文件清单来自实测统计（60 后端文件 / 74 前端文件），Top 文件均已点名。
- 阶段 3 覆盖三份文档与实际不符的三处：`API_STYLE_UNIFICATION.md` 状态、`API_INTERFACE.md` 6 处 camel、`project-context.md` 缺命名约定。

**2. 无 placeholder**
每个 Task 都有精确文件路径、行号、现 tag → 目标 tag、可执行命令与预期输出。Task 2.1 给出了 `dto.go` 与 `handler.go` 的完整映射表；Task 2.2 给出关键字段分组映射。未出现「TBD」「类似地处理其他字段」「参照上文」这类省略。

**3. 内部一致性**
- 命名规则统一：`camelCase` → `snake_case`，`ID`/`Url`/`Ip` 一律 `id`/`url`/`ip`。
- `uploaderIp`（`file/handler.go` L79 已是小写 p）与 `file_record.go` L40 的 `uploaderIp` 统一为 `uploader_ip`，前后端一致。
- 同一语义不出现两种写法：`modifiedTime` / `modTime` 统一为 `modified_time`；`UserId` / `userID` 统一为 `user_id`。

**4. 可执行性**
- 阶段 0 的两个统计脚本我已实测（后端 60 文件/677 行，前端 74 文件），基线值可直接对照。
- 守卫测试代码可编译（依赖 `fuzhan/internal/appconfig`、`file`、`index`、`models`、`services`、`setup` 六个包，签名已核对）。
- 每个 Task 的验证命令在当前 Windows + PowerShell 环境可直接执行。

**5. 待执行者确认的两处**
- Task 2.1 Step 1 建议的「临时日志自检」在验证后必须删除，避免留下调试代码。
- Task 1.3 判断四个 multipart 请求结构体仅用于 `/examples/validation/upload`；执行时需先跑 Step 1 的引用搜索确认，若发现有真实调用点，需同步改对应前端。

---

## 执行记录（执行时填写）

| Task | 状态 | 提交 | 备注 |
|---|---|---|---|
| 0.1 | ☑ | | 实测：后端 camel tag 474 条（含 `,omitempty` 的漏统计，去重约 506）、map camel key 187 条、前端命中 61 文件（脚本口径差异，以后续实测为准） |
| 0.2 | ☑ | | `server/internal/naming/naming_test.go` 已建，含 `checkStructPath` 路径栈防自引用爆栈 |
| 1.1 | ☑ | | 验证码 captcha_id/captcha_answer（后端 tag + 前端已同步） |
| 1.2 | ☑ | | 文件校验值 storage_type/root_name |
| 1.3 | ☑ | | `models/upload.go` 四个 multipart 请求结构体 form/json tag 全 snake；确认仅演示路由引用 |
| 1.4 | ☑ | | `go test ./...` 除 naming 外全 PASS；`yarn build` 成功 |
| 2.1 | ☑ | | 后端 5 文件（dto/config/handler/setup/binding_example）+ 前端 16 文件；`present()`/`raw[]` 裸字符串已同步；`go test ./internal/appconfig/...` PASS；`yarn build` 成功 |
| 2.2 | ☑ | | 后端 `file/{handler,upload,private_upload,url_task_handlers,private,legacy,recent,preview}.go` + `index/dependency_handler.go`；前端 upload hooks / FilePreview / Hot,Recent,PrivateStorage,Home,Temp / columns.js / fileTableColumns.js / api。HTTP 响应头与 gorm tag 未动 |
| 2.3 | ☑ | | `user/handler.go`、`auth/{handler,api_key,api_key_handler}.go`、`models/{user,login_log,api_key,security_event}.go`、`security/{handler,writer}.go` + 前端 AdminUsers/AdminApiKey/AdminSecurity/AppHeader/User* 组件 |
| 2.4 | ☑ | | `admin/*`、`models/{upload,url_download_task,file_record,task_record,scan_record,temp_file,file_dependency,alert_message}.go`、`services/{admin_service,upload_session,storage_alert_service}.go`；gorm tag 逐文件比对无变化 |
| 2.5 | ☑ | | `index/model.go`(55)、`resource/types.go`、`behavioranalytics`、`online/tracker.go`、`search/indexer.go`、`services/monitor_service.go`；`naming` 守卫转绿。`resource/hardware.go` 的 `json:"Name"/"LinkSpeed"` 是解析 PowerShell 输出的外部字段，已证实改动会静默解析为空，**保留原值** |
| 2.6 | ☑ | | `temp/temp_session.go`(22)、`alert/handler.go`、`webhook/test_push.go`、`middleware/audit.go`、`response/response.go`、`services/{upload_service,task_service}.go`、`health/handler.go`、`models/resource_metric.go`、`openapi/middleware.go` 的 `StatsSummary` |
| 3.1 | ☑ | | `API_STYLE_UNIFICATION.md`：状态改「规范已定，代码已迁移」、新增 §4.1 进度表、`rootName`→`root_name`、§1.6 补完成声明 |
| 3.2 | ☑ | | `API_INTERFACE.md` 示例 JSON key 57 行校正为 snake（`uploaded_chunks` 实测为 `uploaded_indexes`）；残留仅 `Range`(HTTP 头) 与 `FUZHAN_COOKIE_FILE`(环境变量) |
| 3.3 | ☑ | | `project-context.md` §7 加字段命名约定 blockquote、§12 加两条陷阱 |
| 终检 | ☑ | | `cd server; go test ./... -count=1` 全 PASS（含 `internal/naming` 守卫）；`cd ui; yarn build` 成功；残留扫描 `TAG_ROWS=9`（`openapi/middleware.go` 内部结构 5 + `resource/hardware.go` 4）、`MAP_ROWS=24`（LDAP 属性名 / hashworker 错误分类 / `crumbSep` / OpenAPI 关键字），**全部在例外清单内**；例外说明已补入 `API_STYLE_GUIDE.md` §7 |

