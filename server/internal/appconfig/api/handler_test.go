package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	appconfig "fuzhan/internal/appconfig"

	"github.com/gin-gonic/gin"
)

// minimalTestConfigPath 在临时目录写入一份最小可用的基础 YAML，并把它设为
// appconfig.ConfigPath，让 SaveConfig 末尾的 updates.Apply 能读取并回写。
func minimalTestConfigPath(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	cfgPath := filepath.Join(tmp, "test.yaml")
	if err := os.WriteFile(cfgPath, []byte("app:\n  name: 浮栈\n"), 0o644); err != nil {
		t.Fatalf("写入测试配置文件失败: %v", err)
	}
	return cfgPath
}

// postConfig 构造一个带 body 的 gin 上下文并调用 SaveConfig。
func postConfig(t *testing.T, path, body string, h *Handler) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	h.SaveConfig(c)
	return w
}

// TestValidateLDAPConfig 覆盖 LDAP 启用条件与端口范围的保存前校验。
func TestValidateLDAPConfig(t *testing.T) {
	cases := []struct {
		name string
		dto  appconfig.LDAPConfigDTO
		want string
	}{
		{"未启用可留空", appconfig.LDAPConfigDTO{Enabled: false}, ""},
		{"启用缺地址", appconfig.LDAPConfigDTO{Enabled: true, BaseDN: "dc=example,dc=com"}, "启用 LDAP 时必须填写服务器地址"},
		{"启用缺 BaseDN", appconfig.LDAPConfigDTO{Enabled: true, Host: "ldap.example.com"}, "启用 LDAP 时必须填写 Base DN"},
		{"端口越界", appconfig.LDAPConfigDTO{Enabled: false, Port: 70000}, "LDAP 端口必须在 1-65535 之间"},
		{"完整配置", appconfig.LDAPConfigDTO{Enabled: true, Host: "ldap.example.com", BaseDN: "dc=example,dc=com", Port: 389}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validateLDAPConfig(tc.dto); got != tc.want {
				t.Errorf("validateLDAPConfig() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestSaveConfig_PersistsSecurity 验证安全配置（信任代理头 / CORS）经
// SaveConfig 写入 GlobalConfig 并持久化到配置文件，覆盖 security 节点保存链路。
func TestSaveConfig_PersistsSecurity(t *testing.T) {
	// 记录并恢复全局状态，避免污染同一包内其它测试
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	appconfig.GlobalConfig = appconfig.Config{} // 关闭私有/临时存储，跳过目录创建副作用

	body := `{"security":{"trustProxy":true,"allowedOrigins":["https://a.example.com","https://b.example.com"]}}`
	postConfig(t, "/api/v1/web/config", body, &Handler{})

	if !appconfig.GlobalConfig.Security.TrustProxy {
		t.Error("SaveConfig 后 GlobalConfig.Security.TrustProxy = false, want true")
	}
	want := []string{"https://a.example.com", "https://b.example.com"}
	if got := appconfig.GlobalConfig.Security.AllowedOrigins; !equalStrings(got, want) {
		t.Errorf("SaveConfig 后 AllowedOrigins = %v, want %v", got, want)
	}

	out, err := os.ReadFile(appconfig.ConfigPath)
	if err != nil {
		t.Fatalf("读取持久化配置失败: %v", err)
	}
	yamlText := string(out)
	if !strings.Contains(yamlText, "trust_proxy: true") {
		t.Errorf("持久化 YAML 缺少 security.trust_proxy 节点: %s", yamlText)
	}
	if !strings.Contains(yamlText, "a.example.com") || !strings.Contains(yamlText, "b.example.com") {
		t.Errorf("持久化 YAML 缺少 allowed_origins 列表: %s", yamlText)
	}
}

// TestSaveConfig_ClearsSecurity 验证安全配置可被显式清空（置 trust_proxy 为
// false、来源为空列表），确保「清空即不限制」的语义能正确落盘。
func TestSaveConfig_ClearsSecurity(t *testing.T) {
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	// 预置一套旧的安全配置，模拟历史值
	appconfig.GlobalConfig = appconfig.Config{
		Security: appconfig.SecurityConfig{
			TrustProxy:     true,
			AllowedOrigins: []string{"https://old.example.com"},
		},
		Storage: appconfig.StorageConfig{
			Temp: appconfig.TempConfig{Enabled: false},
		},
	}

	body := `{"security":{"trustProxy":false,"allowedOrigins":[]}}`
	postConfig(t, "/api/v1/web/config", body, &Handler{})

	if appconfig.GlobalConfig.Security.TrustProxy {
		t.Error("SaveConfig 清空后 TrustProxy = true, want false")
	}
	if len(appconfig.GlobalConfig.Security.AllowedOrigins) != 0 {
		t.Errorf("SaveConfig 清空后 AllowedOrigins = %v, want empty", appconfig.GlobalConfig.Security.AllowedOrigins)
	}
}

// TestSaveConfig_SecurityAbsent_KeepsValues 验证前端只提交部分节点（此处不带
// security）时，安全配置不会被误重置为默认，这是 present() 门禁的核心保证。
func TestSaveConfig_SecurityAbsent_KeepsValues(t *testing.T) {
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	appconfig.GlobalConfig = appconfig.Config{
		Security: appconfig.SecurityConfig{
			TrustProxy:     true,
			AllowedOrigins: []string{"https://keep.example.com"},
		},
		Storage: appconfig.StorageConfig{
			Temp: appconfig.TempConfig{Enabled: false},
		},
	}

	// 只提交 app 节点名，刻意不含 security
	postConfig(t, "/api/v1/web/config", `{"app":{"name":"浮栈"}}`, &Handler{})

	if !appconfig.GlobalConfig.Security.TrustProxy {
		t.Error("未提交 security 时 TrustProxy 被重置 = false, want 保留 true")
	}
	if want := []string{"https://keep.example.com"}; !equalStrings(appconfig.GlobalConfig.Security.AllowedOrigins, want) {
		t.Errorf("未提交 security 时 AllowedOrigins = %v, want 保留 %v", appconfig.GlobalConfig.Security.AllowedOrigins, want)
	}
}

// TestSaveConfig_PreviewGroups 验证预览分组配置经 SaveConfig 写入 GlobalConfig 并持久化：
// 提交的分组按值生效，未提交的分组沿用当前生效值（指针字段语义）。
func TestSaveConfig_PreviewGroups(t *testing.T) {
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	appconfig.GlobalConfig = appconfig.Config{}

	body := `{"preview":{"enabled":false,"groups":{` +
		`"image":{"enabled":false,"mimes":"image/png","exts":"png"},` +
		`"text":{"enabled":true,"mimes":"text/plain","exts":"txt","maxInlineSize":"2M","textChunkSize":"200K"}}}}`
	postConfig(t, "/api/v1/web/config", body, &Handler{})

	pv := appconfig.GlobalConfig.Preview
	if pv.IsEnabled() {
		t.Error("SaveConfig 后 Preview.IsEnabled() = true, want false")
	}
	if pv.IsGroupEnabled("image") {
		t.Error("image 分组开关 = true, want false")
	}
	if got := pv.GroupMimes("image"); got != "image/png" {
		t.Errorf("image 分组 Mimes = %q, want image/png", got)
	}
	if got := pv.GroupExts("image"); got != "png" {
		t.Errorf("image 分组 Exts = %q, want png", got)
	}
	if got := pv.TextMaxInlineSize(); got != "2M" {
		t.Errorf("text 分组 MaxInlineSize = %q, want 2M", got)
	}
	if got := pv.TextChunkSize(); got != "200K" {
		t.Errorf("text 分组 TextChunkSize = %q, want 200K", got)
	}
	// 未提交的 audio 分组：开关视为开启，列表回退内置默认表
	if !pv.IsGroupEnabled("audio") {
		t.Error("未提交的 audio 分组开关 = false, want 默认开启")
	}
	if pv.GroupMimes("audio") == "" {
		t.Error("未提交的 audio 分组 Mimes 为空, want 回退内置默认表")
	}

	out, err := os.ReadFile(appconfig.ConfigPath)
	if err != nil {
		t.Fatalf("读取持久化配置失败: %v", err)
	}
	yamlText := string(out)
	for _, want := range []string{"mimes: image/png", "max_inline_size: 2M", "text_chunk_size: 200K"} {
		if !strings.Contains(yamlText, want) {
			t.Errorf("持久化 YAML 缺少 %q 节点: %s", want, yamlText)
		}
	}
}

// TestGetOptions_CaptchaAndNoAllowedIPRanges 验证公开 options 的 account.captcha
// 含 mode/triggerAfter/required，且 upload.urlUpload 不再暴露 allowedIPRanges。
func TestGetOptions_CaptchaAndNoAllowedIPRanges(t *testing.T) {
	prevCfg := appconfig.GlobalConfig
	defer func() { appconfig.GlobalConfig = prevCfg }()
	appconfig.GlobalConfig = appconfig.Config{
		Account: appconfig.AccountConfig{
			Captcha: appconfig.CaptchaConfig{Mode: "always", TriggerAfter: 4},
		},
		Upload: appconfig.UploadConfig{
			URLUpload: appconfig.URLUploadConfig{AllowedIPRanges: []string{"1.2.3.4/24"}},
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/web/options", nil)
	(&Handler{}).GetOptions(c)

	var resp struct {
		Data struct {
			Account map[string]interface{} `json:"account"`
			Upload  struct {
				URLUpload map[string]interface{} `json:"urlUpload"`
			} `json:"upload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析 options 响应失败: %v", err)
	}

	captcha, ok := resp.Data.Account["captcha"].(map[string]interface{})
	if !ok {
		t.Fatalf("options.account.captcha 缺失或类型错误: %v", resp.Data.Account["captcha"])
	}
	if captcha["mode"] != "always" {
		t.Errorf("options.account.captcha.mode = %v, want always", captcha["mode"])
	}
	if captcha["triggerAfter"] != float64(4) {
		t.Errorf("options.account.captcha.triggerAfter = %v, want 4", captcha["triggerAfter"])
	}
	// mode=always 时 required 恒为 true（与本 IP 失败次数无关）
	if captcha["required"] != true {
		t.Errorf("options.account.captcha.required = %v, want true", captcha["required"])
	}
	if _, exists := resp.Data.Upload.URLUpload["allowedIPRanges"]; exists {
		t.Error("options.upload.urlUpload 不应再包含 allowedIPRanges")
	}
}

// TestSaveConfig_PersistsCaptcha 验证 account.captcha 经 SaveConfig 写入 GlobalConfig
// 并通过 account.captcha.mode / account.captcha.trigger_after 持久化到配置文件。
func TestSaveConfig_PersistsCaptcha(t *testing.T) {
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	defer func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	}()
	appconfig.GlobalConfig = appconfig.Config{}

	body := `{"account":{"captcha":{"mode":"after_n","triggerAfter":3}}}`
	postConfig(t, "/api/v1/web/config", body, &Handler{})

	got := appconfig.GlobalConfig.Account.Captcha
	if got.Mode != "after_n" || got.TriggerAfter != 3 {
		t.Errorf("SaveConfig 后 Captcha = %+v, want mode=after_n triggerAfter=3", got)
	}

	out, err := os.ReadFile(appconfig.ConfigPath)
	if err != nil {
		t.Fatalf("读取持久化配置失败: %v", err)
	}
	yamlText := string(out)
	for _, want := range []string{"captcha:", "mode: after_n", "trigger_after: 3"} {
		if !strings.Contains(yamlText, want) {
			t.Errorf("持久化 YAML 缺少 %q 节点: %s", want, yamlText)
		}
	}
}

// TestSaveConfig_CaptchaTriggerAfterOnlyPositive 验证 trigger_after 仅在 >0 时写入：
// 0/负数保留原值且不产生 account.captcha.trigger_after 更新（持久化 YAML 中无该键）。
func TestSaveConfig_CaptchaTriggerAfterOnlyPositive(t *testing.T) {
	cases := []struct {
		name       string
		trigger    int
		wantAfter  int
		wantHasKey bool
	}{
		{"零值保留原值", 0, 4, false},
		{"负值保留原值", -1, 4, false},
		{"正值写入", 3, 3, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prevPath := appconfig.ConfigPath
			prevCfg := appconfig.GlobalConfig
			prevCaptcha := appconfig.GlobalConfig.Account.Captcha
			appconfig.ConfigPath = minimalTestConfigPath(t)
			t.Cleanup(func() {
				appconfig.ConfigPath = prevPath
				appconfig.GlobalConfig = prevCfg
				appconfig.GlobalConfig.Account.Captcha = prevCaptcha
			})
			appconfig.GlobalConfig = appconfig.Config{
				Account: appconfig.AccountConfig{
					Captcha: appconfig.CaptchaConfig{Mode: "disabled", TriggerAfter: 4},
				},
				Storage: appconfig.StorageConfig{
					Temp: appconfig.TempConfig{Enabled: false},
				},
			}

			body := `{"account":{"captcha":{"mode":"after_n","triggerAfter":` + strconv.Itoa(tc.trigger) + `}}}`
			postConfig(t, "/api/v1/web/config", body, &Handler{})

			got := appconfig.GlobalConfig.Account.Captcha
			if got.Mode != "after_n" {
				t.Errorf("Captcha.Mode = %q, want after_n", got.Mode)
			}
			if got.TriggerAfter != tc.wantAfter {
				t.Errorf("Captcha.TriggerAfter = %d, want %d", got.TriggerAfter, tc.wantAfter)
			}

			out, err := os.ReadFile(appconfig.ConfigPath)
			if err != nil {
				t.Fatalf("读取持久化配置失败: %v", err)
			}
			yamlText := string(out)
			if tc.wantHasKey {
				if !strings.Contains(yamlText, "trigger_after: 3") {
					t.Errorf("持久化 YAML 缺少 trigger_after: 3: %s", yamlText)
				}
			} else if strings.Contains(yamlText, "trigger_after") {
				t.Errorf("triggerAfter=%d 时不应写入 account.captcha.trigger_after: %s", tc.trigger, yamlText)
			}
		})
	}
}

// TestGetOptions_CaptchaRequiredDisabled 验证 mode=disabled 时公开 options 的
// captcha.required 为 false，与 always 用例互补，避免只测 true 的假阳性。
func TestGetOptions_CaptchaRequiredDisabled(t *testing.T) {
	prevCfg := appconfig.GlobalConfig
	t.Cleanup(func() { appconfig.GlobalConfig = prevCfg })
	appconfig.GlobalConfig = appconfig.Config{
		Account: appconfig.AccountConfig{
			Captcha: appconfig.CaptchaConfig{Mode: "disabled", TriggerAfter: 3},
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/web/options", nil)
	(&Handler{}).GetOptions(c)

	var resp struct {
		Data struct {
			Account struct {
				Captcha struct {
					Mode         string `json:"mode"`
					TriggerAfter int    `json:"triggerAfter"`
					Required     bool   `json:"required"`
				} `json:"captcha"`
			} `json:"account"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析 options 响应失败: %v", err)
	}
	if resp.Data.Account.Captcha.Mode != "disabled" {
		t.Errorf("options.account.captcha.mode = %q, want disabled", resp.Data.Account.Captcha.Mode)
	}
	if resp.Data.Account.Captcha.TriggerAfter != 3 {
		t.Errorf("options.account.captcha.triggerAfter = %d, want 3", resp.Data.Account.Captcha.TriggerAfter)
	}
	if resp.Data.Account.Captcha.Required {
		t.Error("mode=disabled: options.account.captcha.required = true, want false")
	}
}

// TestSaveConfig_CaptchaModeNormalized 验证 captcha.mode 大小写变体（afterN / AFTER_N / 带空白）
// 保存后被归一化为规范值 after_n。
func TestSaveConfig_CaptchaModeNormalized(t *testing.T) {
	cases := []string{"afterN", "AFTER_N", " After_N "}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			prevPath := appconfig.ConfigPath
			prevCfg := appconfig.GlobalConfig
			appconfig.ConfigPath = minimalTestConfigPath(t)
			t.Cleanup(func() {
				appconfig.ConfigPath = prevPath
				appconfig.GlobalConfig = prevCfg
			})
			appconfig.GlobalConfig = appconfig.Config{}

			body := `{"account":{"captcha":{"mode":"` + raw + `"}}}`
			postConfig(t, "/api/v1/web/config", body, &Handler{})

			if got := appconfig.GlobalConfig.Account.Captcha.Mode; got != appconfig.CaptchaModeAfterN {
				t.Errorf("Captcha.Mode = %q, want %q", got, appconfig.CaptchaModeAfterN)
			}
		})
	}
}

// TestSaveConfig_CaptchaModeInvalid 验证非法 captcha.mode 返回 400，
// 且 GlobalConfig 中的验证码配置保持原值（不加锁、不写入）。
func TestSaveConfig_CaptchaModeInvalid(t *testing.T) {
	prevPath := appconfig.ConfigPath
	prevCfg := appconfig.GlobalConfig
	appconfig.ConfigPath = minimalTestConfigPath(t)
	t.Cleanup(func() {
		appconfig.ConfigPath = prevPath
		appconfig.GlobalConfig = prevCfg
	})
	appconfig.GlobalConfig = appconfig.Config{
		Account: appconfig.AccountConfig{
			Captcha: appconfig.CaptchaConfig{Mode: appconfig.CaptchaModeDisabled, TriggerAfter: 4},
		},
	}

	w := postConfig(t, "/api/v1/web/config", `{"account":{"captcha":{"mode":"bogus","triggerAfter":9}}}`, &Handler{})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 mode 返回状态码 = %d, want %d", w.Code, http.StatusBadRequest)
	}

	got := appconfig.GlobalConfig.Account.Captcha
	if got.Mode != appconfig.CaptchaModeDisabled || got.TriggerAfter != 4 {
		t.Errorf("非法 mode 保存后 Captcha = %+v, want 保持 mode=disabled triggerAfter=4", got)
	}
}

// TestSaveConfig_PersistsURLUploadMaxSize 验证 URL 上传服务器侧大小硬上限（maxSize）
// 经 SaveConfig 写入 GlobalConfig 并通过 upload.url_upload.max_size 持久化到配置文件，
// 且 0=不限、负数归一为 0。
func TestSaveConfig_PersistsURLUploadMaxSize(t *testing.T) {
	cases := []struct {
		name     string
		maxSize  int64
		wantSize int64
	}{
		{"正数写入", 1 << 30, 1 << 30},
		{"零值表示不限制", 0, 0},
		{"负数归一为 0", -5, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prevPath := appconfig.ConfigPath
			prevCfg := appconfig.GlobalConfig
			appconfig.ConfigPath = minimalTestConfigPath(t)
			t.Cleanup(func() {
				appconfig.ConfigPath = prevPath
				appconfig.GlobalConfig = prevCfg
			})
			appconfig.GlobalConfig = appconfig.Config{
				Storage: appconfig.StorageConfig{
					Temp: appconfig.TempConfig{Enabled: false},
				},
			}

			body := `{"upload":{"urlUpload":{"maxSize":` + strconv.FormatInt(tc.maxSize, 10) + `}}}`
			postConfig(t, "/api/v1/web/config", body, &Handler{})

			got := appconfig.GlobalConfig.Upload.URLUpload.MaxSize
			if got != tc.wantSize {
				t.Errorf("SaveConfig 后 MaxSize = %d, want %d", got, tc.wantSize)
			}

			out, err := os.ReadFile(appconfig.ConfigPath)
			if err != nil {
				t.Fatalf("读取持久化配置失败: %v", err)
			}
			yamlText := string(out)
			if !strings.Contains(yamlText, "max_size: "+strconv.FormatInt(tc.wantSize, 10)) {
				t.Errorf("持久化 YAML 缺少 upload.url_upload.max_size: %s", yamlText)
			}
		})
	}
}

// TestGetOptions_URLUploadExposesMaxSize 验证公开 options 的 upload.urlUpload.maxSize
// 透出当前生效的服务器侧大小硬上限。
func TestGetOptions_URLUploadExposesMaxSize(t *testing.T) {
	prevCfg := appconfig.GlobalConfig
	t.Cleanup(func() { appconfig.GlobalConfig = prevCfg })
	appconfig.GlobalConfig = appconfig.Config{
		Upload: appconfig.UploadConfig{
			URLUpload: appconfig.URLUploadConfig{MaxSize: 2 << 30},
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/api/v1/web/options", nil)
	(&Handler{}).GetOptions(c)

	var resp struct {
		Data struct {
			Upload struct {
				URLUpload map[string]interface{} `json:"urlUpload"`
			} `json:"upload"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析 options 响应失败: %v", err)
	}
	if got, ok := resp.Data.Upload.URLUpload["maxSize"].(float64); !ok || int64(got) != 2<<30 {
		t.Errorf("options.upload.urlUpload.maxSize = %v, want %d", resp.Data.Upload.URLUpload["maxSize"], 2<<30)
	}
}

// equalStrings 按顺序比较两个字符串切片。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
